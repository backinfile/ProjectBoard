package agentexec

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/projectboard/projectboard/internal/agentrequest"
	"github.com/projectboard/projectboard/internal/knowledge"
	"github.com/projectboard/projectboard/internal/projectrepo"
	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
)

type Invocation struct {
	WorkDir, Prompt, ThreadID                 string
	KnowledgeCommand, DatabasePath, ProjectID string
	Timeout                                   time.Duration
	PlanOnly                                  bool
	OnOutput                                  func([]byte)
	OnActivity                                func()
}
type Result struct {
	ThreadID, Status, Message, Raw, CommandJSON string
	KnowledgeOperations                         []knowledge.Operation
}
type Runner interface {
	Run(context.Context, Invocation) (Result, error)
}

type ExecRunner struct {
	Command    string
	SchemaPath string
}

func (r *ExecRunner) Run(ctx context.Context, in Invocation) (Result, error) {
	command := r.Command
	if command == "" {
		command = "codex"
	}
	args, stdin := execInput(in, r.SchemaPath, runtime.GOOS)
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = in.WorkDir
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = activityWriter{writer: &stdout, output: in.OnOutput, activity: in.OnActivity}
	cmd.Stderr = activityWriter{writer: &stderr, activity: in.OnActivity}
	err := runAgentCommand(cmd)
	raw := stdout.String()
	recordedArgs := append([]string(nil), args...)
	if runtime.GOOS == "windows" && len(recordedArgs) > 0 {
		recordedArgs[len(recordedArgs)-1] = "<prompt>"
	}
	commandJSON, _ := json.Marshal(append([]string{command}, recordedArgs...))
	result := Result{Raw: raw, CommandJSON: string(commandJSON)}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event["type"] {
		case "thread.started":
			result.ThreadID, _ = event["thread_id"].(string)
		case "item.completed":
			item, _ := event["item"].(map[string]any)
			if item["type"] == "agent_message" {
				result.Message, _ = item["text"].(string)
			}
		case "turn.failed", "error":
			if err == nil {
				err = errors.New("Codex reported a failed turn")
			}
		}
	}
	if err != nil {
		return result, fmt.Errorf("codex exec: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var envelope struct {
		Status              string                `json:"status"`
		Message             string                `json:"message"`
		KnowledgeOperations []knowledge.Operation `json:"knowledgeOperations"`
	}
	if json.Unmarshal([]byte(result.Message), &envelope) != nil {
		return result, errors.New("Codex returned invalid structured output")
	}
	result.Status, result.Message, result.KnowledgeOperations = envelope.Status, envelope.Message, envelope.KnowledgeOperations
	if result.Status == "" || result.Message == "" {
		return result, errors.New("Codex result is missing status or message")
	}
	return result, nil
}

func execArgs(in Invocation, schemaPath string) []string {
	return execArgsWithPrompt(in, schemaPath, "-")
}

type activityWriter struct {
	writer   io.Writer
	output   func([]byte)
	activity func()
}

func (w activityWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if n > 0 {
		if w.activity != nil {
			w.activity()
		}
		if w.output != nil {
			w.output(append([]byte(nil), p[:n]...))
		}
	}
	return n, err
}

func execInput(in Invocation, schemaPath, goos string) ([]string, io.Reader) {
	if goos == "windows" {
		return execArgsWithPrompt(in, schemaPath, in.Prompt), nil
	}
	return execArgs(in, schemaPath), strings.NewReader(in.Prompt)
}

func execArgsWithPrompt(in Invocation, schemaPath, promptArg string) []string {
	args := []string{"exec", "--json", "--sandbox", "danger-full-access", "--output-schema", schemaPath, "-C", in.WorkDir}
	if in.KnowledgeCommand != "" && in.DatabasePath != "" && in.ProjectID != "" {
		mcpArgs := []string{"knowledge-mcp", in.DatabasePath, in.ProjectID}
		quoted := make([]string, len(mcpArgs))
		for index, value := range mcpArgs {
			quoted[index] = strconv.Quote(value)
		}
		configs := []string{
			"mcp_servers.projectboard_knowledge.command=" + strconv.Quote(in.KnowledgeCommand),
			"mcp_servers.projectboard_knowledge.args=[" + strings.Join(quoted, ",") + "]",
			`mcp_servers.projectboard_knowledge.enabled_tools=["knowledge_search","knowledge_read"]`,
			"mcp_servers.projectboard_knowledge.default_tools_approval_mode=\"approve\"",
			"mcp_servers.projectboard_knowledge.required=true",
		}
		for _, config := range configs {
			args = append(args, "--config", config)
		}
	}
	return append(args, promptArg)
}

type Options struct {
	DataDir          string
	DatabasePath     string
	KnowledgeCommand string
	Runner           Runner
	PollInterval     time.Duration
	StallTimeout     time.Duration
}
type Module struct {
	store            *store.Store
	queue            *workqueue.Module
	requests         *agentrequest.Module
	knowledge        *knowledge.Module
	dataDir          string
	databasePath     string
	knowledgeCommand string
	runner           Runner
	poll             time.Duration
	stall            time.Duration
	wake             chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	mu               sync.Mutex
	repoMu           sync.Mutex
	running          map[string]*runControl
}
type runControl struct {
	itemID       string
	cancel       context.CancelCauseFunc
	done         chan struct{}
	lastActivity time.Time
}

var (
	errWorkItemBlocked  = errors.New("work item blocked by a member")
	errExecutionStalled = errors.New("Codex produced no activity before the stall timeout")
)

func New(database *store.Store, queue *workqueue.Module, requests *agentrequest.Module, knowledgeModule *knowledge.Module, options ...Options) *Module {
	var option Options
	if len(options) > 0 {
		option = options[0]
	}
	if option.DataDir == "" {
		option.DataDir = "./data"
	}
	if option.PollInterval <= 0 {
		option.PollInterval = 5 * time.Second
	}
	if option.StallTimeout <= 0 {
		option.StallTimeout = 10 * time.Minute
	}
	if option.Runner == nil {
		option.Runner = &ExecRunner{Command: "codex", SchemaPath: filepath.Join(option.DataDir, "codex-result-schema.json")}
	}
	return &Module{store: database, queue: queue, requests: requests, knowledge: knowledgeModule, dataDir: option.DataDir, databasePath: option.DatabasePath, knowledgeCommand: option.KnowledgeCommand, runner: option.Runner, poll: option.PollInterval, stall: option.StallTimeout, wake: make(chan struct{}, 1), running: map[string]*runControl{}}
}

const resultSchema = `{"type":"object","properties":{"status":{"type":"string","enum":["planned","completed","failed"]},"message":{"type":"string"},"knowledgeOperations":{"type":"array","maxItems":100,"items":{"type":"object","properties":{"type":{"type":"string","enum":["create","update","move","delete"]},"nodeId":{"type":"string"},"parentId":{"type":"string"},"title":{"type":"string"},"markdown":{"type":"string"},"summary":{"type":"string"},"triggerDescription":{"type":"string"},"sortOrder":{"type":"integer"},"expectedVersion":{"type":"integer"}},"required":["type"],"additionalProperties":false}}},"required":["status","message"],"additionalProperties":false}`

func (m *Module) Start() error {
	if err := os.MkdirAll(filepath.Join(m.dataDir, "workspaces"), 0o700); err != nil {
		return err
	}
	if _, ok := m.runner.(*ExecRunner); ok {
		if err := os.WriteFile(filepath.Join(m.dataDir, "codex-result-schema.json"), []byte(resultSchema), 0o600); err != nil {
			return err
		}
	}
	type interruptedRequest struct{ requestID, itemID string }
	interrupted := []interruptedRequest{}
	rows, err := m.store.DB.Query("SELECT id,COALESCE(source_work_item_id,'') FROM agent_requests WHERE status='running'")
	if err != nil {
		return err
	}
	for rows.Next() {
		var value interruptedRequest
		if err = rows.Scan(&value.requestID, &value.itemID); err != nil {
			_ = rows.Close()
			return err
		}
		interrupted = append(interrupted, value)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	stamp := now()
	_, _ = m.store.DB.Exec("UPDATE agent_requests SET status='failed',ended_at=?,error_message='ProjectBoard restarted; a new retry was queued' WHERE status='running'", stamp)
	for _, value := range interrupted {
		_, _ = m.requests.Retry(context.Background(), value.requestID, "")
		if value.itemID != "" {
			_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state='queued',resume_requested=1,version=version+1,updated_at=? WHERE id=? AND stage<>'closed'", stamp, value.itemID)
			_ = m.event(&claim{RequestID: value.requestID, ItemID: value.itemID}, "agent_restart_queued", "ProjectBoard restarted; a fresh Agent request was queued automatically")
		}
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go m.loop()
	return nil
}
func (m *Module) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Lock()
	for _, control := range m.running {
		control.cancel(context.Canceled)
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *Module) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Module) Cancel(requestID string) {
	m.mu.Lock()
	control, ok := m.running[requestID]
	m.mu.Unlock()
	if ok {
		control.cancel(context.Canceled)
	}
}
func (m *Module) loop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.poll)
	defer ticker.Stop()
	for {
		m.inspectStalled()
		m.schedule()
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}
	}
}

type claim struct {
	RequestID, Kind, ItemID, AgentID, ProjectID, ProjectKey, ProjectPath, Title, Description, Criteria, TargetBranch, WorkflowType, Phase, BaseCommit, Workspace string
	CustomPrompt                                                                                                                                                 string
	AgentRules, ValidationCommands, ForbiddenPaths, AgentPrompts, KnowledgeSnapshot                                                                              string
	RequestNumber, ItemNumber, Timeout                                                                                                                           int64
	PauseBeforeCompletion, PlanOnly                                                                                                                              bool
}

func (m *Module) schedule() {
	for {
		claimed, err := m.claim()
		if err != nil || claimed == nil {
			return
		}
		ctx, cancel := context.WithCancelCause(m.ctx)
		control := &runControl{itemID: claimed.ItemID, cancel: cancel, done: make(chan struct{})}
		m.mu.Lock()
		m.running[claimed.RequestID] = control
		m.mu.Unlock()
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer func() {
				m.mu.Lock()
				delete(m.running, claimed.RequestID)
				close(control.done)
				m.mu.Unlock()
				m.Wake()
			}()
			m.execute(ctx, claimed)
		}()
	}
}

func (m *Module) inspectStalled() {
	cutoff := time.Now().Add(-m.stall)
	m.mu.Lock()
	for _, control := range m.running {
		if !control.lastActivity.IsZero() && control.lastActivity.Before(cutoff) {
			control.cancel(errExecutionStalled)
		}
	}
	m.mu.Unlock()
}

func (m *Module) noteActivity(requestID string) {
	m.mu.Lock()
	if control := m.running[requestID]; control != nil {
		control.lastActivity = time.Now()
	}
	m.mu.Unlock()
}

func (m *Module) appendOutput(requestID string, chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	m.noteActivity(requestID)
	_, _ = m.store.DB.Exec("UPDATE agent_requests SET output_jsonl=output_jsonl||? WHERE id=? AND status='running'", string(chunk), requestID)
}

func (m *Module) claim() (*claim, error) {
	var claimed *claim
	err := m.store.Write(context.Background(), func(tx *sql.Tx) error {
		row := tx.QueryRow(`SELECT r.id,r.kind,r.project_id,p.project_key,p.project_path,r.number,COALESCE(r.source_work_item_id,''),COALESCE(w.number,0),COALESCE(w.title,r.title),COALESCE(w.description_markdown,''),COALESCE(w.acceptance_criteria_markdown,''),COALESCE(w.target_branch,p.default_target_branch),COALESCE(w.workflow_type,'standard'),COALESCE(w.agent_phase,'work'),COALESCE(w.base_commit_sha,''),COALESCE(w.pause_before_completion,0),r.prompt_markdown,p.agent_rules_markdown,p.validation_commands_json,p.forbidden_paths_json,p.agent_prompts_json,a.id,a.turn_timeout_minutes
			FROM agent_requests r JOIN projects p ON p.id=r.project_id LEFT JOIN work_items w ON w.id=r.source_work_item_id CROSS JOIN agents a
			WHERE r.status='queued' AND a.status='active' AND a.revoked_at IS NULL
			AND (w.id IS NULL OR w.blocked_at IS NULL)
			AND (SELECT COUNT(*) FROM agent_requests active WHERE active.assigned_agent_id=a.id AND active.status='running')<a.max_concurrent_tasks
			AND (r.kind NOT IN('task_knowledge','project_knowledge_compaction') OR NOT EXISTS(SELECT 1 FROM agent_requests k WHERE k.project_id=r.project_id AND k.status='running' AND k.kind IN('task_knowledge','project_knowledge_compaction')))
			AND (r.kind NOT IN('task_plan','task_execution') OR NOT EXISTS (
				SELECT 1 FROM work_item_tags wt JOIN json_each(a.reject_tags_json) rejected
				WHERE wt.work_item_id=w.id AND lower(wt.tag)=lower(CAST(rejected.value AS TEXT))))
			AND (r.kind NOT IN('task_plan','task_execution') OR json_array_length(a.accept_tags_json)=0 OR EXISTS (
				SELECT 1 FROM work_item_tags wt JOIN json_each(a.accept_tags_json) accepted
				WHERE wt.work_item_id=w.id AND lower(wt.tag)=lower(CAST(accepted.value AS TEXT))))
			AND (r.kind NOT IN('task_plan','task_execution') OR (w.workflow_type='standard' AND w.agent_phase<>'merge') OR NOT EXISTS (
				SELECT 1 FROM agent_requests active JOIN work_items active_work ON active_work.id=active.source_work_item_id
				WHERE active.project_id=r.project_id AND active.status='running'
				AND (active_work.workflow_type='simple_conversation' OR active_work.agent_phase='merge')))
			ORDER BY (CAST((SELECT COUNT(*) FROM agent_requests active WHERE active.assigned_agent_id=a.id AND active.status='running') AS REAL)/a.max_concurrent_tasks),a.created_at,a.id,r.created_at,r.id LIMIT 1`)
		var value claim
		var pause int
		if err := row.Scan(&value.RequestID, &value.Kind, &value.ProjectID, &value.ProjectKey, &value.ProjectPath, &value.RequestNumber, &value.ItemID, &value.ItemNumber, &value.Title, &value.Description, &value.Criteria, &value.TargetBranch, &value.WorkflowType, &value.Phase, &value.BaseCommit, &pause, &value.CustomPrompt, &value.AgentRules, &value.ValidationCommands, &value.ForbiddenPaths, &value.AgentPrompts, &value.AgentID, &value.Timeout); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		value.PauseBeforeCompletion = pause != 0
		value.PlanOnly = value.Kind == "task_plan"
		if isKnowledgeKind(value.Kind) {
			value.Workspace = filepath.Join(m.dataDir, "workspaces", safe(value.RequestID))
		} else if value.WorkflowType == "simple_conversation" || value.Phase == "merge" {
			value.Workspace = value.ProjectPath
		} else {
			value.Workspace = projectrepo.WorktreePath(value.ProjectPath, value.ProjectKey, value.ItemNumber)
		}
		stamp := now()
		result, err := tx.Exec("UPDATE agent_requests SET status='running',assigned_agent_id=?,started_at=?,workspace_path=? WHERE id=? AND status='queued'", value.AgentID, stamp, value.Workspace, value.RequestID)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count == 1 {
			claimed = &value
			if value.ItemID != "" && (value.Kind == "task_plan" || value.Kind == "task_execution") {
				_, err = tx.Exec("UPDATE work_items SET stage=CASE WHEN stage='created' THEN 'in_progress' ELSE stage END,agent_state='running',assigned_agent_id=?,version=version+1,updated_at=? WHERE id=?", value.AgentID, stamp, value.ItemID)
			}
		}
		return nil
	})
	return claimed, err
}

func safe(value string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(strings.ToLower(value))
}
func isKnowledgeKind(kind string) bool {
	return kind == "task_knowledge" || kind == "project_knowledge_compaction"
}
func (m *Module) prepare(ctx context.Context, claimed *claim) error {
	if isKnowledgeKind(claimed.Kind) {
		if err := os.MkdirAll(claimed.Workspace, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(claimed.Workspace, "PROJECT_KNOWLEDGE.md"), []byte(claimed.KnowledgeSnapshot), 0o400)
	}
	if claimed.WorkflowType == "simple_conversation" || claimed.Phase == "merge" {
		m.repoMu.Lock()
		defer m.repoMu.Unlock()
		status, err := projectrepo.Status(ctx, claimed.ProjectPath)
		if err != nil || status != "" {
			if err == nil {
				err = errors.New("project repository must be clean before writing its target branch")
			}
			return err
		}
		branch, err := projectrepo.CurrentBranch(ctx, claimed.ProjectPath)
		if err != nil {
			return err
		}
		if branch != claimed.TargetBranch {
			if err = projectrepo.Checkout(ctx, claimed.ProjectPath, claimed.TargetBranch); err != nil {
				return err
			}
		}
		claimed.Workspace = claimed.ProjectPath
		_, err = m.store.DB.ExecContext(ctx, "UPDATE work_items SET workspace_path=? WHERE id=?", claimed.Workspace, claimed.ItemID)
		return err
	}
	m.repoMu.Lock()
	workspace, err := projectrepo.EnsureWorktree(ctx, claimed.ProjectPath, claimed.ProjectKey, claimed.ItemNumber, claimed.TargetBranch)
	m.repoMu.Unlock()
	if err != nil {
		return err
	}
	claimed.Workspace = workspace.Path
	if claimed.BaseCommit == "" {
		claimed.BaseCommit = workspace.BaseCommit
	}
	_, err = m.store.DB.ExecContext(ctx, "UPDATE work_items SET workspace_path=?,base_commit_sha=COALESCE(base_commit_sha,?) WHERE id=?", claimed.Workspace, claimed.BaseCommit, claimed.ItemID)
	return err
}

func (m *Module) prompt(ctx context.Context, claimed *claim) string {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "You are executing one immutable ProjectBoard Agent Request.\nRequest: %s\nType: %s\nProject: %s\n", claimed.RequestID, claimed.Kind, claimed.ProjectKey)
	if claimed.ItemID != "" {
		fmt.Fprintf(&prompt, "Task: %s\nTitle: %s\nDescription:\n%s\nAcceptance criteria:\n%s\nTarget branch: %s\nWorkflow: %s\n", claimed.ItemID, claimed.Title, claimed.Description, claimed.Criteria, claimed.TargetBranch, claimed.WorkflowType)
	}
	fmt.Fprintf(&prompt, "Project Agent rules:\n%s\nProject prompt segments (JSON):\n%s\nValidation commands (JSON):\n%s\nForbidden paths (JSON):\n%s\n", claimed.AgentRules, claimed.AgentPrompts, claimed.ValidationCommands, claimed.ForbiddenPaths)
	if strings.TrimSpace(claimed.CustomPrompt) != "" {
		fmt.Fprintf(&prompt, "Request context:\n%s\n", claimed.CustomPrompt)
	}
	if isKnowledgeKind(claimed.Kind) {
		prompt.WriteString("The following project knowledge snapshot is read-only context. Do not edit the snapshot file directly.\n\n")
	} else {
		prompt.WriteString("The following project knowledge catalog is discovery metadata, not the full knowledge content. When the task depends on project-specific decisions, conventions, architecture, release procedures, or runbooks, call knowledge_search and then knowledge_read before relying on that knowledge. Do not query for generic programming facts. If a search is insufficient, rewrite it at most once.\n\n")
	}
	prompt.WriteString(claimed.KnowledgeSnapshot)
	if claimed.PlanOnly {
		prompt.WriteString("\nProduce a concrete implementation plan only. Do not edit files. Return status planned.\n")
	} else if isKnowledgeKind(claimed.Kind) {
		prompt.WriteString("\nReturn complete knowledgeOperations. Each create or update must include a concise summary and triggerDescription explaining when ordinary task Agents should retrieve the node. Each update/move/delete must include the current expectedVersion. The server applies the entire list transactionally; patches and partial writes are not accepted. Locked nodes cannot be changed, but children may be created below them. Return status completed.\n")
	} else if claimed.Phase == "merge" {
		workBranch := fmt.Sprintf("projectboard/%s/%d", strings.ToLower(claimed.ProjectKey), claimed.ItemNumber)
		fmt.Fprintf(&prompt, "\nBefore merging, rebase the task branch %s onto the current tip of the checked out target branch %s in its existing task worktree. Resolve all rebase conflicts on the task branch and verify the repository after the rebase. If %s changes before the merge, repeat the rebase and verification. Only after the rebase and verification succeed, merge %s into %s with --no-ff, using merge commit message %q. Do not fetch, pull, or push. Return completed only when the branch is fully merged and both working trees are clean.\n", workBranch, claimed.TargetBranch, claimed.TargetBranch, workBranch, claimed.TargetBranch, "merge: "+claimed.ItemID+" "+claimed.Title)
	} else if claimed.WorkflowType == "simple_conversation" {
		prompt.WriteString("\nWork directly on the checked out project target branch. Implement, verify, and commit locally. Do not fetch, pull, or push. Return completed only when the repository is clean.\n")
	} else {
		prompt.WriteString("\nImplement and verify the task in this isolated worktree, then commit all task changes on the current task branch. Do not merge the target branch and do not fetch, pull, or push. Return completed only when the worktree is clean and ready to merge.\n")
	}
	if claimed.ItemID != "" {
		if item, err := m.queue.Get(ctx, claimed.ItemID); err == nil {
			prompt.WriteString("Recent task conversation:\n")
			for _, entry := range item.Conversation {
				raw, _ := json.Marshal(entry)
				prompt.Write(raw)
				prompt.WriteByte('\n')
			}
		}
	}
	prompt.WriteString("Your final response must follow the supplied JSON schema. Put user-facing Markdown in message.\n")
	return prompt.String()
}

func (m *Module) execute(parent context.Context, claimed *claim) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(claimed.Timeout)*time.Minute)
	defer cancel()
	var snapshot string
	var err error
	if isKnowledgeKind(claimed.Kind) {
		snapshot, err = m.knowledge.Snapshot(ctx, claimed.ProjectID)
	} else {
		snapshot, err = m.knowledge.Catalog(ctx, claimed.ProjectID)
	}
	if err != nil {
		m.fail(claimed, Result{}, err)
		return
	}
	claimed.KnowledgeSnapshot = snapshot
	if err = m.prepare(ctx, claimed); err != nil {
		m.fail(claimed, Result{}, err)
		return
	}
	prompt := m.prompt(ctx, claimed)
	_, _ = m.store.DB.Exec("UPDATE agent_requests SET prompt_markdown=? WHERE id=? AND status='running'", prompt, claimed.RequestID)
	_ = m.event(claimed, "agent_started", "Local Codex execution started")
	m.noteActivity(claimed.RequestID)
	knowledgeCommand, databasePath, projectID := m.knowledgeCommand, m.databasePath, claimed.ProjectID
	if isKnowledgeKind(claimed.Kind) {
		knowledgeCommand, databasePath, projectID = "", "", ""
	}
	result, err := m.runner.Run(ctx, Invocation{
		WorkDir: claimed.Workspace, Prompt: prompt, Timeout: time.Duration(claimed.Timeout) * time.Minute, PlanOnly: claimed.PlanOnly,
		OnOutput: func(chunk []byte) { m.appendOutput(claimed.RequestID, chunk) }, OnActivity: func() { m.noteActivity(claimed.RequestID) },
		KnowledgeCommand: knowledgeCommand, DatabasePath: databasePath, ProjectID: projectID,
	})
	if err != nil {
		switch context.Cause(parent) {
		case errWorkItemBlocked:
			m.interrupt(claimed, result, "Agent execution stopped because the task was blocked", true)
			return
		case errExecutionStalled:
			m.interrupt(claimed, result, fmt.Sprintf("Codex produced no output or activity for %s; execution was stopped automatically", m.stall), false)
			return
		}
		m.fail(claimed, result, err)
		return
	}
	if !m.active(claimed.RequestID) {
		return
	}
	if err = m.finish(claimed, result); err != nil {
		m.fail(claimed, result, err)
		return
	}
	m.record(claimed, result, "succeeded", nil)
	if claimed.Kind == "task_knowledge" {
		_, _ = m.requests.MaybeScheduleCompaction(context.Background(), claimed.ProjectID)
		m.Wake()
	}
}

func (m *Module) interrupt(claimed *claim, result Result, message string, requeue bool) {
	if !m.active(claimed.RequestID) {
		return
	}
	m.record(claimed, result, "failed", errors.New(message))
	if claimed.ItemID == "" {
		return
	}
	state := "paused_failure"
	if requeue {
		state = "queued"
		_, _ = m.requests.Retry(context.Background(), claimed.RequestID, "")
		m.Wake()
	}
	_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state=?,codex_thread_id=COALESCE(?,codex_thread_id),workspace_path=?,version=version+1,updated_at=? WHERE id=? AND stage<>'closed'", state, nullable(result.ThreadID), claimed.Workspace, now(), claimed.ItemID)
	_ = m.event(claimed, map[bool]string{true: "agent_interrupted", false: "agent_stalled"}[requeue], message)
}

func (m *Module) finish(claimed *claim, result Result) error {
	if result.Status == "failed" {
		return errors.New("Agent returned failed status")
	}
	if isKnowledgeKind(claimed.Kind) {
		if result.Status != "completed" {
			return errors.New("knowledge request did not return completed status")
		}
		_, err := m.knowledge.Apply(context.Background(), knowledge.Actor{Type: "agent", ID: claimed.AgentID}, claimed.ProjectID, result.KnowledgeOperations)
		return err
	}
	if claimed.ItemID == "" {
		return errors.New("task Agent request has no source task")
	}
	item, err := m.queue.Get(context.Background(), claimed.ItemID)
	if err != nil {
		return err
	}
	if result.Message != "" {
		item, err = m.queue.AddMessage(context.Background(), workqueue.Actor{Type: "agent", ID: claimed.AgentID}, item.ID, result.Message, item.Version)
		if err != nil {
			return err
		}
	}
	if claimed.Kind == "task_plan" {
		if result.Status != "planned" {
			return errors.New("plan request did not return planned status")
		}
		_, err = m.store.DB.Exec("UPDATE work_items SET agent_state='paused_plan',plan_pause_consumed=1,codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", nullable(result.ThreadID), claimed.Workspace, now(), claimed.ItemID)
		if err == nil {
			_ = m.event(claimed, "agent_plan_paused", "Agent completed the plan and is waiting for confirmation")
		}
		return err
	}
	if result.Status != "completed" {
		return errors.New("task execution did not return completed status")
	}
	if status, statusErr := projectrepo.Status(context.Background(), claimed.Workspace); statusErr != nil || status != "" {
		if statusErr != nil {
			return statusErr
		}
		return errors.New("Codex completed with uncommitted workspace changes")
	}
	if claimed.Phase == "merge" {
		workBranch := fmt.Sprintf("projectboard/%s/%d", strings.ToLower(claimed.ProjectKey), claimed.ItemNumber)
		merged, mergeErr := projectrepo.IsAncestor(context.Background(), claimed.ProjectPath, workBranch, claimed.TargetBranch)
		if mergeErr != nil || !merged {
			if mergeErr == nil {
				mergeErr = errors.New("task branch is not merged into the target branch")
			}
			return mergeErr
		}
		return m.closeTask(claimed)
	}
	if claimed.WorkflowType == "simple_conversation" {
		if claimed.PauseBeforeCompletion {
			_, err = m.store.DB.Exec("UPDATE work_items SET stage='completed',completed_at=?,agent_phase='close_review',agent_state='paused_completion',codex_thread_id=?,workspace_path=NULL,version=version+1,updated_at=? WHERE id=?", now(), nullable(result.ThreadID), now(), claimed.ItemID)
			if err == nil {
				_ = m.event(claimed, "agent_completion_paused", "Agent completed its work and is waiting for confirmation")
			}
			return err
		}
		return m.closeTask(claimed)
	}
	stamp := now()
	if claimed.PauseBeforeCompletion {
		_, err = m.store.DB.Exec("UPDATE work_items SET stage='completed',completed_at=?,agent_phase='merge_review',agent_state='paused_completion',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", stamp, nullable(result.ThreadID), claimed.Workspace, stamp, claimed.ItemID)
		if err == nil {
			_ = m.event(claimed, "agent_completion_paused", "Agent completed implementation and is waiting for merge approval")
		}
		return err
	}
	_, err = m.store.DB.Exec("UPDATE work_items SET stage='completed',completed_at=?,agent_phase='merge',agent_state='idle',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", stamp, nullable(result.ThreadID), claimed.Workspace, stamp, claimed.ItemID)
	if err != nil {
		return err
	}
	m.record(claimed, result, "succeeded", nil)
	_, err = m.requests.Create(context.Background(), agentrequest.CreateInput{ProjectID: claimed.ProjectID, Kind: "task_execution", SourceWorkItemID: claimed.ItemID, Title: "Merge task: " + claimed.ItemID + " " + claimed.Title, CreatedByType: "system"})
	if err == nil {
		m.Wake()
	}
	return err
}

func (m *Module) closeTask(claimed *claim) error {
	item, err := m.queue.Get(context.Background(), claimed.ItemID)
	if err != nil {
		return err
	}
	actor := workqueue.Actor{Type: "agent", ID: claimed.AgentID}
	if item.Stage == "created" {
		item, err = m.queue.MoveStage(context.Background(), actor, item.ID, item.Version, "in_progress", "Agent execution started")
	}
	if err == nil && item.Stage == "in_progress" {
		item, err = m.queue.MoveStage(context.Background(), actor, item.ID, item.Version, "completed", "Agent execution completed")
	}
	if err == nil && item.Stage == "completed" {
		_, err = m.queue.MoveStage(context.Background(), actor, item.ID, item.Version, "closed", "Agent execution auto-closed task")
	}
	return err
}

func (m *Module) record(claimed *claim, result Result, status string, runErr error) {
	var errorMessage any
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	payload, _ := json.Marshal(map[string]any{"status": result.Status, "message": result.Message, "knowledgeOperations": result.KnowledgeOperations})
	_, _ = m.store.DB.Exec("UPDATE agent_requests SET status=?,thread_id=?,command_json=?,result_json=?,output_jsonl=CASE WHEN ?='' THEN output_jsonl ELSE ? END,final_message=?,ended_at=?,error_message=? WHERE id=? AND status='running'", status, nullable(result.ThreadID), defaultJSON(result.CommandJSON), string(payload), result.Raw, result.Raw, nullable(result.Message), now(), errorMessage, claimed.RequestID)
}
func (m *Module) fail(claimed *claim, result Result, err error) {
	if !m.active(claimed.RequestID) {
		return
	}
	m.record(claimed, result, "failed", err)
	if claimed.ItemID != "" && (claimed.Kind == "task_plan" || claimed.Kind == "task_execution") {
		_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state='paused_failure',version=version+1,updated_at=? WHERE id=? AND stage<>'closed'", now(), claimed.ItemID)
	}
}
func (m *Module) active(requestID string) bool {
	var count int
	return m.store.DB.QueryRow("SELECT COUNT(*) FROM agent_requests WHERE id=? AND status='running'", requestID).Scan(&count) == nil && count == 1
}

func (m *Module) event(claimed *claim, kind, message string) error {
	if claimed.ItemID == "" {
		return nil
	}
	raw, _ := json.Marshal(map[string]any{"message": message, "requestId": claimed.RequestID})
	if _, err := m.store.DB.Exec("INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) SELECT ?,id,?,stage,'system',NULL,?,version,? FROM work_items WHERE id=?", security.Token(18), kind, string(raw), now(), claimed.ItemID); err != nil {
		return err
	}
	return m.notifyAgentEvent(claimed.ItemID, kind)
}

func agentEventNotification(kind string) (string, string, bool) {
	switch kind {
	case "agent_started":
		return "Agent 已开始执行", "本地 Codex Agent 已开始执行任务。", true
	case "agent_plan_paused":
		return "Agent 计划等待确认", "Agent 已完成计划，正在等待成员确认继续。", true
	case "agent_completion_paused":
		return "Agent 已完成任务", "Agent 已完成任务，正在等待成员验收。", true
	case "agent_stalled":
		return "Agent 执行已自动停止", "Codex 长时间没有活动，本次执行已停止并释放容量。", true
	case "agent_interrupted":
		return "Agent 执行已中断", "任务被阻塞，本地 Agent 执行已停止。", true
	case "agent_restart_queued":
		return "Agent 已自动恢复排队", "ProjectBoard 服务重启后已创建新的 Agent 重试需求。", true
	default:
		return "", "", false
	}
}

func (m *Module) notifyAgentEvent(itemID, kind string) error {
	title, body, notify := agentEventNotification(kind)
	if !notify {
		return nil
	}
	rows, err := m.store.DB.Query(`SELECT w.project_id,w.created_by_user_id FROM work_items w WHERE w.id=? AND w.created_by_user_id IS NOT NULL
		UNION SELECT w.project_id,w.assignee_id FROM work_items w WHERE w.id=? AND w.assignee_kind='human' AND w.assignee_id IS NOT NULL
		UNION SELECT w.project_id,f.user_id FROM work_items w JOIN work_item_followers f ON f.work_item_id=w.id WHERE w.id=?`, itemID, itemID, itemID)
	if err != nil {
		return err
	}
	type recipient struct{ projectID, userID string }
	recipients := []recipient{}
	for rows.Next() {
		var projectID, userID string
		if err = rows.Scan(&projectID, &userID); err != nil {
			_ = rows.Close()
			return err
		}
		recipients = append(recipients, recipient{projectID: projectID, userID: userID})
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if _, err = m.store.DB.Exec("INSERT INTO notifications(id,user_id,project_id,work_item_id,kind,title,body,actor_type,created_at) VALUES(?,?,?,?,?,?,?,'system',?)", security.Token(18), recipient.userID, recipient.projectID, itemID, kind, title, body, now()); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) DeactivateAgent(ctx context.Context, agentID string) error {
	var waits []<-chan struct{}
	m.mu.Lock()
	for requestID, control := range m.running {
		var assigned string
		if m.store.DB.QueryRow("SELECT COALESCE(assigned_agent_id,'') FROM agent_requests WHERE id=?", requestID).Scan(&assigned) == nil && assigned == agentID {
			control.cancel(context.Canceled)
			waits = append(waits, control.done)
		}
	}
	m.mu.Unlock()
	if err := waitForRuns(ctx, waits); err != nil {
		return err
	}
	_, err := m.store.DB.ExecContext(ctx, "UPDATE agent_requests SET status='queued',assigned_agent_id=NULL,started_at=NULL,ended_at=NULL,error_message=NULL WHERE assigned_agent_id=? AND status='running'", agentID)
	m.Wake()
	return err
}

func (m *Module) BlockWorkItem(ctx context.Context, itemID string) error {
	var waits []<-chan struct{}
	m.mu.Lock()
	for _, control := range m.running {
		if control.itemID == itemID {
			control.cancel(errWorkItemBlocked)
			waits = append(waits, control.done)
		}
	}
	m.mu.Unlock()
	return waitForRuns(ctx, waits)
}
func waitForRuns(ctx context.Context, waits []<-chan struct{}) error {
	for _, done := range waits {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (m *Module) CleanWorkspace(ctx context.Context, itemID string) error {
	var stage, workflow, workspace, projectPath, projectKey string
	var number int64
	if err := m.store.DB.QueryRowContext(ctx, `SELECT w.stage,w.workflow_type,COALESCE(w.workspace_path,''),p.project_path,p.project_key,w.number
		FROM work_items w JOIN projects p ON p.id=w.project_id WHERE w.id=?`, itemID).Scan(&stage, &workflow, &workspace, &projectPath, &projectKey, &number); err != nil {
		return err
	}
	if stage != "closed" {
		return errors.New("only closed task workspaces can be cleaned")
	}
	if workflow == "simple_conversation" || workspace == "" {
		if workflow == "simple_conversation" && workspace != "" {
			_, err := m.store.DB.ExecContext(ctx, "UPDATE work_items SET workspace_path=NULL,version=version+1,updated_at=? WHERE id=?", now(), itemID)
			return err
		}
		return nil
	}
	expected := projectrepo.WorktreePath(projectPath, projectKey, number)
	if !strings.EqualFold(filepath.Clean(workspace), filepath.Clean(expected)) {
		return errors.New("workspace path does not match the managed task worktree")
	}
	if err := projectrepo.RemoveWorktree(ctx, projectPath, workspace); err != nil {
		return err
	}
	_, _ = m.store.DB.ExecContext(ctx, "UPDATE agent_requests SET workspace_path=NULL WHERE source_work_item_id=? AND workspace_path=?", itemID, workspace)
	_, err := m.store.DB.ExecContext(ctx, "UPDATE work_items SET workspace_path=NULL,version=version+1,updated_at=? WHERE id=?", now(), itemID)
	return err
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
func defaultJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "[]"
	}
	return value
}
func ParsePositive(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

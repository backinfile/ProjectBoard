package agentexec

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
)

type Credential struct {
	Token  string
	Revoke func(context.Context) error
}
type CredentialProvider interface {
	Issue(context.Context, string) (*Credential, error)
}
type Invocation struct {
	WorkDir, Prompt, ThreadID string
	Timeout                   time.Duration
	PlanOnly                  bool
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
	args := execArgs(in, r.SchemaPath)
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = in.WorkDir
	cmd.Stdin = strings.NewReader(in.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	raw := stdout.String()
	commandJSON, _ := json.Marshal(append([]string{command}, args...))
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
	return []string{"exec", "--json", "--sandbox", "danger-full-access", "--output-schema", schemaPath, "-C", in.WorkDir, "-"}
}

type Options struct {
	DataDir      string
	Runner       Runner
	Credentials  CredentialProvider
	PollInterval time.Duration
}
type Module struct {
	store       *store.Store
	queue       *workqueue.Module
	requests    *agentrequest.Module
	knowledge   *knowledge.Module
	dataDir     string
	runner      Runner
	credentials CredentialProvider
	poll        time.Duration
	wake        chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.Mutex
	running     map[string]runControl
}
type runControl struct {
	cancel context.CancelFunc
	done   chan struct{}
}

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
	if option.Runner == nil {
		option.Runner = &ExecRunner{Command: "codex", SchemaPath: filepath.Join(option.DataDir, "codex-result-schema.json")}
	}
	return &Module{store: database, queue: queue, requests: requests, knowledge: knowledgeModule, dataDir: option.DataDir, runner: option.Runner, credentials: option.Credentials, poll: option.PollInterval, wake: make(chan struct{}, 1), running: map[string]runControl{}}
}

const resultSchema = `{"type":"object","properties":{"status":{"type":"string","enum":["planned","completed","failed"]},"message":{"type":"string"},"knowledgeOperations":{"type":"array","maxItems":100,"items":{"type":"object","properties":{"type":{"type":"string","enum":["create","update","move","delete"]},"nodeId":{"type":"string"},"parentId":{"type":"string"},"title":{"type":"string"},"markdown":{"type":"string"},"sortOrder":{"type":"integer"},"fileIds":{"type":"array","items":{"type":"string"}},"expectedVersion":{"type":"integer"}},"required":["type"],"additionalProperties":false}}},"required":["status","message"],"additionalProperties":false}`

func (m *Module) Start() error {
	if err := os.MkdirAll(filepath.Join(m.dataDir, "workspaces"), 0o700); err != nil {
		return err
	}
	if _, ok := m.runner.(*ExecRunner); ok {
		if err := os.WriteFile(filepath.Join(m.dataDir, "codex-result-schema.json"), []byte(resultSchema), 0o600); err != nil {
			return err
		}
	}
	stamp := now()
	_, _ = m.store.DB.Exec("UPDATE agent_requests SET status='failed',ended_at=?,error_message='ProjectBoard restarted while Codex was running' WHERE status='running'", stamp)
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
		control.cancel()
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
		control.cancel()
	}
}
func (m *Module) loop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.poll)
	defer ticker.Stop()
	for {
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
	RequestID, Kind, ItemID, AgentID, ProjectID, ProjectKey, Title, Description, Criteria, TargetBranch, Workspace string
	CustomPrompt                                                                                                   string
	AgentRules, ValidationCommands, ForbiddenPaths, AgentPrompts, KnowledgeSnapshot                                string
	RequestNumber, ItemNumber, Timeout                                                                             int64
	PauseAfterCompletion, PlanOnly                                                                                 bool
}

func (m *Module) schedule() {
	for {
		claimed, err := m.claim()
		if err != nil || claimed == nil {
			return
		}
		ctx, cancel := context.WithCancel(m.ctx)
		control := runControl{cancel: cancel, done: make(chan struct{})}
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

func (m *Module) claim() (*claim, error) {
	var claimed *claim
	err := m.store.Write(context.Background(), func(tx *sql.Tx) error {
		row := tx.QueryRow(`SELECT r.id,r.kind,r.project_id,p.project_key,r.number,COALESCE(r.source_work_item_id,''),COALESCE(w.number,0),COALESCE(w.title,r.title),COALESCE(w.description_markdown,''),COALESCE(w.acceptance_criteria_markdown,''),COALESCE(w.target_branch,p.default_target_branch),COALESCE(w.pause_after_completion,0),r.prompt_markdown,p.agent_rules_markdown,p.validation_commands_json,p.forbidden_paths_json,p.agent_prompts_json,a.id,a.turn_timeout_minutes
			FROM agent_requests r JOIN projects p ON p.id=r.project_id LEFT JOIN work_items w ON w.id=r.source_work_item_id CROSS JOIN agents a
			WHERE r.status='queued' AND a.status='active' AND a.revoked_at IS NULL
			AND (w.id IS NULL OR w.blocked_at IS NULL)
			AND (SELECT COUNT(*) FROM agent_requests active WHERE active.assigned_agent_id=a.id AND active.status='running')<a.max_concurrent_tasks
			AND (r.kind NOT IN('task_knowledge','project_knowledge_compaction') OR NOT EXISTS(SELECT 1 FROM agent_requests k WHERE k.project_id=r.project_id AND k.status='running' AND k.kind IN('task_knowledge','project_knowledge_compaction')))
			ORDER BY (CAST((SELECT COUNT(*) FROM agent_requests active WHERE active.assigned_agent_id=a.id AND active.status='running') AS REAL)/a.max_concurrent_tasks),a.created_at,a.id,r.created_at,r.id LIMIT 1`)
		var value claim
		var pause int
		if err := row.Scan(&value.RequestID, &value.Kind, &value.ProjectID, &value.ProjectKey, &value.RequestNumber, &value.ItemID, &value.ItemNumber, &value.Title, &value.Description, &value.Criteria, &value.TargetBranch, &pause, &value.CustomPrompt, &value.AgentRules, &value.ValidationCommands, &value.ForbiddenPaths, &value.AgentPrompts, &value.AgentID, &value.Timeout); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		value.PauseAfterCompletion = pause != 0
		value.PlanOnly = value.Kind == "task_plan"
		value.Workspace = filepath.Join(m.dataDir, "workspaces", safe(value.RequestID))
		stamp := now()
		result, err := tx.Exec("UPDATE agent_requests SET status='running',assigned_agent_id=?,started_at=?,workspace_path=? WHERE id=? AND status='queued'", value.AgentID, stamp, value.Workspace, value.RequestID)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count == 1 {
			claimed = &value
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
	if _, err := os.Stat(filepath.Join(claimed.Workspace, ".git")); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(claimed.Workspace), 0o700); err != nil {
		return err
	}
	var cloneURL string
	if err := m.store.DB.QueryRowContext(ctx, "SELECT COALESCE(g.clone_url,p.repository_url) FROM projects p LEFT JOIN project_repository_grants g ON g.project_id=p.id AND g.revoked_at IS NULL WHERE p.id=?", claimed.ProjectID).Scan(&cloneURL); err != nil {
		return err
	}
	credential, err := m.issue(ctx, claimed.ProjectID)
	if err != nil {
		return err
	}
	if credential != nil && credential.Revoke != nil {
		defer credential.Revoke(context.Background())
	}
	if err = git(ctx, "", credential, cloneArgs(cloneURL, claimed.Workspace)...); err != nil {
		return err
	}
	requestedBranch := strings.TrimSpace(claimed.TargetBranch)
	if requestedBranch != "" {
		exists, existsErr := gitRefExists(ctx, claimed.Workspace, "refs/remotes/origin/"+requestedBranch)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			if err = git(ctx, claimed.Workspace, nil, "switch", requestedBranch); err != nil {
				return err
			}
		} else {
			claimed.TargetBranch, err = gitOutput(ctx, claimed.Workspace, nil, "branch", "--show-current")
			if err != nil {
				return err
			}
			claimed.TargetBranch = strings.TrimSpace(claimed.TargetBranch)
		}
	}
	branch := fmt.Sprintf("projectboard/%s/%d", strings.ToLower(claimed.ProjectKey), claimed.ItemNumber)
	exists, err := gitRefExists(ctx, claimed.Workspace, "refs/remotes/origin/"+branch)
	if err != nil {
		return err
	}
	if exists {
		return git(ctx, claimed.Workspace, nil, "switch", "-c", branch, "--track", "origin/"+branch)
	}
	return git(ctx, claimed.Workspace, nil, "switch", "-c", branch)
}

func cloneArgs(cloneURL, workspace string) []string { return []string{"clone", cloneURL, workspace} }

func (m *Module) prompt(ctx context.Context, claimed *claim) string {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "You are executing one immutable ProjectBoard Agent Request.\nRequest: %s\nType: %s\nProject: %s\n", claimed.RequestID, claimed.Kind, claimed.ProjectKey)
	if claimed.ItemID != "" {
		fmt.Fprintf(&prompt, "Task: %s\nTitle: %s\nDescription:\n%s\nAcceptance criteria:\n%s\nDevelopment branch: %s\n", claimed.ItemID, claimed.Title, claimed.Description, claimed.Criteria, claimed.TargetBranch)
		prompt.WriteString("Default Git workflow: work on the already-created ProjectBoard branch, verify and commit, then let ProjectBoard push it. If the task conversation specifies a different workflow, follow that explicit instruction instead.\n")
	}
	fmt.Fprintf(&prompt, "Project Agent rules:\n%s\nProject prompt segments (JSON):\n%s\nValidation commands (JSON):\n%s\nForbidden paths (JSON):\n%s\n", claimed.AgentRules, claimed.AgentPrompts, claimed.ValidationCommands, claimed.ForbiddenPaths)
	if strings.TrimSpace(claimed.CustomPrompt) != "" {
		fmt.Fprintf(&prompt, "Request context:\n%s\n", claimed.CustomPrompt)
	}
	prompt.WriteString("The following project knowledge snapshot is read-only context. Do not edit the snapshot file directly.\n\n")
	prompt.WriteString(claimed.KnowledgeSnapshot)
	if claimed.PlanOnly {
		prompt.WriteString("\nProduce a concrete implementation plan only. Do not edit files. Return status planned.\n")
	} else if isKnowledgeKind(claimed.Kind) {
		prompt.WriteString("\nReturn complete knowledgeOperations. Each update/move/delete must include the current expectedVersion. The server applies the entire list transactionally; patches and partial writes are not accepted. Locked nodes cannot be changed, but children may be created below them. Return status completed.\n")
	} else {
		prompt.WriteString("\nImplement and verify the task, commit all task changes, and do not push. ProjectBoard will push the clean committed branch. Return completed only when genuinely complete.\n")
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
	snapshot, err := m.knowledge.Snapshot(ctx, claimed.ProjectID)
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
	result, err := m.runner.Run(ctx, Invocation{WorkDir: claimed.Workspace, Prompt: prompt, Timeout: time.Duration(claimed.Timeout) * time.Minute, PlanOnly: claimed.PlanOnly})
	if err != nil {
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
		return nil
	}
	if result.Status != "completed" {
		return errors.New("task execution did not return completed status")
	}
	if err = m.push(context.Background(), claimed); err != nil {
		return err
	}
	item, err = m.queue.Get(context.Background(), claimed.ItemID)
	if err != nil {
		return err
	}
	if item.Stage == "created" {
		item, err = m.queue.MoveStage(context.Background(), workqueue.Actor{Type: "agent", ID: claimed.AgentID}, item.ID, item.Version, "in_progress", "Agent execution started")
		if err != nil {
			return err
		}
	}
	if item.Stage == "in_progress" {
		item, err = m.queue.MoveStage(context.Background(), workqueue.Actor{Type: "agent", ID: claimed.AgentID}, item.ID, item.Version, "completed", "Agent execution completed")
		if err != nil {
			return err
		}
	}
	if !claimed.PauseAfterCompletion && item.Stage == "completed" {
		_, err = m.queue.MoveStage(context.Background(), workqueue.Actor{Type: "agent", ID: claimed.AgentID}, item.ID, item.Version, "closed", "Agent execution auto-closed task")
	}
	return err
}

func (m *Module) record(claimed *claim, result Result, status string, runErr error) {
	var errorMessage any
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	payload, _ := json.Marshal(map[string]any{"status": result.Status, "message": result.Message, "knowledgeOperations": result.KnowledgeOperations})
	_, _ = m.store.DB.Exec("UPDATE agent_requests SET status=?,thread_id=?,command_json=?,result_json=?,output_jsonl=?,final_message=?,ended_at=?,error_message=? WHERE id=? AND status='running'", status, nullable(result.ThreadID), defaultJSON(result.CommandJSON), string(payload), result.Raw, nullable(result.Message), now(), errorMessage, claimed.RequestID)
}
func (m *Module) fail(claimed *claim, result Result, err error) {
	if !m.active(claimed.RequestID) {
		return
	}
	m.record(claimed, result, "failed", err)
}
func (m *Module) active(requestID string) bool {
	var count int
	return m.store.DB.QueryRow("SELECT COUNT(*) FROM agent_requests WHERE id=? AND status='running'", requestID).Scan(&count) == nil && count == 1
}

func (m *Module) push(ctx context.Context, claimed *claim) error {
	status, err := gitOutput(ctx, claimed.Workspace, nil, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("Codex completed with uncommitted workspace changes")
	}
	credential, err := m.issue(ctx, claimed.ProjectID)
	if err != nil {
		return err
	}
	if credential != nil && credential.Revoke != nil {
		defer credential.Revoke(context.Background())
	}
	branch, err := gitOutput(ctx, claimed.Workspace, nil, "branch", "--show-current")
	if err != nil {
		return err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("Codex completed on a detached HEAD")
	}
	return git(ctx, claimed.Workspace, credential, "push", "-u", "origin", branch)
}

func (m *Module) issue(ctx context.Context, projectID string) (*Credential, error) {
	if m.credentials == nil {
		return nil, nil
	}
	return m.credentials.Issue(ctx, projectID)
}
func (m *Module) DeactivateAgent(ctx context.Context, agentID string) error {
	var waits []<-chan struct{}
	m.mu.Lock()
	for requestID, control := range m.running {
		var assigned string
		if m.store.DB.QueryRow("SELECT COALESCE(assigned_agent_id,'') FROM agent_requests WHERE id=?", requestID).Scan(&assigned) == nil && assigned == agentID {
			control.cancel()
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
	var stage, path string
	if err := m.store.DB.QueryRowContext(ctx, `SELECT w.stage,COALESCE((SELECT r.workspace_path FROM agent_requests r WHERE r.source_work_item_id=w.id AND r.workspace_path IS NOT NULL ORDER BY r.number DESC LIMIT 1),'') FROM work_items w WHERE w.id=?`, itemID).Scan(&stage, &path); err != nil {
		return err
	}
	if stage != "closed" {
		return errors.New("only closed task workspaces can be cleaned")
	}
	if path == "" {
		return nil
	}
	base, _ := filepath.Abs(filepath.Join(m.dataDir, "workspaces"))
	target, _ := filepath.Abs(path)
	relative, err := filepath.Rel(base, target)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return errors.New("workspace path is outside the managed directory")
	}
	if runtime.GOOS == "windows" {
		target = filepath.Clean(target)
	}
	if err = os.RemoveAll(target); err == nil {
		_, err = m.store.DB.ExecContext(ctx, "UPDATE agent_requests SET workspace_path=NULL WHERE source_work_item_id=? AND workspace_path=?", itemID, path)
	}
	return err
}

func gitRefExists(ctx context.Context, dir, ref string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git show-ref --verify --quiet %s: %w", ref, err)
}
func git(ctx context.Context, dir string, credential *Credential, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if credential != nil && credential.Token != "" {
		cmd.Env = append(os.Environ(), "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1=!f() { echo username=x-access-token; echo password=$PROJECTBOARD_GIT_TOKEN; }; f", "PROJECTBOARD_GIT_TOKEN="+credential.Token)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
func gitOutput(ctx context.Context, dir string, credential *Credential, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if credential != nil && credential.Token != "" {
		cmd.Env = append(os.Environ(), "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1=!f() { echo username=x-access-token; echo password=$PROJECTBOARD_GIT_TOKEN; }; f", "PROJECTBOARD_GIT_TOKEN="+credential.Token)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
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

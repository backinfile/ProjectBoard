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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/projectboard/projectboard/internal/projectrepo"
	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
)

type Invocation struct {
	WorkDir, Prompt, ThreadID string
	Timeout                   time.Duration
	PlanOnly                  bool
}
type Result struct{ ThreadID, Status, Message, Raw, CommandJSON string }
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
	result := Result{Raw: raw, ThreadID: in.ThreadID, CommandJSON: string(commandJSON)}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event["type"] {
		case "thread.started":
			if v, ok := event["thread_id"].(string); ok {
				result.ThreadID = v
			}
		case "item.completed":
			item, _ := event["item"].(map[string]any)
			if item["type"] == "agent_message" {
				if v, ok := item["text"].(string); ok {
					result.Message = v
				}
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
	var envelope struct{ Status, Message string }
	if json.Unmarshal([]byte(result.Message), &envelope) != nil {
		return result, errors.New("Codex returned invalid structured output")
	}
	result.Status = envelope.Status
	result.Message = envelope.Message
	if result.Status == "" || result.Message == "" {
		return result, errors.New("Codex result is missing status or message")
	}
	return result, nil
}

func execArgs(in Invocation, schemaPath string) []string {
	args := []string{"exec"}
	if in.ThreadID != "" {
		return append(args, "resume", in.ThreadID, "--json", "--output-schema", schemaPath, "-c", `sandbox_mode="danger-full-access"`, "-")
	}
	return append(args, "--json", "--sandbox", "danger-full-access", "--output-schema", schemaPath, "-C", in.WorkDir, "-")
}

type Options struct {
	DataDir      string
	Runner       Runner
	PollInterval time.Duration
}
type Module struct {
	store   *store.Store
	queue   *workqueue.Module
	dataDir string
	runner  Runner
	poll    time.Duration
	wake    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	running map[string]runControl
}

type runControl struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func New(database *store.Store, queue *workqueue.Module, options ...Options) *Module {
	var o Options
	if len(options) > 0 {
		o = options[0]
	}
	if o.DataDir == "" {
		o.DataDir = "./data"
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 5 * time.Second
	}
	if o.Runner == nil {
		o.Runner = &ExecRunner{Command: "codex", SchemaPath: filepath.Join(o.DataDir, "codex-result-schema.json")}
	}
	return &Module{store: database, queue: queue, dataDir: o.DataDir, runner: o.Runner, poll: o.PollInterval, wake: make(chan struct{}, 1), running: map[string]runControl{}}
}

const resultSchema = `{"type":"object","properties":{"status":{"type":"string","enum":["planned","completed","paused","failed"]},"message":{"type":"string"}},"required":["status","message"],"additionalProperties":false}`

func (m *Module) Start() error {
	if _, ok := m.runner.(*ExecRunner); ok {
		if err := os.WriteFile(filepath.Join(m.dataDir, "codex-result-schema.json"), []byte(resultSchema), 0o600); err != nil {
			return err
		}
	}
	type interrupted struct{ itemID, executionID string }
	interruptedRuns := []interrupted{}
	rows, queryErr := m.store.DB.Query(`SELECT w.id,COALESCE((SELECT e.id FROM agent_executions e WHERE e.work_item_id=w.id AND e.state='running' ORDER BY e.started_at DESC LIMIT 1),'') FROM work_items w WHERE w.agent_state='running'`)
	if queryErr == nil {
		for rows.Next() {
			var run interrupted
			if rows.Scan(&run.itemID, &run.executionID) == nil {
				interruptedRuns = append(interruptedRuns, run)
			}
		}
		rows.Close()
	}
	stamp := now()
	_, _ = m.store.DB.Exec("UPDATE agent_executions SET state='failed',ended_at=?,error_message='ProjectBoard restarted while Codex was running' WHERE state='running'", stamp)
	_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state='paused_failure',resume_requested=0,version=version+1,updated_at=? WHERE agent_state='running'", stamp)
	for _, run := range interruptedRuns {
		raw, _ := json.Marshal(map[string]any{"message": "ProjectBoard restarted while local Codex was running", "executionId": run.executionID})
		_, _ = m.store.DB.Exec("INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) SELECT ?,id,'agent_interrupted',stage,'system',NULL,?,version,? FROM work_items WHERE id=?", security.Token(18), string(raw), stamp, run.itemID)
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
	ExecutionID, ItemID, AgentID, ProjectID, ProjectKey, ProjectPath, Title, Description, Criteria string
	TargetBranch, WorkflowType, Phase, BaseCommit, ThreadID, Workspace                             string
	AgentRules, ValidationCommands, ForbiddenPaths, AgentPrompts                                   string
	Number, Attempt, Timeout                                                                       int64
	PlanOnly                                                                                       bool
}

func (m *Module) schedule() {
	for {
		c, err := m.claim()
		if err != nil || c == nil {
			return
		}
		ctx, cancel := context.WithCancel(m.ctx)
		control := runControl{cancel: cancel, done: make(chan struct{})}
		m.mu.Lock()
		m.running[c.ExecutionID] = control
		m.mu.Unlock()
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer func() { m.mu.Lock(); delete(m.running, c.ExecutionID); close(control.done); m.mu.Unlock(); m.Wake() }()
			m.execute(ctx, c)
		}()
	}
}

func (m *Module) claim() (*claim, error) {
	var c *claim
	err := m.store.Write(context.Background(), func(tx *sql.Tx) error {
		row := tx.QueryRow(`SELECT w.id,w.project_id,p.project_key,p.project_path,w.number,w.title,w.description_markdown,w.acceptance_criteria_markdown,w.target_branch,w.workflow_type,w.agent_phase,COALESCE(w.base_commit_sha,''),COALESCE(w.codex_thread_id,''),COALESCE(w.workspace_path,''),p.agent_rules_markdown,p.validation_commands_json,p.forbidden_paths_json,p.agent_prompts_json,a.id,a.turn_timeout_minutes,
		(SELECT COALESCE(MAX(e.attempt_number),0)+1 FROM agent_executions e WHERE e.work_item_id=w.id),
		CASE WHEN w.agent_phase='work' AND w.pause_after_plan=1 AND w.plan_pause_consumed=0 THEN 1 ELSE 0 END
		FROM work_items w JOIN projects p ON p.id=w.project_id CROSS JOIN agents a
		WHERE w.is_agent_task=1 AND w.blocked_at IS NULL AND w.agent_state='queued'
		AND ((w.agent_phase='work' AND w.stage IN('created','in_progress')) OR
			(w.workflow_type='standard' AND w.agent_phase='merge' AND w.stage='completed'))
		AND a.status='active' AND a.revoked_at IS NULL
		AND (SELECT COUNT(*) FROM agent_executions e WHERE e.agent_id=a.id AND e.state='running')<a.max_concurrent_tasks
		AND NOT EXISTS (
			SELECT 1 FROM work_item_tags wt JOIN json_each(a.reject_tags_json) rejected
			WHERE wt.work_item_id=w.id AND lower(wt.tag)=lower(CAST(rejected.value AS TEXT))
		)
		AND (json_array_length(a.accept_tags_json)=0 OR EXISTS (
			SELECT 1 FROM work_item_tags wt JOIN json_each(a.accept_tags_json) accepted
			WHERE wt.work_item_id=w.id AND lower(wt.tag)=lower(CAST(accepted.value AS TEXT))
		))
		AND ((w.workflow_type='standard' AND w.agent_phase<>'merge') OR NOT EXISTS (
			SELECT 1 FROM work_items active
			WHERE active.project_id=w.project_id AND active.agent_state='running'
			AND (active.workflow_type='simple_conversation' OR active.agent_phase='merge')
		))
		ORDER BY (CAST((SELECT COUNT(*) FROM agent_executions e WHERE e.agent_id=a.id AND e.state='running') AS REAL)/a.max_concurrent_tasks),a.created_at,a.id,w.created_at,w.id LIMIT 1`)
		var v claim
		var plan int
		if err := row.Scan(&v.ItemID, &v.ProjectID, &v.ProjectKey, &v.ProjectPath, &v.Number, &v.Title, &v.Description, &v.Criteria, &v.TargetBranch, &v.WorkflowType, &v.Phase, &v.BaseCommit, &v.ThreadID, &v.Workspace, &v.AgentRules, &v.ValidationCommands, &v.ForbiddenPaths, &v.AgentPrompts, &v.AgentID, &v.Timeout, &v.Attempt, &plan); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		v.PlanOnly = plan != 0
		v.ExecutionID = security.Token(18)
		if v.WorkflowType == "simple_conversation" || v.Phase == "merge" {
			v.Workspace = v.ProjectPath
		} else if v.Workspace == "" {
			v.Workspace = projectrepo.WorktreePath(v.ProjectPath, v.ProjectKey, v.Number)
		}
		stamp := now()
		result, err := tx.Exec("UPDATE work_items SET stage=CASE WHEN stage='created' THEN 'in_progress' ELSE stage END,agent_state='running',assigned_agent_id=?,resume_requested=0,version=version+1,updated_at=? WHERE id=? AND agent_state='queued'", v.AgentID, stamp, v.ItemID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return nil
		}
		_, err = tx.Exec("INSERT INTO agent_executions(id,work_item_id,agent_id,attempt_number,state,prompt_markdown,workspace_path,started_at) VALUES(?,?,?,?,?,?,?,?)", v.ExecutionID, v.ItemID, v.AgentID, v.Attempt, "running", "pending", v.Workspace, stamp)
		if err == nil {
			c = &v
		}
		return err
	})
	return c, err
}
func (m *Module) prepare(ctx context.Context, c *claim) error {
	if c.WorkflowType == "simple_conversation" || c.Phase == "merge" {
		status, err := projectrepo.Status(ctx, c.ProjectPath)
		if err != nil {
			return err
		}
		if status != "" {
			return errors.New("project repository must be clean before writing its target branch")
		}
		branch, err := projectrepo.CurrentBranch(ctx, c.ProjectPath)
		if err != nil {
			return err
		}
		if branch != c.TargetBranch {
			if err = projectrepo.Checkout(ctx, c.ProjectPath, c.TargetBranch); err != nil {
				return err
			}
		}
		c.Workspace = c.ProjectPath
		_, err = m.store.DB.ExecContext(ctx, "UPDATE work_items SET workspace_path=? WHERE id=?", c.Workspace, c.ItemID)
		return err
	}
	workspace, err := projectrepo.EnsureWorktree(ctx, c.ProjectPath, c.ProjectKey, c.Number, c.TargetBranch)
	if err != nil {
		return err
	}
	c.Workspace = workspace.Path
	if c.BaseCommit == "" {
		c.BaseCommit = workspace.BaseCommit
	}
	_, err = m.store.DB.ExecContext(ctx, "UPDATE work_items SET workspace_path=?,base_commit_sha=COALESCE(base_commit_sha,?) WHERE id=?", c.Workspace, c.BaseCommit, c.ItemID)
	return err
}
func (m *Module) prompt(ctx context.Context, c *claim) string {
	item, _ := m.queue.Get(ctx, c.ItemID)
	var b strings.Builder
	fmt.Fprintf(&b, "You are the local Codex Agent for ProjectBoard task %s.\nTitle: %s\nDescription:\n%s\nAcceptance criteria:\n%s\nTarget branch: %s\nWorkflow: %s\n", c.ItemID, c.Title, c.Description, c.Criteria, c.TargetBranch, c.WorkflowType)
	fmt.Fprintf(&b, "Project Agent rules:\n%s\nProject prompt segments (JSON):\n%s\nValidation commands (JSON):\n%s\nForbidden paths (JSON):\n%s\n", c.AgentRules, c.AgentPrompts, c.ValidationCommands, c.ForbiddenPaths)
	if c.PlanOnly {
		b.WriteString("Produce a concrete implementation plan only. Do not edit files. Return status planned.\n")
	} else if c.Phase == "merge" {
		workBranch := fmt.Sprintf("projectboard/%s/%d", strings.ToLower(c.ProjectKey), c.Number)
		fmt.Fprintf(&b, "Complete this task by merging %s into the currently checked out target branch %s with --no-ff. Use merge commit message %q. Resolve conflicts without discarding unrelated human changes and verify the repository. Do not fetch, pull, or push unless the task description or conversation explicitly requests that network operation. Return completed only when the work branch is fully merged and the repository is clean.\n", workBranch, c.TargetBranch, "merge: "+c.ItemID+" "+c.Title)
	} else if c.WorkflowType == "simple_conversation" {
		b.WriteString("Work directly on the checked out project target branch. Implement, verify, and commit the task changes locally. Do not fetch, pull, or push unless the task description or conversation explicitly requests that network operation. Return completed only when the repository is clean and the task is genuinely complete.\n")
	} else {
		b.WriteString("Implement and verify the task in this isolated worktree, then commit all task changes on the current task branch. Do not merge the target branch. Do not fetch, pull, or push unless the task description or conversation explicitly requests that network operation. Return completed only when the worktree is clean and the task is genuinely ready to merge; otherwise return paused or failed.\n")
	}
	b.WriteString("Recent ProjectBoard conversation:\n")
	for _, entry := range item.Conversation {
		raw, _ := json.Marshal(entry)
		b.Write(raw)
		b.WriteByte('\n')
	}
	b.WriteString("Your final response must follow the supplied JSON schema. Put the user-facing Markdown in message.\n")
	return b.String()
}
func (m *Module) execute(parent context.Context, c *claim) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(c.Timeout)*time.Minute)
	defer cancel()
	if err := m.prepare(ctx, c); err != nil {
		m.recordResult(c, Result{ThreadID: c.ThreadID}, "failed", err)
		m.fail(c, c.ThreadID, err)
		return
	}
	prompt := m.prompt(ctx, c)
	_, _ = m.store.DB.Exec("UPDATE agent_executions SET prompt_markdown=? WHERE id=?", prompt, c.ExecutionID)
	_ = m.event(c, "agent_started", "Local Codex execution started")
	result, err := m.runner.Run(ctx, Invocation{WorkDir: c.Workspace, Prompt: prompt, ThreadID: c.ThreadID, Timeout: time.Duration(c.Timeout) * time.Minute, PlanOnly: c.PlanOnly})
	if err != nil {
		m.recordResult(c, result, "failed", err)
		m.fail(c, result.ThreadID, err)
		return
	}
	m.recordResult(c, result, result.Status, nil)
	m.finish(c, result)
}
func (m *Module) recordResult(c *claim, r Result, state string, runErr error) {
	var msg any
	if runErr != nil {
		msg = runErr.Error()
	}
	payload, _ := json.Marshal(map[string]any{"status": r.Status, "message": r.Message})
	_, _ = m.store.DB.Exec("UPDATE agent_executions SET state=?,thread_id=?,command_json=?,result_json=?,output_jsonl=?,final_message=?,ended_at=?,error_message=? WHERE id=?", state, nullable(r.ThreadID), defaultJSON(r.CommandJSON), string(payload), r.Raw, nullable(r.Message), now(), msg, c.ExecutionID)
}
func (m *Module) fail(c *claim, threadID string, err error) {
	if !m.active(c) {
		return
	}
	_ = m.event(c, "agent_failed", err.Error())
	_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state='paused_failure',codex_thread_id=COALESCE(?,codex_thread_id),workspace_path=?,version=version+1,updated_at=? WHERE id=?", nullable(threadID), c.Workspace, now(), c.ItemID)
}
func (m *Module) finish(c *claim, r Result) {
	if !m.active(c) {
		return
	}
	if r.Message != "" {
		_ = m.agentMessage(c, r.Message)
	}
	stamp := now()
	switch r.Status {
	case "planned":
		_ = m.event(c, "agent_plan_paused", "Agent completed the plan and is waiting for confirmation")
		_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state='paused_plan',plan_pause_consumed=1,codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", r.ThreadID, c.Workspace, stamp, c.ItemID)
	case "completed":
		status, err := projectrepo.Status(context.Background(), c.Workspace)
		if err != nil || status != "" {
			if err == nil {
				err = errors.New("Codex completed with uncommitted workspace changes")
			}
			m.recordResult(c, r, "failed", err)
			m.fail(c, r.ThreadID, err)
			return
		}
		var pending, pause int
		_ = m.store.DB.QueryRow("SELECT resume_requested,pause_before_completion FROM work_items WHERE id=?", c.ItemID).Scan(&pending, &pause)
		if pending != 0 {
			_ = m.event(c, "agent_resumed", "New member messages arrived while Codex was running; continuing implementation")
			_, _ = m.store.DB.Exec("UPDATE work_items SET stage='in_progress',completed_at=NULL,agent_phase='work',agent_state='queued',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", r.ThreadID, c.Workspace, stamp, c.ItemID)
			return
		}
		if c.Phase == "merge" {
			workBranch := fmt.Sprintf("projectboard/%s/%d", strings.ToLower(c.ProjectKey), c.Number)
			merged, mergeErr := projectrepo.IsAncestor(context.Background(), c.ProjectPath, workBranch, c.TargetBranch)
			if mergeErr != nil || !merged {
				if mergeErr == nil {
					mergeErr = errors.New("task branch is not merged into the target branch")
				}
				m.recordResult(c, r, "failed", mergeErr)
				m.fail(c, r.ThreadID, mergeErr)
				return
			}
			workHead, headErr := projectrepo.ResolveRevision(context.Background(), c.ProjectPath, workBranch)
			if headErr != nil {
				m.recordResult(c, r, "failed", headErr)
				m.fail(c, r.ThreadID, headErr)
				return
			}
			if workHead != c.BaseCommit {
				var mergeCommit bool
				mergeCommit, mergeErr = projectrepo.HasSecondParent(context.Background(), c.ProjectPath, c.TargetBranch)
				if mergeErr != nil || !mergeCommit {
					if mergeErr == nil {
						mergeErr = errors.New("task changes were not integrated with a --no-ff merge commit")
					}
					m.recordResult(c, r, "failed", mergeErr)
					m.fail(c, r.ThreadID, mergeErr)
					return
				}
			}
			head, _ := projectrepo.Head(context.Background(), c.ProjectPath)
			m.recordCommitEvidence(c, head, "merge: "+c.ItemID+" "+c.Title)
			m.closeTask(c, r.ThreadID, stamp)
			return
		}
		if c.WorkflowType == "simple_conversation" {
			if pause != 0 {
				_ = m.event(c, "agent_completion_paused", "Agent completed its work and is waiting for confirmation before closing")
				_, _ = m.store.DB.Exec("UPDATE work_items SET agent_phase='close_review',agent_state='paused_completion',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", r.ThreadID, c.Workspace, stamp, c.ItemID)
				return
			}
			head, _ := projectrepo.Head(context.Background(), c.ProjectPath)
			m.recordCommitEvidence(c, head, c.Title)
			m.closeTask(c, r.ThreadID, stamp)
			return
		}
		if pause != 0 {
			_ = m.event(c, "agent_completion_paused", "Agent completed implementation and is waiting for merge approval")
			_, _ = m.store.DB.Exec("UPDATE work_items SET stage='completed',completed_at=?,agent_phase='merge_review',agent_state='paused_completion',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", stamp, r.ThreadID, c.Workspace, stamp, c.ItemID)
			return
		}
		_, _ = m.store.DB.Exec("UPDATE work_items SET stage='completed',completed_at=?,agent_phase='merge',agent_state='queued',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", stamp, r.ThreadID, c.Workspace, stamp, c.ItemID)
		m.Wake()
	case "paused":
		_ = m.event(c, "agent_paused", r.Message)
		_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state='paused_failure',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", r.ThreadID, c.Workspace, stamp, c.ItemID)
	default:
		m.fail(c, r.ThreadID, errors.New("Agent returned failed status"))
	}
}

func (m *Module) closeTask(c *claim, threadID, stamp string) {
	_ = m.event(c, "agent_closed", "Agent completed and closed the task")
	workspace := any(c.Workspace)
	if c.WorkflowType == "simple_conversation" {
		workspace = nil
	}
	_, _ = m.store.DB.Exec("UPDATE work_items SET stage='closed',agent_state='finished',completed_at=?,closed_at=?,codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", stamp, stamp, threadID, workspace, stamp, c.ItemID)
}

func (m *Module) recordCommitEvidence(c *claim, sha, message string) {
	if sha == "" {
		return
	}
	_, _ = m.store.DB.Exec(`INSERT OR IGNORE INTO git_commit_evidence(id,work_item_id,repository_id,commit_sha,message,branch,files_json,created_at) VALUES(?,?,?,?,?,?,'[]',?)`, security.Token(18), c.ItemID, c.ProjectID, sha, message, c.TargetBranch, now())
}

func (m *Module) active(c *claim) bool {
	var count int
	return m.store.DB.QueryRow("SELECT COUNT(*) FROM work_items WHERE id=? AND assigned_agent_id=? AND agent_state='running'", c.ItemID, c.AgentID).Scan(&count) == nil && count == 1
}
func (m *Module) agentMessage(c *claim, message string) error {
	raw, _ := json.Marshal(map[string]any{"markdown": message})
	_, err := m.store.DB.Exec("INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) SELECT ?,id,'message',stage,'agent',?, ?,version,? FROM work_items WHERE id=?", security.Token(18), c.AgentID, string(raw), now(), c.ItemID)
	return err
}
func (m *Module) event(c *claim, kind, message string) error {
	raw, _ := json.Marshal(map[string]any{"message": message, "executionId": c.ExecutionID})
	_, err := m.store.DB.Exec("INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) SELECT ?,id,?,stage,'system',NULL,?,version,? FROM work_items WHERE id=?", security.Token(18), kind, string(raw), now(), c.ItemID)
	return err
}

func (m *Module) DeactivateAgent(ctx context.Context, agentID string) error {
	var waits []<-chan struct{}
	m.mu.Lock()
	for executionID, control := range m.running {
		var id string
		if m.store.DB.QueryRow("SELECT agent_id FROM agent_executions WHERE id=?", executionID).Scan(&id) == nil && id == agentID {
			control.cancel()
			waits = append(waits, control.done)
		}
	}
	m.mu.Unlock()
	if err := waitForRuns(ctx, waits); err != nil {
		return err
	}
	stamp := now()
	_, err := m.store.DB.ExecContext(ctx, "UPDATE work_items SET stage='created',agent_state='queued',assigned_agent_id=NULL,codex_thread_id=NULL,resume_requested=0,version=version+1,updated_at=? WHERE assigned_agent_id=? AND stage<>'closed'", stamp, agentID)
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
	_, err := m.store.DB.ExecContext(ctx, "UPDATE work_items SET workspace_path=NULL,version=version+1,updated_at=? WHERE id=?", now(), itemID)
	return err
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func nullable(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
func defaultJSON(v string) string {
	if strings.TrimSpace(v) == "" {
		return "[]"
	}
	return v
}
func ParsePositive(raw string, fallback int) int {
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

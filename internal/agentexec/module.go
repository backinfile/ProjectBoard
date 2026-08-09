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

	"github.com/projectboard/projectboard/internal/security"
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
	Credentials  CredentialProvider
	PollInterval time.Duration
}
type Module struct {
	store       *store.Store
	queue       *workqueue.Module
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
	return &Module{store: database, queue: queue, dataDir: o.DataDir, runner: o.Runner, credentials: o.Credentials, poll: o.PollInterval, wake: make(chan struct{}, 1), running: map[string]runControl{}}
}

const resultSchema = `{"type":"object","properties":{"status":{"type":"string","enum":["planned","completed","paused","failed"]},"message":{"type":"string"}},"required":["status","message"],"additionalProperties":false}`

func (m *Module) Start() error {
	if err := os.MkdirAll(filepath.Join(m.dataDir, "workspaces"), 0o700); err != nil {
		return err
	}
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
	ExecutionID, ItemID, AgentID, ProjectID, ProjectKey, Title, Description, Criteria, TargetBranch, ThreadID, Workspace string
	AgentRules, ValidationCommands, ForbiddenPaths, AgentPrompts                                                         string
	Number, Attempt, Timeout                                                                                             int64
	PlanOnly                                                                                                             bool
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
		row := tx.QueryRow(`SELECT w.id,w.project_id,p.project_key,w.number,w.title,w.description_markdown,w.acceptance_criteria_markdown,w.target_branch,COALESCE(w.codex_thread_id,''),COALESCE(w.workspace_path,''),p.agent_rules_markdown,p.validation_commands_json,p.forbidden_paths_json,p.agent_prompts_json,a.id,a.turn_timeout_minutes,
		(SELECT COALESCE(MAX(e.attempt_number),0)+1 FROM agent_executions e WHERE e.work_item_id=w.id),CASE WHEN w.pause_after_plan=1 AND w.plan_pause_consumed=0 THEN 1 ELSE 0 END
		FROM work_items w JOIN projects p ON p.id=w.project_id CROSS JOIN agents a
		WHERE w.is_agent_task=1 AND w.blocked_at IS NULL AND w.stage IN('created','in_progress') AND w.agent_state='queued' AND a.status='active' AND a.revoked_at IS NULL
		AND (SELECT COUNT(*) FROM agent_executions e WHERE e.agent_id=a.id AND e.state='running')<a.max_concurrent_tasks
		ORDER BY (CAST((SELECT COUNT(*) FROM agent_executions e WHERE e.agent_id=a.id AND e.state='running') AS REAL)/a.max_concurrent_tasks),a.created_at,a.id,w.created_at,w.id LIMIT 1`)
		var v claim
		var plan int
		if err := row.Scan(&v.ItemID, &v.ProjectID, &v.ProjectKey, &v.Number, &v.Title, &v.Description, &v.Criteria, &v.TargetBranch, &v.ThreadID, &v.Workspace, &v.AgentRules, &v.ValidationCommands, &v.ForbiddenPaths, &v.AgentPrompts, &v.AgentID, &v.Timeout, &v.Attempt, &plan); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		v.PlanOnly = plan != 0
		v.ExecutionID = security.Token(18)
		stamp := now()
		result, err := tx.Exec("UPDATE work_items SET stage='in_progress',agent_state='running',assigned_agent_id=?,resume_requested=0,version=version+1,updated_at=? WHERE id=? AND agent_state='queued'", v.AgentID, stamp, v.ItemID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return nil
		}
		workspace := v.Workspace
		if workspace == "" {
			workspace = filepath.Join(m.dataDir, "workspaces", safe(v.ItemID))
		}
		v.Workspace = workspace
		_, err = tx.Exec("INSERT INTO agent_executions(id,work_item_id,agent_id,attempt_number,state,prompt_markdown,workspace_path,started_at) VALUES(?,?,?,?,?,?,?,?)", v.ExecutionID, v.ItemID, v.AgentID, v.Attempt, "running", "pending", workspace, stamp)
		if err == nil {
			c = &v
		}
		return err
	})
	return c, err
}

func safe(v string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
	return r.Replace(strings.ToLower(v))
}
func (m *Module) prepare(ctx context.Context, c *claim) error {
	if _, err := os.Stat(filepath.Join(c.Workspace, ".git")); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.Workspace), 0o700); err != nil {
		return err
	}
	var cloneURL string
	if err := m.store.DB.QueryRowContext(ctx, "SELECT COALESCE(g.clone_url,p.repository_url) FROM projects p LEFT JOIN project_repository_grants g ON g.project_id=p.id AND g.revoked_at IS NULL WHERE p.id=?", c.ProjectID).Scan(&cloneURL); err != nil {
		return err
	}
	cred, err := m.issue(ctx, c.ProjectID)
	if err != nil {
		return err
	}
	if cred != nil && cred.Revoke != nil {
		defer cred.Revoke(context.Background())
	}
	// Let the remote select its default branch. The task's development branch is
	// workflow metadata and may be renamed or created later; using it as a clone
	// constraint makes workspace creation fail before the Agent can do any work.
	args := cloneArgs(cloneURL, c.Workspace)
	if err = git(ctx, "", cred, args...); err != nil {
		return err
	}
	requestedBranch := strings.TrimSpace(c.TargetBranch)
	if requestedBranch != "" {
		exists, existsErr := gitRefExists(ctx, c.Workspace, "refs/remotes/origin/"+requestedBranch)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			if err = git(ctx, c.Workspace, nil, "switch", requestedBranch); err != nil {
				return err
			}
		} else {
			c.TargetBranch, err = gitOutput(ctx, c.Workspace, nil, "branch", "--show-current")
			if err != nil {
				return err
			}
			c.TargetBranch = strings.TrimSpace(c.TargetBranch)
		}
	}
	branch := fmt.Sprintf("projectboard/%s/%d", strings.ToLower(c.ProjectKey), c.Number)
	return git(ctx, c.Workspace, nil, "switch", "-c", branch)
}

func cloneArgs(cloneURL, workspace string) []string {
	return []string{"clone", cloneURL, workspace}
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

func git(ctx context.Context, dir string, cred *Credential, args ...string) error {
	full := []string{}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	cmd.Dir = dir
	if cred != nil && cred.Token != "" {
		cmd.Env = append(os.Environ(), "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1=!f() { echo username=x-access-token; echo password=$PROJECTBOARD_GIT_TOKEN; }; f", "PROJECTBOARD_GIT_TOKEN="+cred.Token)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
func (m *Module) issue(ctx context.Context, projectID string) (*Credential, error) {
	if m.credentials == nil {
		return nil, nil
	}
	return m.credentials.Issue(ctx, projectID)
}

func (m *Module) prompt(ctx context.Context, c *claim) string {
	item, _ := m.queue.Get(ctx, c.ItemID)
	var b strings.Builder
	fmt.Fprintf(&b, "You are the local Codex Agent for ProjectBoard task %s.\nTitle: %s\nDescription:\n%s\nAcceptance criteria:\n%s\nDevelopment branch: %s\n", c.ItemID, c.Title, c.Description, c.Criteria, c.TargetBranch)
	b.WriteString("Default Git workflow: start from the development branch, do the task on the already-created ProjectBoard work branch, and after verification merge the work branch back into the development branch. Leave the branch that should be published checked out. If the task description or ProjectBoard conversation explicitly specifies a different branch or Git workflow, follow that explicit instruction instead.\n")
	fmt.Fprintf(&b, "Project Agent rules:\n%s\nProject prompt segments (JSON):\n%s\nValidation commands (JSON):\n%s\nForbidden paths (JSON):\n%s\n", c.AgentRules, c.AgentPrompts, c.ValidationCommands, c.ForbiddenPaths)
	if c.PlanOnly {
		b.WriteString("Produce a concrete implementation plan only. Do not edit files. Return status planned.\n")
	} else {
		b.WriteString("Implement and verify the task in this workspace, then commit all task changes on the current branch. Do not push; ProjectBoard will push the clean committed branch. Return completed only when the task is genuinely complete; otherwise return paused or failed.\n")
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
		var pending, pause int
		_ = m.store.DB.QueryRow("SELECT resume_requested,pause_after_completion FROM work_items WHERE id=?", c.ItemID).Scan(&pending, &pause)
		if pending != 0 {
			_ = m.event(c, "agent_resumed", "New member messages arrived while Codex was running; continuing the session")
			_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state='queued',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", r.ThreadID, c.Workspace, stamp, c.ItemID)
			return
		}
		if err := m.push(context.Background(), c); err != nil {
			m.recordResult(c, r, "failed", err)
			m.fail(c, r.ThreadID, err)
			return
		}
		if pause != 0 {
			_ = m.event(c, "agent_completion_paused", "Agent completed the task and is waiting for human closure")
			_, _ = m.store.DB.Exec("UPDATE work_items SET stage='completed',agent_state='paused_completion',completed_at=?,codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", stamp, r.ThreadID, c.Workspace, stamp, c.ItemID)
		} else {
			_ = m.event(c, "agent_closed", "Agent completed and automatically closed the task")
			_, _ = m.store.DB.Exec("UPDATE work_items SET stage='closed',agent_state='finished',completed_at=?,closed_at=?,codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", stamp, stamp, r.ThreadID, c.Workspace, stamp, c.ItemID)
		}
	case "paused":
		_ = m.event(c, "agent_paused", r.Message)
		_, _ = m.store.DB.Exec("UPDATE work_items SET agent_state='paused_failure',codex_thread_id=?,workspace_path=?,version=version+1,updated_at=? WHERE id=?", r.ThreadID, c.Workspace, stamp, c.ItemID)
	default:
		m.fail(c, r.ThreadID, errors.New("Agent returned failed status"))
	}
}
func (m *Module) active(c *claim) bool {
	var count int
	return m.store.DB.QueryRow("SELECT COUNT(*) FROM work_items WHERE id=? AND assigned_agent_id=? AND agent_state='running'", c.ItemID, c.AgentID).Scan(&count) == nil && count == 1
}
func (m *Module) push(ctx context.Context, c *claim) error {
	status, err := gitOutput(ctx, c.Workspace, nil, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("Codex completed with uncommitted workspace changes")
	}
	cred, err := m.issue(ctx, c.ProjectID)
	if err != nil {
		return err
	}
	if cred != nil && cred.Revoke != nil {
		defer cred.Revoke(context.Background())
	}
	branch, err := gitOutput(ctx, c.Workspace, nil, "branch", "--show-current")
	if err != nil {
		return err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("Codex completed on a detached HEAD")
	}
	return git(ctx, c.Workspace, cred, "push", "-u", "origin", branch)
}

func gitOutput(ctx context.Context, dir string, cred *Credential, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if cred != nil && cred.Token != "" {
		cmd.Env = append(os.Environ(), "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1=!f() { echo username=x-access-token; echo password=$PROJECTBOARD_GIT_TOKEN; }; f", "PROJECTBOARD_GIT_TOKEN="+cred.Token)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
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
	var stage, path string
	if err := m.store.DB.QueryRowContext(ctx, "SELECT stage,COALESCE(workspace_path,'') FROM work_items WHERE id=?", itemID).Scan(&stage, &path); err != nil {
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
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return errors.New("workspace path is outside the managed directory")
	}
	if runtime.GOOS == "windows" {
		target = filepath.Clean(target)
	}
	if err = os.RemoveAll(target); err == nil {
		_, err = m.store.DB.ExecContext(ctx, "UPDATE work_items SET workspace_path=NULL,version=version+1,updated_at=? WHERE id=?", now(), itemID)
	}
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

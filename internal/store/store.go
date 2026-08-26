package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/domain"
	"github.com/projectboard/projectboard/internal/ids"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type Store struct {
	db   *sql.DB
	path string
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path != ":memory:" {
		path = filepath.Clean(path)
		if err := backupBeforeMigration(path); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS projects (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', path TEXT NOT NULL,
 normalized_path TEXT NOT NULL UNIQUE, color TEXT NOT NULL DEFAULT '#c9f24e', default_branch TEXT NOT NULL DEFAULT 'main',
 default_agent_id TEXT NOT NULL DEFAULT '', archived INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
 id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), task_key TEXT NOT NULL,
 title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, run_status TEXT NOT NULL,
 priority TEXT NOT NULL DEFAULT 'medium', labels_json TEXT NOT NULL DEFAULT '[]', acceptance_json TEXT NOT NULL DEFAULT '[]',
 due_at TEXT, version INTEGER NOT NULL DEFAULT 1, deleted_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(project_id, task_key)
);
CREATE TABLE IF NOT EXISTS agents (
 id TEXT PRIMARY KEY, project_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, model TEXT NOT NULL DEFAULT '',
 reasoning TEXT NOT NULL DEFAULT 'medium', system_prompt TEXT NOT NULL DEFAULT '', sandbox TEXT NOT NULL DEFAULT 'workspace-write',
 network INTEGER NOT NULL DEFAULT 0, is_default INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conversations (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), agent_id TEXT NOT NULL REFERENCES agents(id),
 title TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '', codex_thread_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(task_id, agent_id)
);
CREATE TABLE IF NOT EXISTS messages (
 id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id), role TEXT NOT NULL,
 kind TEXT NOT NULL DEFAULT 'text', content TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS runs (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), conversation_id TEXT NOT NULL REFERENCES conversations(id),
 agent_id TEXT NOT NULL REFERENCES agents(id), workspace_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
 prompt TEXT NOT NULL, started_at TEXT, finished_at TEXT, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS task_workspaces (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), path TEXT NOT NULL, branch TEXT NOT NULL,
 base_commit TEXT NOT NULL, kind TEXT NOT NULL, parent_id TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'ready',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS parallel_plans (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), main_agent_id TEXT NOT NULL, prompt TEXT NOT NULL,
 agents_json TEXT NOT NULL, authorized_children INTEGER NOT NULL, snapshot_commit TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'proposed', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS candidate_changes (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), workspace_id TEXT NOT NULL REFERENCES task_workspaces(id),
 commit_hash TEXT NOT NULL, patch TEXT NOT NULL, summary TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
 created_at TEXT NOT NULL, decided_at TEXT
);
CREATE TABLE IF NOT EXISTS approvals (
 id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id), method TEXT NOT NULL, detail_json TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending', decision TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, decided_at TEXT
);
CREATE TABLE IF NOT EXISTS run_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL REFERENCES runs(id), event_type TEXT NOT NULL,
 payload_json TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS knowledge_versions (
 id TEXT PRIMARY KEY, knowledge_id TEXT NOT NULL REFERENCES knowledge(id), version INTEGER NOT NULL, content TEXT NOT NULL,
 summary TEXT NOT NULL, source_run_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'accepted', created_at TEXT NOT NULL,
 UNIQUE(knowledge_id, version)
);
CREATE TABLE IF NOT EXISTS capability_tokens (
 token_hash TEXT PRIMARY KEY, project_id TEXT NOT NULL, task_id TEXT NOT NULL, run_id TEXT NOT NULL,
 permissions_json TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT
);
CREATE TABLE IF NOT EXISTS knowledge (
 id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), title TEXT NOT NULL, content TEXT NOT NULL,
 summary TEXT NOT NULL DEFAULT '', category TEXT NOT NULL DEFAULT 'general', permission TEXT NOT NULL DEFAULT 'suggest',
 locked INTEGER NOT NULL DEFAULT 0, sensitive INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
 deleted_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS search_fts USING fts5(entity_type, entity_id UNINDEXED, project_id UNINDEXED, title, body);
CREATE TABLE IF NOT EXISTS notifications (
 id TEXT PRIMARY KEY, project_id TEXT NOT NULL DEFAULT '', type TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL,
 entity_id TEXT NOT NULL DEFAULT '', read_at TEXT, snoozed_until TEXT, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS automation_rules (
 id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), name TEXT NOT NULL, trigger_type TEXT NOT NULL,
 conditions_json TEXT NOT NULL DEFAULT '{}', actions_json TEXT NOT NULL DEFAULT '[]', enabled INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS templates (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', body_json TEXT NOT NULL,
 builtin INTEGER NOT NULL DEFAULT 0, archived INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_events (
 id TEXT PRIMARY KEY, project_id TEXT NOT NULL DEFAULT '', task_id TEXT NOT NULL DEFAULT '', run_id TEXT NOT NULL DEFAULT '',
 action TEXT NOT NULL, actor TEXT NOT NULL, detail_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at TEXT NOT NULL);
INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(1,datetime('now'));
`)
	if err != nil {
		return err
	}
	return s.seed(ctx)
}

func (s *Store) seed(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO agents
 (id, name, model, reasoning, system_prompt, sandbox, network, is_default, created_at)
 VALUES ('agent_codex_default','Codex · 自动实现','', 'high', '围绕任务验收标准工作，修改后运行验证并报告风险。','workspace-write',1,1,?)`, now)
	if err != nil {
		return err
	}
	for _, tpl := range []struct{ id, kind, name, desc, body string }{
		{"tpl_feature", "task", "实现功能", "从需求到验证的功能任务", `{"priority":"high","acceptance":["功能满足需求","测试通过","修改已审核"]}`},
		{"tpl_bug", "task", "修复 Bug", "诊断、复现并修复问题", `{"priority":"high","acceptance":["问题可复现","回归测试通过"]}`},
		{"tpl_research", "task", "调研方案", "比较方案并形成可执行结论", `{"priority":"medium","acceptance":["记录约束","给出推荐方案"]}`},
		{"tpl_project", "project", "软件研发项目", "任务—Agent—验证—知识闭环", `{"defaultBranch":"main"}`},
	} {
		_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO templates(id,kind,name,description,body_json,builtin,created_at,updated_at) VALUES(?,?,?,?,?,1,?,?)`, tpl.id, tpl.kind, tpl.name, tpl.desc, tpl.body, now, now)
		if err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key,value_json,updated_at) VALUES('global','{"concurrency":10,"listenAddress":"127.0.0.1:5173","allowedOrigins":[],"notificationEnabled":false,"runRetentionDays":90,"trashRetentionDays":30}',?)`, now)
	return err
}

func normalizePath(path string) string {
	return strings.ToLower(filepath.Clean(strings.TrimSpace(path)))
}

func (s *Store) CreateProject(ctx context.Context, p domain.Project) (domain.Project, error) {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Path) == "" {
		return p, fmt.Errorf("name and path are required")
	}
	if p.ID == "" {
		p.ID = ids.New("prj")
	}
	if p.Color == "" {
		p.Color = "#c9f24e"
	}
	if p.DefaultBranch == "" {
		p.DefaultBranch = "main"
	}
	if p.DefaultAgentID == "" {
		p.DefaultAgentID = "agent_codex_default"
	}
	p.CreatedAt = time.Now().UTC()
	p.UpdatedAt = p.CreatedAt
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,name,description,path,normalized_path,color,default_branch,default_agent_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, p.ID, p.Name, p.Description, filepath.Clean(p.Path), normalizePath(p.Path), p.Color, p.DefaultBranch, p.DefaultAgentID, p.CreatedAt.Format(time.RFC3339Nano), p.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return p, ErrConflict
		}
		return p, err
	}
	_ = s.index(ctx, "project", p.ID, p.ID, p.Name, p.Description)
	return p, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,path,color,default_branch,default_agent_id,archived,created_at,updated_at FROM projects WHERE archived=0 ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var created, updated string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Path, &p.Color, &p.DefaultBranch, &p.DefaultAgentID, &p.Archived, &created, &updated); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(created)
		p.UpdatedAt = parseTime(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProject(ctx context.Context, id string) (domain.Project, error) {
	var p domain.Project
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,description,path,color,default_branch,default_agent_id,archived,created_at,updated_at FROM projects WHERE id=?`, id).Scan(&p.ID, &p.Name, &p.Description, &p.Path, &p.Color, &p.DefaultBranch, &p.DefaultAgentID, &p.Archived, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.CreatedAt = parseTime(created)
	p.UpdatedAt = parseTime(updated)
	return p, nil
}

func (s *Store) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	if strings.TrimSpace(t.Title) == "" || t.ProjectID == "" {
		return t, fmt.Errorf("projectId and title are required")
	}
	if t.ID == "" {
		t.ID = ids.New("tsk")
	}
	if t.Status == "" {
		t.Status = domain.TaskInbox
	}
	if !t.Status.Valid() {
		return t, fmt.Errorf("invalid task status")
	}
	if t.RunStatus == "" {
		t.RunStatus = domain.RunNotStarted
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	t.Version = 1
	if t.Key == "" {
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*)+1 FROM tasks WHERE project_id=?`, t.ProjectID).Scan(&n)
		t.Key = fmt.Sprintf("PB-%d", n)
	}
	labels, _ := json.Marshal(t.Labels)
	acceptance, _ := json.Marshal(t.Acceptance)
	_, err := s.db.ExecContext(ctx, `INSERT INTO tasks(id,project_id,task_key,title,description,status,run_status,priority,labels_json,acceptance_json,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, t.ID, t.ProjectID, t.Key, t.Title, t.Description, t.Status, t.RunStatus, t.Priority, string(labels), string(acceptance), t.Version, t.CreatedAt.Format(time.RFC3339Nano), t.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return t, err
	}
	_ = s.index(ctx, "task", t.ID, t.ProjectID, t.Title, t.Description)
	_ = s.audit(ctx, t.ProjectID, t.ID, "", "task.created", "user", map[string]any{"title": t.Title})
	return t, nil
}

func (s *Store) ListTasks(ctx context.Context, projectID string) ([]domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,task_key,title,description,status,run_status,priority,labels_json,acceptance_json,due_at,version,deleted_at,created_at,updated_at FROM tasks WHERE project_id=? AND deleted_at IS NULL ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTask(ctx context.Context, id string) (domain.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,project_id,task_key,title,description,status,run_status,priority,labels_json,acceptance_json,due_at,version,deleted_at,created_at,updated_at FROM tasks WHERE id=?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

type scanner interface{ Scan(...any) error }

func scanTask(row scanner) (domain.Task, error) {
	var t domain.Task
	var labels, acceptance string
	var due, deleted sql.NullString
	var created, updated string
	err := row.Scan(&t.ID, &t.ProjectID, &t.Key, &t.Title, &t.Description, &t.Status, &t.RunStatus, &t.Priority, &labels, &acceptance, &due, &t.Version, &deleted, &created, &updated)
	if err != nil {
		return t, err
	}
	_ = json.Unmarshal([]byte(labels), &t.Labels)
	_ = json.Unmarshal([]byte(acceptance), &t.Acceptance)
	if due.Valid {
		v := parseTime(due.String)
		t.DueAt = &v
	}
	if deleted.Valid {
		v := parseTime(deleted.String)
		t.DeletedAt = &v
	}
	t.CreatedAt = parseTime(created)
	t.UpdatedAt = parseTime(updated)
	return t, nil
}

func (s *Store) UpdateTaskStatus(ctx context.Context, id string, status domain.TaskStatus, version int64) (domain.Task, error) {
	if !status.Valid() {
		return domain.Task{}, fmt.Errorf("invalid task status")
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, status, now.Format(time.RFC3339Nano), id, version)
	if err != nil {
		return domain.Task{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.Task{}, ErrConflict
	}
	t, err := s.GetTask(ctx, id)
	if err == nil {
		_ = s.audit(ctx, t.ProjectID, t.ID, "", "task.status_changed", "user", map[string]any{"status": status})
	}
	return t, err
}

func (s *Store) ListAgents(ctx context.Context, projectID string) ([]domain.AgentProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,name,model,reasoning,system_prompt,sandbox,network,is_default,created_at FROM agents WHERE project_id='' OR project_id=? ORDER BY is_default DESC,name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AgentProfile
	for rows.Next() {
		var a domain.AgentProfile
		var created string
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.Name, &a.Model, &a.Reasoning, &a.SystemPrompt, &a.Sandbox, &a.Network, &a.IsDefault, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = parseTime(created)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetOrCreateConversation(ctx context.Context, taskID, agentID string) (domain.Conversation, error) {
	var c domain.Conversation
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,task_id,agent_id,title,summary,codex_thread_id,created_at,updated_at FROM conversations WHERE task_id=? AND agent_id=?`, taskID, agentID).Scan(&c.ID, &c.TaskID, &c.AgentID, &c.Title, &c.Summary, &c.CodexThreadID, &created, &updated)
	if err == nil {
		c.CreatedAt = parseTime(created)
		c.UpdatedAt = parseTime(updated)
		return c, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return c, err
	}
	c = domain.Conversation{ID: ids.New("cnv"), TaskID: taskID, AgentID: agentID, Title: "任务对话", CreatedAt: time.Now().UTC()}
	c.UpdatedAt = c.CreatedAt
	_, err = s.db.ExecContext(ctx, `INSERT INTO conversations(id,task_id,agent_id,title,created_at,updated_at) VALUES(?,?,?,?,?,?)`, c.ID, c.TaskID, c.AgentID, c.Title, c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano))
	return c, err
}

func (s *Store) ListConversations(ctx context.Context, taskID string) ([]domain.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,agent_id,title,summary,codex_thread_id,created_at,updated_at FROM conversations WHERE task_id=? ORDER BY updated_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Conversation
	for rows.Next() {
		var c domain.Conversation
		var created, updated string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.AgentID, &c.Title, &c.Summary, &c.CodexThreadID, &created, &updated); err != nil {
			return nil, err
		}
		c.CreatedAt = parseTime(created)
		c.UpdatedAt = parseTime(updated)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddMessage(ctx context.Context, m domain.Message) (domain.Message, error) {
	if m.ID == "" {
		m.ID = ids.New("msg")
	}
	if m.Kind == "" {
		m.Kind = "text"
	}
	m.CreatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages(id,conversation_id,role,kind,content,created_at) VALUES(?,?,?,?,?,?)`, m.ID, m.ConversationID, m.Role, m.Kind, m.Content, m.CreatedAt.Format(time.RFC3339Nano))
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE conversations SET updated_at=? WHERE id=?`, m.CreatedAt.Format(time.RFC3339Nano), m.ConversationID)
	}
	return m, err
}

func (s *Store) ListMessages(ctx context.Context, conversationID string) ([]domain.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,conversation_id,role,kind,content,created_at FROM messages WHERE conversation_id=? ORDER BY created_at`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var created string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Kind, &m.Content, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTime(created)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateRun(ctx context.Context, r domain.Run) (domain.Run, error) {
	if r.ID == "" {
		r.ID = ids.New("run")
	}
	r.Status = domain.RunQueued
	r.CreatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO runs(id,task_id,conversation_id,agent_id,workspace_id,status,prompt,created_at) VALUES(?,?,?,?,?,?,?,?)`, r.ID, r.TaskID, r.ConversationID, r.AgentID, r.WorkspaceID, r.Status, r.Prompt, r.CreatedAt.Format(time.RFC3339Nano))
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE tasks SET run_status=?,updated_at=? WHERE id=?`, r.Status, r.CreatedAt.Format(time.RFC3339Nano), r.TaskID)
	}
	return r, err
}

func (s *Store) ListRuns(ctx context.Context, taskID string) ([]domain.Run, error) {
	query := `SELECT id,task_id,conversation_id,agent_id,workspace_id,status,prompt,started_at,finished_at,created_at FROM runs`
	args := []any{}
	if taskID != "" {
		query += " WHERE task_id=?"
		args = append(args, taskID)
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Run
	for rows.Next() {
		var r domain.Run
		var started, finished sql.NullString
		var created string
		if err := rows.Scan(&r.ID, &r.TaskID, &r.ConversationID, &r.AgentID, &r.WorkspaceID, &r.Status, &r.Prompt, &started, &finished, &created); err != nil {
			return nil, err
		}
		if started.Valid {
			v := parseTime(started.String)
			r.StartedAt = &v
		}
		if finished.Valid {
			v := parseTime(finished.String)
			r.FinishedAt = &v
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Dashboard(ctx context.Context, projectID string) (map[string]any, error) {
	tasks, err := s.ListTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, t := range tasks {
		counts[string(t.Status)]++
	}
	runs, err := s.ListRuns(ctx, "")
	if err != nil {
		return nil, err
	}
	active := 0
	for _, r := range runs {
		if r.Status == domain.RunRunning || r.Status == domain.RunQueued || r.Status == domain.RunWaitingApproval {
			active++
		}
	}
	return map[string]any{"taskCount": len(tasks), "statusCounts": counts, "activeRuns": active, "unreviewedFiles": 0, "recentTasks": tasks}, nil
}

func (s *Store) QueryRows(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				m[c] = string(b)
			} else {
				m[c] = vals[i]
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Search(ctx context.Context, query, projectID string) ([]map[string]any, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []map[string]any{}, nil
	}
	return s.QueryRows(ctx, `SELECT entity_type AS type,entity_id AS id,title,snippet(search_fts,4,'<mark>','</mark>','…',16) AS snippet FROM search_fts WHERE search_fts MATCH ? AND (?='' OR project_id=?) LIMIT 50`, query, projectID, projectID)
}

func (s *Store) index(ctx context.Context, kind, id, projectID, title, body string) error {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM search_fts WHERE entity_type=? AND entity_id=?`, kind, id)
	_, err := s.db.ExecContext(ctx, `INSERT INTO search_fts(entity_type,entity_id,project_id,title,body) VALUES(?,?,?,?,?)`, kind, id, projectID, title, body)
	return err
}
func (s *Store) audit(ctx context.Context, projectID, taskID, runID, action, actor string, detail any) error {
	raw, _ := json.Marshal(detail)
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(id,project_id,task_id,run_id,action,actor,detail_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, ids.New("aud"), projectID, taskID, runID, action, actor, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func parseTime(v string) time.Time { t, _ := time.Parse(time.RFC3339Nano, v); return t }

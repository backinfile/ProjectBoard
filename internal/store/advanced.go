package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/projectboard/projectboard/internal/domain"
	"github.com/projectboard/projectboard/internal/ids"
	"github.com/projectboard/projectboard/internal/workspace"
)

func (s *Store) GetAgent(ctx context.Context, id string) (domain.AgentProfile, error) {
	var a domain.AgentProfile
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,name,model,reasoning,system_prompt,sandbox,network,is_default,created_at FROM agents WHERE id=?`, id).Scan(&a.ID, &a.ProjectID, &a.Name, &a.Model, &a.Reasoning, &a.SystemPrompt, &a.Sandbox, &a.Network, &a.IsDefault, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	a.CreatedAt = parseTime(created)
	return a, err
}

func (s *Store) GetConversation(ctx context.Context, id string) (domain.Conversation, error) {
	var c domain.Conversation
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,task_id,agent_id,title,summary,codex_thread_id,created_at,updated_at FROM conversations WHERE id=?`, id).Scan(&c.ID, &c.TaskID, &c.AgentID, &c.Title, &c.Summary, &c.CodexThreadID, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, err
}

func (s *Store) UpdateConversationThread(ctx context.Context, id, threadID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE conversations SET codex_thread_id=?,updated_at=? WHERE id=?`, threadID, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) UpdateRun(ctx context.Context, id string, status domain.RunStatus) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if status == domain.RunRunning {
		_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?,started_at=COALESCE(started_at,?) WHERE id=?`, status, now, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?,finished_at=? WHERE id=?`, status, now, id)
	return err
}

func (s *Store) AddRunEvent(ctx context.Context, runID, eventType string, payload any) error {
	raw, _ := json.Marshal(payload)
	_, err := s.db.ExecContext(ctx, `INSERT INTO run_events(run_id,event_type,payload_json,created_at) VALUES(?,?,?,?)`, runID, eventType, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) CreateApproval(ctx context.Context, runID, method string, detail any) (domain.ApprovalRequest, error) {
	raw, _ := json.Marshal(detail)
	a := domain.ApprovalRequest{ID: ids.New("apr"), RunID: runID, Method: method, Detail: string(raw), Status: "pending", CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO approvals(id,run_id,method,detail_json,status,created_at) VALUES(?,?,?,?,?,?)`, a.ID, a.RunID, a.Method, a.Detail, a.Status, a.CreatedAt.Format(time.RFC3339Nano))
	return a, err
}

func (s *Store) DecideApproval(ctx context.Context, id, decision string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE approvals SET status='decided',decision=?,decided_at=? WHERE id=? AND status='pending'`, decision, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) SetTaskRunStatus(ctx context.Context, taskID string, status domain.RunStatus) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET run_status=?,updated_at=? WHERE id=?`, status, time.Now().UTC().Format(time.RFC3339Nano), taskID)
	return err
}

func (s *Store) SaveWorkspace(ctx context.Context, w domain.TaskWorkspace) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO task_workspaces(id,task_id,path,branch,base_commit,kind,parent_id,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET path=excluded.path,branch=excluded.branch,base_commit=excluded.base_commit,state=excluded.state,updated_at=excluded.updated_at`, w.ID, w.TaskID, w.Path, w.Branch, w.BaseCommit, w.Kind, w.ParentID, w.State, now, now)
	return err
}

func (s *Store) GetMainWorkspace(ctx context.Context, taskID string) (domain.TaskWorkspace, error) {
	var w domain.TaskWorkspace
	err := s.db.QueryRowContext(ctx, `SELECT id,task_id,path,branch,base_commit,kind,parent_id,state FROM task_workspaces WHERE task_id=? AND kind='main'`, taskID).Scan(&w.ID, &w.TaskID, &w.Path, &w.Branch, &w.BaseCommit, &w.Kind, &w.ParentID, &w.State)
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

func (s *Store) GetWorkspace(ctx context.Context, id string) (domain.TaskWorkspace, error) {
	var w domain.TaskWorkspace
	err := s.db.QueryRowContext(ctx, `SELECT id,task_id,path,branch,base_commit,kind,parent_id,state FROM task_workspaces WHERE id=?`, id).Scan(&w.ID, &w.TaskID, &w.Path, &w.Branch, &w.BaseCommit, &w.Kind, &w.ParentID, &w.State)
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

func (s *Store) CreateParallelPlan(ctx context.Context, p domain.ParallelPlan) (domain.ParallelPlan, error) {
	if len(p.Agents) < 2 {
		return p, fmt.Errorf("parallel plan requires at least two agents")
	}
	if p.ID == "" {
		p.ID = ids.New("plan")
	}
	p.Status = "proposed"
	p.AuthorizedChildren = len(p.Agents) - 1
	p.CreatedAt = time.Now().UTC()
	raw, _ := json.Marshal(p.Agents)
	_, err := s.db.ExecContext(ctx, `INSERT INTO parallel_plans(id,task_id,main_agent_id,prompt,agents_json,authorized_children,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, p.ID, p.TaskID, p.MainAgentID, p.Prompt, string(raw), p.AuthorizedChildren, p.Status, p.CreatedAt.Format(time.RFC3339Nano), p.CreatedAt.Format(time.RFC3339Nano))
	return p, err
}

func (s *Store) GetParallelPlan(ctx context.Context, id string) (domain.ParallelPlan, error) {
	var p domain.ParallelPlan
	var raw, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,task_id,main_agent_id,prompt,agents_json,authorized_children,snapshot_commit,status,created_at FROM parallel_plans WHERE id=?`, id).Scan(&p.ID, &p.TaskID, &p.MainAgentID, &p.Prompt, &raw, &p.AuthorizedChildren, &p.SnapshotCommit, &p.Status, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	_ = json.Unmarshal([]byte(raw), &p.Agents)
	p.CreatedAt = parseTime(created)
	return p, err
}

func (s *Store) ApproveParallelPlan(ctx context.Context, id, snapshot string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE parallel_plans SET status='approved',snapshot_commit=?,updated_at=? WHERE id=? AND status='proposed'`, snapshot, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) SaveCandidate(ctx context.Context, c workspace.Candidate, summary string) (string, error) {
	id := ids.New("cand")
	_, err := s.db.ExecContext(ctx, `INSERT INTO candidate_changes(id,task_id,workspace_id,commit_hash,patch,summary,created_at) VALUES(?,?,?,?,?,?,?)`, id, c.Workspace.TaskID, c.Workspace.ID, c.Commit, c.Patch, summary, time.Now().UTC().Format(time.RFC3339Nano))
	return id, err
}
func (s *Store) GetCandidate(ctx context.Context, id string) (workspace.Candidate, string, error) {
	var c workspace.Candidate
	var summary string
	err := s.db.QueryRowContext(ctx, `SELECT w.id,w.task_id,w.path,w.branch,w.base_commit,w.kind,w.parent_id,c.commit_hash,c.patch,c.summary FROM candidate_changes c JOIN task_workspaces w ON w.id=c.workspace_id WHERE c.id=? AND c.status='pending'`, id).Scan(&c.Workspace.ID, &c.Workspace.TaskID, &c.Workspace.Path, &c.Workspace.Branch, &c.Workspace.BaseCommit, &c.Workspace.Kind, &c.Workspace.ParentID, &c.Commit, &c.Patch, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return c, "", ErrNotFound
	}
	return c, summary, err
}
func (s *Store) DecideCandidate(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE candidate_changes SET status=?,decided_at=? WHERE id=? AND status='pending'`, status, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) ListKnowledge(ctx context.Context, projectID string) ([]domain.Knowledge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,title,content,summary,category,permission,locked,sensitive,version,created_at,updated_at FROM knowledge WHERE project_id=? AND deleted_at IS NULL ORDER BY updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Knowledge
	for rows.Next() {
		var k domain.Knowledge
		var created, updated string
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.Title, &k.Content, &k.Summary, &k.Category, &k.Permission, &k.Locked, &k.Sensitive, &k.Version, &created, &updated); err != nil {
			return nil, err
		}
		k.CreatedAt, k.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) ListReadableKnowledge(ctx context.Context, projectID, query string) ([]domain.Knowledge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,title,content,summary,category,permission,locked,sensitive,version,created_at,updated_at FROM knowledge WHERE project_id=? AND deleted_at IS NULL AND permission NOT IN ('deny','hidden') AND (?='' OR title LIKE '%'||?||'%' OR summary LIKE '%'||?||'%' OR content LIKE '%'||?||'%') ORDER BY updated_at DESC LIMIT 50`, projectID, query, query, query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Knowledge
	for rows.Next() {
		var k domain.Knowledge
		var created, updated string
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.Title, &k.Content, &k.Summary, &k.Category, &k.Permission, &k.Locked, &k.Sensitive, &k.Version, &created, &updated); err != nil {
			return nil, err
		}
		k.CreatedAt, k.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) GetReadableKnowledge(ctx context.Context, id, projectID string) (domain.Knowledge, error) {
	var k domain.Knowledge
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,title,content,summary,category,permission,locked,sensitive,version,created_at,updated_at FROM knowledge WHERE id=? AND project_id=? AND deleted_at IS NULL AND permission NOT IN ('deny','hidden')`, id, projectID).Scan(&k.ID, &k.ProjectID, &k.Title, &k.Content, &k.Summary, &k.Category, &k.Permission, &k.Locked, &k.Sensitive, &k.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return k, ErrNotFound
	}
	k.CreatedAt, k.UpdatedAt = parseTime(created), parseTime(updated)
	return k, err
}

func (s *Store) AuditMCP(ctx context.Context, projectID, taskID, runID, tool string, args any) error {
	return s.audit(ctx, projectID, taskID, runID, "mcp."+tool, "agent", args)
}

func (s *Store) CreateKnowledge(ctx context.Context, k domain.Knowledge) (domain.Knowledge, error) {
	if k.Title == "" || k.ProjectID == "" {
		return k, fmt.Errorf("projectId and title are required")
	}
	if k.ID == "" {
		k.ID = ids.New("knw")
	}
	if k.Category == "" {
		k.Category = "general"
	}
	if k.Permission == "" {
		k.Permission = "suggest"
	}
	k.Version = 1
	k.CreatedAt = time.Now().UTC()
	k.UpdatedAt = k.CreatedAt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return k, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO knowledge(id,project_id,title,content,summary,category,permission,locked,sensitive,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, k.ID, k.ProjectID, k.Title, k.Content, k.Summary, k.Category, k.Permission, k.Locked, k.Sensitive, k.Version, k.CreatedAt.Format(time.RFC3339Nano), k.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return k, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO knowledge_versions(id,knowledge_id,version,content,summary,created_at) VALUES(?,?,?,?,?,?)`, ids.New("kv"), k.ID, 1, k.Content, k.Summary, k.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return k, err
	}
	if err = tx.Commit(); err != nil {
		return k, err
	}
	_ = s.index(ctx, "knowledge", k.ID, k.ProjectID, k.Title, k.Summary+"\n"+k.Content)
	return k, nil
}

func (s *Store) IssueCapability(ctx context.Context, token, projectID, taskID, runID string, permissions []string, expires time.Time) error {
	h := sha256.Sum256([]byte(token))
	raw, _ := json.Marshal(permissions)
	_, err := s.db.ExecContext(ctx, `INSERT INTO capability_tokens(token_hash,project_id,task_id,run_id,permissions_json,expires_at) VALUES(?,?,?,?,?,?)`, hex.EncodeToString(h[:]), projectID, taskID, runID, string(raw), expires.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ResolveCapability(ctx context.Context, token string) (projectID, taskID, runID string, permissions []string, err error) {
	h := sha256.Sum256([]byte(token))
	var raw, expires string
	var revoked sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT project_id,task_id,run_id,permissions_json,expires_at,revoked_at FROM capability_tokens WHERE token_hash=?`, hex.EncodeToString(h[:])).Scan(&projectID, &taskID, &runID, &raw, &expires, &revoked)
	if err != nil {
		return
	}
	if revoked.Valid || parseTime(expires).Before(time.Now().UTC()) {
		err = ErrNotFound
		return
	}
	_ = json.Unmarshal([]byte(raw), &permissions)
	return
}

func (s *Store) RevokeCapabilities(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE capability_tokens SET revoked_at=? WHERE run_id=? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339Nano), runID)
	return err
}

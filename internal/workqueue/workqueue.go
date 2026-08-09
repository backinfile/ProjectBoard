package workqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
)

type Actor struct{ Type, ID string }
type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string { return e.Message }

type WorkItem struct {
	ID                   string           `json:"id"`
	ProjectID            string           `json:"project_id"`
	Number               int64            `json:"number"`
	ParentID             *string          `json:"parent_id"`
	Title                string           `json:"title"`
	Description          string           `json:"description_markdown"`
	AcceptanceCriteria   string           `json:"acceptance_criteria_markdown"`
	Priority             string           `json:"priority"`
	Stage                string           `json:"stage"`
	TargetBranch         string           `json:"target_branch"`
	AssigneeKind         *string          `json:"assignee_kind"`
	AssigneeID           *string          `json:"assignee_id"`
	IsAgentTask          bool             `json:"is_agent_task"`
	PauseAfterPlan       bool             `json:"pause_after_plan"`
	PauseAfterCompletion bool             `json:"pause_after_completion"`
	PlanPauseConsumed    bool             `json:"plan_pause_consumed"`
	AgentState           string           `json:"agent_state"`
	AssignedAgentID      *string          `json:"assigned_agent_id"`
	CodexThreadID        *string          `json:"codex_thread_id"`
	WorkspacePath        *string          `json:"workspace_path"`
	WorkspaceSizeBytes   int64            `json:"workspace_size_bytes"`
	ResumeRequested      bool             `json:"resume_requested"`
	BlockedAt            *string          `json:"blocked_at"`
	BlockedReason        *string          `json:"blocked_reason"`
	Version              int64            `json:"version"`
	CreatedAt            string           `json:"created_at"`
	UpdatedAt            string           `json:"updated_at"`
	CompletedAt          *string          `json:"completed_at"`
	ClosedAt             *string          `json:"closed_at"`
	CreatedByUserID      *string          `json:"created_by_user_id"`
	FollowerIDs          []string         `json:"follower_ids"`
	Tags                 []string         `json:"tags"`
	Conversation         []map[string]any `json:"conversation"`
	Executions           []map[string]any `json:"executions"`
	Attachments          []map[string]any `json:"attachments"`
	Dependencies         []string         `json:"dependencies"`
}

type Module struct {
	store *store.Store
	now   func() time.Time
}

func New(s *store.Store) *Module   { return &Module{store: s, now: time.Now} }
func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func id() string                   { return security.Token(18) }
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func nullable(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

type CreateInput struct {
	RequestID, ProjectID, Title, DescriptionMarkdown, AcceptanceCriteriaMarkdown string
	Priority, TargetBranch, AssigneeKind, AssigneeID, ParentID, CreatedByUserID  string
	FollowerIDs                                                                  []string
	Tags                                                                         []string
	IsAgentTask, PauseAfterPlan, PauseAfterCompletion                            bool
}

func (m *Module) Create(ctx context.Context, actor Actor, in CreateInput) (*WorkItem, error) {
	if strings.TrimSpace(in.Title) == "" || in.ProjectID == "" {
		return nil, &Error{422, "VALIDATION_ERROR", "Project and title are required"}
	}
	if in.Priority == "" {
		in.Priority = "medium"
	}
	if !in.IsAgentTask {
		in.PauseAfterPlan = false
		in.PauseAfterCompletion = false
	} else {
		in.AssigneeKind = ""
		in.AssigneeID = ""
	}
	var itemID string
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		var key, branch, allowedJSON string
		if err := tx.QueryRowContext(ctx, "SELECT project_key,default_target_branch,allowed_target_branches_json FROM projects WHERE id=?", in.ProjectID).Scan(&key, &branch, &allowedJSON); err != nil {
			return err
		}
		if in.TargetBranch != "" {
			branch = in.TargetBranch
		}
		if !branchAllowed(branch, allowedJSON) {
			return &Error{422, "TARGET_BRANCH_NOT_ALLOWED", "Target branch is not allowed by the project"}
		}
		var number int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(number),0)+1 FROM work_items WHERE project_id=?", in.ProjectID).Scan(&number); err != nil {
			return err
		}
		itemID = fmt.Sprintf("%s-%d", strings.ToUpper(key), number)
		stamp := timestamp(m.now())
		agentState := "idle"
		if in.IsAgentTask {
			agentState = "queued"
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO work_items(id,project_id,number,parent_id,title,description_markdown,acceptance_criteria_markdown,priority,stage,target_branch,assignee_kind,assignee_id,is_agent_task,pause_after_plan,pause_after_completion,agent_state,created_by_user_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, itemID, in.ProjectID, number, nullable(in.ParentID), in.Title, in.DescriptionMarkdown, in.AcceptanceCriteriaMarkdown, in.Priority, "created", branch, nullable(in.AssigneeKind), nullable(in.AssigneeID), boolInt(in.IsAgentTask), boolInt(in.PauseAfterPlan), boolInt(in.PauseAfterCompletion), agentState, nullable(in.CreatedByUserID), stamp, stamp)
		if err != nil {
			return err
		}
		for _, fid := range in.FollowerIDs {
			if fid != "" {
				if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO work_item_followers(work_item_id,user_id,created_at) VALUES(?,?,?)", itemID, fid, stamp); err != nil {
					return err
				}
			}
		}
		tags, tagErr := normalizeTags(in.Tags)
		if tagErr != nil {
			return tagErr
		}
		for _, tag := range tags {
			if _, err = tx.ExecContext(ctx, "INSERT INTO work_item_tags(work_item_id,tag,created_at) VALUES(?,?,?)", itemID, tag, stamp); err != nil {
				return err
			}
		}
		return timeline(ctx, tx, itemID, "stage_transition", "created", actor, map[string]any{"from": nil, "to": "created"}, 1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func scanItem(row interface{ Scan(...any) error }) (*WorkItem, error) {
	var w WorkItem
	var agentTask, pap, pac, ppc, resume int
	err := row.Scan(&w.ID, &w.ProjectID, &w.Number, &w.ParentID, &w.Title, &w.Description, &w.AcceptanceCriteria, &w.Priority, &w.Stage, &w.TargetBranch, &w.AssigneeKind, &w.AssigneeID, &agentTask, &pap, &pac, &ppc, &w.AgentState, &w.AssignedAgentID, &w.CodexThreadID, &w.WorkspacePath, &resume, &w.BlockedAt, &w.BlockedReason, &w.Version, &w.CreatedAt, &w.UpdatedAt, &w.CompletedAt, &w.ClosedAt, &w.CreatedByUserID)
	w.IsAgentTask = agentTask != 0
	w.PauseAfterPlan = pap != 0
	w.PauseAfterCompletion = pac != 0
	w.PlanPauseConsumed = ppc != 0
	w.ResumeRequested = resume != 0
	return &w, err
}

const itemSelect = `SELECT id,project_id,number,parent_id,title,description_markdown,acceptance_criteria_markdown,priority,stage,target_branch,assignee_kind,assignee_id,is_agent_task,pause_after_plan,pause_after_completion,plan_pause_consumed,agent_state,assigned_agent_id,codex_thread_id,workspace_path,resume_requested,blocked_at,blocked_reason,version,created_at,updated_at,completed_at,closed_at,created_by_user_id FROM work_items`

func (m *Module) Get(ctx context.Context, itemID string) (*WorkItem, error) {
	w, err := scanItem(m.store.DB.QueryRowContext(ctx, itemSelect+" WHERE id=?", itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &Error{404, "NOT_FOUND", "Work item not found"}
	}
	if err != nil {
		return nil, err
	}
	w.Conversation = readMaps(ctx, m.store.DB, "SELECT id,kind,stage,author_type,author_id,payload_json,created_at FROM conversation_entries WHERE work_item_id=? ORDER BY created_at,id", itemID)
	for _, entry := range w.Conversation {
		if payload, ok := entry["payload_json"]; ok {
			entry["payload"] = payload
		}
	}
	w.Executions = readMaps(ctx, m.store.DB, "SELECT id,attempt_number,state,thread_id,command_json,result_json,final_message,workspace_path,started_at,ended_at,error_message FROM agent_executions WHERE work_item_id=? ORDER BY attempt_number", itemID)
	w.Attachments = readMaps(ctx, m.store.DB, "SELECT id,entry_id,original_name,mime,size,created_at FROM attachments WHERE work_item_id=? ORDER BY created_at,id", itemID)
	w.FollowerIDs = []string{}
	rows, _ := m.store.DB.QueryContext(ctx, "SELECT user_id FROM work_item_followers WHERE work_item_id=? ORDER BY created_at,user_id", itemID)
	if rows != nil {
		for rows.Next() {
			var v string
			if rows.Scan(&v) == nil {
				w.FollowerIDs = append(w.FollowerIDs, v)
			}
		}
		rows.Close()
	}
	w.Tags = []string{}
	tagRows, _ := m.store.DB.QueryContext(ctx, "SELECT tag FROM work_item_tags WHERE work_item_id=? ORDER BY created_at,tag", itemID)
	if tagRows != nil {
		for tagRows.Next() {
			var tag string
			if tagRows.Scan(&tag) == nil {
				w.Tags = append(w.Tags, tag)
			}
		}
		tagRows.Close()
	}
	w.Dependencies = []string{}
	deps, _ := m.store.DB.QueryContext(ctx, "SELECT depends_on_id FROM work_item_dependencies WHERE work_item_id=?", itemID)
	if deps != nil {
		for deps.Next() {
			var v string
			if deps.Scan(&v) == nil {
				w.Dependencies = append(w.Dependencies, v)
			}
		}
		deps.Close()
	}
	return w, nil
}

type UpdateInput struct {
	ExpectedVersion                                                                int64
	Title, Description, Criteria, Priority, TargetBranch, AssigneeKind, AssigneeID string
	FollowerIDs                                                                    []string
	Tags                                                                           []string
	Configure                                                                      bool
	IsAgentTask, PauseAfterPlan, PauseAfterCompletion                              bool
}

func (m *Module) Update(ctx context.Context, actor Actor, itemID string, in UpdateInput) (*WorkItem, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != in.ExpectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.Stage == "closed" {
			return &Error{409, "TERMINAL", "Closed work items are read-only"}
		}
		stamp := timestamp(m.now())
		title, desc, criteria, priority, branch := current.Title, current.Description, current.AcceptanceCriteria, current.Priority, current.TargetBranch
		kind, idv := current.AssigneeKind, current.AssigneeID
		isAgent, pap, pac := current.IsAgentTask, current.PauseAfterPlan, current.PauseAfterCompletion
		if in.Title != "" {
			title = in.Title
		}
		if in.Description != "" {
			desc = in.Description
		}
		if in.Criteria != "" {
			criteria = in.Criteria
		}
		if in.Priority != "" {
			priority = in.Priority
		}
		if in.TargetBranch != "" {
			var allowedJSON string
			if err = tx.QueryRowContext(ctx, "SELECT allowed_target_branches_json FROM projects WHERE id=?", current.ProjectID).Scan(&allowedJSON); err != nil {
				return err
			}
			if !branchAllowed(in.TargetBranch, allowedJSON) {
				return &Error{422, "TARGET_BRANCH_NOT_ALLOWED", "Target branch is not allowed by the project"}
			}
			branch = in.TargetBranch
		}
		if in.Configure {
			var executionCount int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_executions WHERE work_item_id=?", itemID).Scan(&executionCount); err != nil {
				return err
			}
			if current.AssignedAgentID != nil || executionCount > 0 {
				if in.IsAgentTask != current.IsAgentTask || in.PauseAfterPlan != current.PauseAfterPlan || in.PauseAfterCompletion != current.PauseAfterCompletion {
					return &Error{409, "AGENT_CONFIG_LOCKED", "Agent settings are locked after execution starts"}
				}
			} else {
				isAgent, pap, pac = in.IsAgentTask, in.PauseAfterPlan, in.PauseAfterCompletion
				if !isAgent {
					pap = false
					pac = false
				}
				kind = nil
				idv = nil
				if in.AssigneeID != "" {
					kind = &in.AssigneeKind
					idv = &in.AssigneeID
				}
			}
		}
		state := current.AgentState
		if isAgent && state == "idle" {
			state = "queued"
		}
		if !isAgent {
			state = "idle"
		}
		_, err = tx.ExecContext(ctx, "UPDATE work_items SET title=?,description_markdown=?,acceptance_criteria_markdown=?,priority=?,target_branch=?,assignee_kind=?,assignee_id=?,is_agent_task=?,pause_after_plan=?,pause_after_completion=?,agent_state=?,version=version+1,updated_at=? WHERE id=?", title, desc, criteria, priority, branch, kind, idv, boolInt(isAgent), boolInt(pap), boolInt(pac), state, stamp, itemID)
		if err != nil {
			return err
		}
		if in.Configure {
			if _, err = tx.ExecContext(ctx, "DELETE FROM work_item_followers WHERE work_item_id=?", itemID); err != nil {
				return err
			}
			for _, fid := range in.FollowerIDs {
				if fid != "" {
					if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO work_item_followers(work_item_id,user_id,created_at) VALUES(?,?,?)", itemID, fid, stamp); err != nil {
						return err
					}
				}
			}
			tags, tagErr := normalizeTags(in.Tags)
			if tagErr != nil {
				return tagErr
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM work_item_tags WHERE work_item_id=?", itemID); err != nil {
				return err
			}
			for _, tag := range tags {
				if _, err = tx.ExecContext(ctx, "INSERT INTO work_item_tags(work_item_id,tag,created_at) VALUES(?,?,?)", itemID, tag, stamp); err != nil {
					return err
				}
			}
		}
		return timeline(ctx, tx, itemID, "work_item_updated", current.Stage, actor, map[string]any{"isAgentTask": isAgent}, current.Version+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func normalizeTags(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, &Error{422, "TOO_MANY_TAGS", "A task can have at most 20 tags"}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > 30 {
			return nil, &Error{422, "TAG_TOO_LONG", "Tags can contain at most 30 characters"}
		}
		key := strings.ToLower(tag)
		if !seen[key] {
			seen[key] = true
			out = append(out, tag)
		}
	}
	return out, nil
}

func branchAllowed(branch, raw string) bool {
	var allowed []string
	if json.Unmarshal([]byte(raw), &allowed) != nil || len(allowed) == 0 {
		return false
	}
	for _, candidate := range allowed {
		if candidate == branch {
			return true
		}
	}
	return false
}

var allowedTransitions = map[string]map[string]bool{"created": {"in_progress": true}, "in_progress": {"completed": true}, "completed": {"in_progress": true, "closed": true}}

func (m *Module) MoveStage(ctx context.Context, actor Actor, itemID string, version int64, target, note string) (*WorkItem, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != version {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.AgentState == "running" && actor.Type != "agent" {
			return &Error{409, "AGENT_RUNNING", "Cannot change state while Agent is running"}
		}
		if !allowedTransitions[current.Stage][target] {
			return &Error{409, "INVALID_TRANSITION", "Invalid task state transition"}
		}
		stamp := timestamp(m.now())
		var completed, closed any
		if target == "completed" {
			completed = stamp
		}
		if target == "closed" {
			completed = current.CompletedAt
			closed = stamp
		}
		if target == "in_progress" {
			completed = nil
			closed = nil
		}
		agentState := current.AgentState
		if target == "closed" {
			agentState = "finished"
		}
		_, err = tx.ExecContext(ctx, "UPDATE work_items SET stage=?,agent_state=?,completed_at=?,closed_at=?,version=version+1,updated_at=? WHERE id=?", target, agentState, completed, closed, stamp, itemID)
		if err != nil {
			return err
		}
		return timeline(ctx, tx, itemID, "stage_transition", target, actor, map[string]any{"from": current.Stage, "to": target, "noteMarkdown": note}, version+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) AddMessage(ctx context.Context, actor Actor, itemID, markdown string, expectedVersion int64) (*WorkItem, error) {
	return m.AddMessageWithResume(ctx, actor, itemID, markdown, expectedVersion, false)
}
func (m *Module) AddMessageWithResume(ctx context.Context, actor Actor, itemID, markdown string, expectedVersion int64, resume bool) (*WorkItem, error) {
	if strings.TrimSpace(markdown) == "" {
		return nil, &Error{422, "VALIDATION_ERROR", "Message is required"}
	}
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Stage == "closed" {
			return &Error{409, "TERMINAL", "Closed work items are read-only"}
		}
		if expectedVersion > 0 && current.Version != expectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if resume && (!current.IsAgentTask || !strings.HasPrefix(current.AgentState, "paused_")) {
			return &Error{409, "AGENT_NOT_PAUSED", "Agent is not paused"}
		}
		raw, _ := json.Marshal(map[string]any{"markdown": markdown, "resumeAgent": resume})
		stamp := timestamp(m.now())
		if _, err = tx.ExecContext(ctx, "INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) VALUES(?,?,?,?,?,?,?,?,?)", id(), itemID, "message", current.Stage, actor.Type, nullable(actor.ID), string(raw), current.Version, stamp); err != nil {
			return err
		}
		if current.IsAgentTask && current.AgentState == "running" && !resume {
			_, err = tx.ExecContext(ctx, "UPDATE work_items SET resume_requested=1 WHERE id=?", itemID)
		}
		if resume {
			_, err = tx.ExecContext(ctx, "UPDATE work_items SET resume_requested=1,agent_state='queued',stage=CASE WHEN stage='completed' THEN 'in_progress' ELSE stage END,completed_at=CASE WHEN stage='completed' THEN NULL ELSE completed_at END,version=version+1,updated_at=? WHERE id=?", stamp, itemID)
			if err == nil {
				err = timeline(ctx, tx, itemID, "agent_resumed", "in_progress", Actor{Type: "system"}, map[string]any{"message": "A member requested the Agent to continue"}, current.Version+1)
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) SetBlocked(ctx context.Context, actor Actor, itemID string, version int64, reason string, blocked bool) (*WorkItem, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != version {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.Stage == "closed" {
			return &Error{409, "TERMINAL", "Closed work items are read-only"}
		}
		stamp := timestamp(m.now())
		var at, why any
		kind := "unblocked"
		if blocked {
			if strings.TrimSpace(reason) == "" {
				return &Error{422, "REASON_REQUIRED", "Block reason required"}
			}
			at = stamp
			why = reason
			kind = "blocked"
		}
		_, err = tx.ExecContext(ctx, "UPDATE work_items SET blocked_at=?,blocked_reason=?,version=version+1,updated_at=? WHERE id=?", at, why, stamp, itemID)
		if err != nil {
			return err
		}
		return timeline(ctx, tx, itemID, kind, current.Stage, actor, map[string]any{"reason": reason}, version+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) Assign(ctx context.Context, actor Actor, itemID string, version int64, kind, assigneeID, reason string) (*WorkItem, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != version {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.IsAgentTask {
			return &Error{409, "AGENT_TASK_AUTO_ASSIGNED", "Agent tasks are assigned automatically"}
		}
		if current.Stage == "closed" {
			return &Error{409, "TERMINAL", "Closed work items are read-only"}
		}
		stamp := timestamp(m.now())
		_, err = tx.ExecContext(ctx, "UPDATE work_items SET assignee_kind=?,assignee_id=?,version=version+1,updated_at=? WHERE id=?", kind, assigneeID, stamp, itemID)
		if err != nil {
			return err
		}
		return timeline(ctx, tx, itemID, "assignment_changed", current.Stage, actor, map[string]any{"kind": kind, "id": assigneeID, "reason": reason}, version+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func getTx(ctx context.Context, tx *sql.Tx, itemID string) (*WorkItem, error) {
	w, err := scanItem(tx.QueryRowContext(ctx, itemSelect+" WHERE id=?", itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &Error{404, "NOT_FOUND", "Work item not found"}
	}
	return w, err
}
func timeline(ctx context.Context, tx *sql.Tx, itemID, kind, stage string, actor Actor, payload any, version int64) error {
	raw, _ := json.Marshal(payload)
	_, err := tx.ExecContext(ctx, "INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) VALUES(?,?,?,?,?,?,?,?,?)", id(), itemID, kind, stage, actor.Type, nullable(actor.ID), string(raw), version, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func readMaps(ctx context.Context, db *sql.DB, query string, args ...any) []map[string]any {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Scan(ptrs...) != nil {
			continue
		}
		row := map[string]any{}
		for i, c := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if strings.HasSuffix(c, "_json") {
				var decoded any
				if s, ok := v.(string); ok && json.Unmarshal([]byte(s), &decoded) == nil {
					v = decoded
				}
			}
			row[c] = v
		}
		out = append(out, row)
	}
	return out
}

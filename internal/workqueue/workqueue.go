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
	ID                 string           `json:"id"`
	ProjectID          string           `json:"project_id"`
	Number             int64            `json:"number"`
	ParentID           *string          `json:"parent_id"`
	Title              string           `json:"title"`
	Description        string           `json:"description_markdown"`
	AcceptanceCriteria string           `json:"acceptance_criteria_markdown"`
	Priority           string           `json:"priority"`
	Stage              string           `json:"stage"`
	DiscussionMode     string           `json:"discussion_mode"`
	ExecutionMode      string           `json:"execution_mode"`
	AcceptanceMode     string           `json:"acceptance_mode"`
	TargetBranch       string           `json:"target_branch"`
	AssigneeKind       *string          `json:"assignee_kind"`
	AssigneeID         *string          `json:"assignee_id"`
	AssignmentState    *string          `json:"assignment_state"`
	BlockedAt          *string          `json:"blocked_at"`
	BlockedReason      *string          `json:"blocked_reason"`
	Version            int64            `json:"version"`
	CreatedAt          string           `json:"created_at"`
	UpdatedAt          string           `json:"updated_at"`
	CompletedAt        *string          `json:"completed_at"`
	CreatedByUserID    *string          `json:"created_by_user_id"`
	FollowerIDs        []string         `json:"follower_ids"`
	Conversation       []map[string]any `json:"conversation"`
	Conclusions        []map[string]any `json:"conclusions"`
	Executions         []map[string]any `json:"executions"`
	Acceptances        []map[string]any `json:"acceptances"`
	Attachments        []map[string]any `json:"attachments"`
	Dependencies       []string         `json:"dependencies"`
}

type Module struct {
	store *store.Store
	now   func() time.Time
}

func New(s *store.Store) *Module   { return &Module{store: s, now: time.Now} }
func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func id() string                   { return security.Token(18) }

type CreateInput struct {
	RequestID, ProjectID, Title, DescriptionMarkdown, AcceptanceCriteriaMarkdown string
	Priority, TargetBranch, AssigneeKind, AssigneeID, ParentID, CreatedByUserID  string
	Stage, DiscussionMode, ExecutionMode, AcceptanceMode                         string
	FollowerIDs                                                                  []string
}

func (m *Module) Create(ctx context.Context, actor Actor, input CreateInput) (*WorkItem, error) {
	if strings.TrimSpace(input.Title) == "" || input.ProjectID == "" {
		return nil, &Error{422, "VALIDATION_ERROR", "Project and title are required"}
	}
	if input.Priority == "" {
		input.Priority = "medium"
	}
	var itemID string
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		var key, branch, discussion, execution, acceptance string
		if err := tx.QueryRowContext(ctx, "SELECT project_key,default_target_branch,discussion_mode,execution_mode,acceptance_mode FROM projects WHERE id=?", input.ProjectID).Scan(&key, &branch, &discussion, &execution, &acceptance); err != nil {
			return err
		}
		if input.TargetBranch != "" {
			branch = input.TargetBranch
		}
		if input.DiscussionMode != "" {
			discussion = input.DiscussionMode
		}
		if input.ExecutionMode != "" {
			execution = input.ExecutionMode
		}
		if input.AcceptanceMode != "" {
			acceptance = input.AcceptanceMode
		}
		var number int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(number),0)+1 FROM work_items WHERE project_id=?", input.ProjectID).Scan(&number); err != nil {
			return err
		}
		itemID = fmt.Sprintf("%s-%d", strings.ToUpper(key), number)
		now := timestamp(m.now())
		stage := input.Stage
		if stage == "" {
			stage = "todo"
		}
		var kind, idValue, state any
		if input.AssigneeID != "" {
			if input.Stage == "" {
				stage = "discussion"
			}
			kind = input.AssigneeKind
			idValue = input.AssigneeID
			state = "reserved"
		}
		var parent any
		if input.ParentID != "" {
			parent = input.ParentID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO work_items(id,project_id,number,parent_id,title,description_markdown,acceptance_criteria_markdown,priority,stage,discussion_mode,execution_mode,acceptance_mode,target_branch,assignee_kind,assignee_id,assignment_state,created_by_user_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, itemID, input.ProjectID, number, parent, input.Title, input.DescriptionMarkdown, input.AcceptanceCriteriaMarkdown, input.Priority, stage, discussion, execution, acceptance, branch, kind, idValue, state, nullable(input.CreatedByUserID), now, now)
		if err != nil {
			return err
		}
		if input.AssigneeID != "" {
			_, err = tx.ExecContext(ctx, "INSERT INTO assignments(id,work_item_id,assignee_kind,assignee_id,state,generation,created_at) VALUES(?,?,?,?,?,?,?)", id(), itemID, input.AssigneeKind, input.AssigneeID, "reserved", 1, now)
			if err != nil {
				return err
			}
		}
		for _, followerID := range input.FollowerIDs {
			if followerID == "" {
				continue
			}
			if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO work_item_followers(work_item_id,user_id,created_at) VALUES(?,?,?)", itemID, followerID, now); err != nil {
				return err
			}
		}
		return timeline(ctx, tx, itemID, "stage_transition", stage, actor, map[string]any{"from": nil, "to": stage}, 1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) Get(ctx context.Context, itemID string) (*WorkItem, error) {
	row := m.store.DB.QueryRowContext(ctx, `SELECT id,project_id,number,parent_id,title,description_markdown,acceptance_criteria_markdown,priority,stage,discussion_mode,execution_mode,acceptance_mode,target_branch,assignee_kind,assignee_id,assignment_state,blocked_at,blocked_reason,version,created_at,updated_at,completed_at,created_by_user_id FROM work_items WHERE id=?`, itemID)
	var w WorkItem
	if err := row.Scan(&w.ID, &w.ProjectID, &w.Number, &w.ParentID, &w.Title, &w.Description, &w.AcceptanceCriteria, &w.Priority, &w.Stage, &w.DiscussionMode, &w.ExecutionMode, &w.AcceptanceMode, &w.TargetBranch, &w.AssigneeKind, &w.AssigneeID, &w.AssignmentState, &w.BlockedAt, &w.BlockedReason, &w.Version, &w.CreatedAt, &w.UpdatedAt, &w.CompletedAt, &w.CreatedByUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &Error{404, "NOT_FOUND", "Work item not found"}
		}
		return nil, err
	}
	w.Conversation = readMaps(ctx, m.store.DB, "SELECT id,kind,stage,author_type,author_id,payload_json,created_at FROM conversation_entries WHERE work_item_id=? ORDER BY created_at,id", itemID)
	w.Conclusions = readMaps(ctx, m.store.DB, "SELECT id,version,goal_markdown,scope_markdown,out_of_scope_markdown,implementation_plan_markdown,acceptance_criteria_markdown,risks_markdown,created_at FROM discussion_conclusions WHERE work_item_id=? ORDER BY version", itemID)
	w.Executions = readMaps(ctx, m.store.DB, "SELECT id,number,summary_markdown,commits_json,changed_files_json,remaining_risks_markdown,started_at,ended_at FROM execution_attempts WHERE work_item_id=? ORDER BY number", itemID)
	w.Acceptances = readMaps(ctx, m.store.DB, "SELECT id,number,outcome,note_markdown,criteria_results_json,started_at,completed_at FROM acceptance_attempts WHERE work_item_id=? ORDER BY number", itemID)
	w.Attachments = readMaps(ctx, m.store.DB, "SELECT id,entry_id,original_name,mime,size,created_at FROM attachments WHERE work_item_id=? ORDER BY created_at,id", itemID)
	w.Dependencies = []string{}
	w.FollowerIDs = []string{}
	followers, _ := m.store.DB.QueryContext(ctx, "SELECT user_id FROM work_item_followers WHERE work_item_id=? ORDER BY created_at,user_id", itemID)
	if followers != nil {
		for followers.Next() {
			var followerID string
			if followers.Scan(&followerID) == nil {
				w.FollowerIDs = append(w.FollowerIDs, followerID)
			}
		}
		_ = followers.Close()
	}
	rows, _ := m.store.DB.QueryContext(ctx, "SELECT depends_on_id FROM work_item_dependencies WHERE work_item_id=?", itemID)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var d string
			_ = rows.Scan(&d)
			w.Dependencies = append(w.Dependencies, d)
		}
	}
	return &w, nil
}

type ConclusionInput struct {
	ExpectedVersion                                int64
	Goal, Scope, OutOfScope, Plan, Criteria, Risks string
}

func (m *Module) Freeze(ctx context.Context, actor Actor, itemID string, input ConclusionInput) (*WorkItem, error) {
	err := m.transition(ctx, actor, itemID, input.ExpectedVersion, "discussion", "execution", func(tx *sql.Tx, current *WorkItem, now string) error {
		var version int64
		_ = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0)+1 FROM discussion_conclusions WHERE work_item_id=?", itemID).Scan(&version)
		_, err := tx.ExecContext(ctx, "INSERT INTO discussion_conclusions(id,work_item_id,version,goal_markdown,scope_markdown,out_of_scope_markdown,implementation_plan_markdown,acceptance_criteria_markdown,risks_markdown,created_by_type,created_by_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id(), itemID, version, input.Goal, input.Scope, input.OutOfScope, input.Plan, input.Criteria, input.Risks, actor.Type, actor.ID, now)
		if err != nil {
			return err
		}
		return timeline(ctx, tx, itemID, "conclusion_frozen", "execution", actor, map[string]any{"version": version}, current.Version+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

type Validation struct {
	Name       string `json:"name"`
	Required   bool   `json:"required"`
	ExitCode   int    `json:"exitCode"`
	DurationMS int64  `json:"durationMs"`
	LogSummary string `json:"logSummary"`
}
type ExecutionInput struct {
	ExpectedVersion                            int64
	Summary                                    string
	Pushed, WorktreeClean, ForbiddenPathsClean bool
	Commits, ChangedFiles                      []string
	Validations                                []Validation
	RemainingRisks                             string
}

func (m *Module) SubmitExecution(ctx context.Context, actor Actor, itemID string, input ExecutionInput) (*WorkItem, error) {
	for _, v := range input.Validations {
		if v.Required && v.ExitCode != 0 {
			return nil, &Error{422, "VALIDATION_FAILED", "Required validation failed"}
		}
	}
	if !input.WorktreeClean || !input.ForbiddenPathsClean {
		return nil, &Error{422, "EXECUTION_EVIDENCE_INVALID", "Execution safety checks failed"}
	}
	err := m.transition(ctx, actor, itemID, input.ExpectedVersion, "execution", "acceptance", func(tx *sql.Tx, current *WorkItem, now string) error {
		var number, conclusion int64
		_ = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(number),0)+1 FROM execution_attempts WHERE work_item_id=?", itemID).Scan(&number)
		_ = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),1) FROM discussion_conclusions WHERE work_item_id=?", itemID).Scan(&conclusion)
		eid := id()
		commits, _ := json.Marshal(input.Commits)
		files, _ := json.Marshal(input.ChangedFiles)
		_, err := tx.ExecContext(ctx, "INSERT INTO execution_attempts(id,work_item_id,number,assignee_kind,assignee_id,conclusion_version,target_branch,task_branch,lease_generation,summary_markdown,commits_json,changed_files_json,remaining_risks_markdown,pushed_at,started_at,ended_at,end_reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", eid, itemID, number, current.AssigneeKind, current.AssigneeID, conclusion, current.TargetBranch, "projectboard/"+strings.ToLower(strings.Split(itemID, "-")[0])+"/"+fmt.Sprint(current.Number), 0, input.Summary, string(commits), string(files), input.RemainingRisks, now, now, now, "submitted")
		if err != nil {
			return err
		}
		for _, v := range input.Validations {
			required := 0
			if v.Required {
				required = 1
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO validation_runs(id,execution_attempt_id,name,required,exit_code,duration_ms,log_summary,created_at) VALUES(?,?,?,?,?,?,?,?)", id(), eid, v.Name, required, v.ExitCode, v.DurationMS, v.LogSummary, now)
			if err != nil {
				return err
			}
		}
		return timeline(ctx, tx, itemID, "validation", "acceptance", actor, map[string]any{"executionAttemptId": eid, "validations": input.Validations}, current.Version+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

type AcceptanceInput struct {
	ExpectedVersion int64
	Outcome, Note   string
	CriteriaResults any
}

func (m *Module) Accept(ctx context.Context, actor Actor, itemID string, input AcceptanceInput) (*WorkItem, error) {
	next := "execution"
	if input.Outcome == "pass" {
		next = "completed"
		if actor.Type == "agent" {
			var allowTaskClose, allowSubtaskClose int
			var parentID sql.NullString
			err := m.store.DB.QueryRowContext(ctx, `SELECT p.allow_agent_auto_close,p.allow_agent_auto_close_subtasks,w.parent_id
				FROM work_items w JOIN projects p ON p.id=w.project_id WHERE w.id=?`, itemID).Scan(&allowTaskClose, &allowSubtaskClose, &parentID)
			if err != nil {
				return nil, err
			}
			if (!parentID.Valid && allowTaskClose != 0) || (parentID.Valid && allowSubtaskClose != 0) {
				next = "order_closed"
			}
		}
	} else if input.Outcome == "scope_unclear" {
		next = "discussion"
	} else if input.Outcome == "abandon" {
		next = "abandoned"
	}
	err := m.transition(ctx, actor, itemID, input.ExpectedVersion, "acceptance", next, func(tx *sql.Tx, current *WorkItem, now string) error {
		var number, conclusion int64
		_ = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(number),0)+1 FROM acceptance_attempts WHERE work_item_id=?", itemID).Scan(&number)
		_ = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),1) FROM discussion_conclusions WHERE work_item_id=?", itemID).Scan(&conclusion)
		criteria, _ := json.Marshal(input.CriteriaResults)
		_, err := tx.ExecContext(ctx, "INSERT INTO acceptance_attempts(id,work_item_id,number,acceptor_type,acceptor_id,conclusion_version,outcome,note_markdown,criteria_results_json,started_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", id(), itemID, number, actor.Type, actor.ID, conclusion, input.Outcome, input.Note, string(criteria), now, now)
		if err != nil {
			return err
		}
		return timeline(ctx, tx, itemID, "acceptance_result", next, actor, map[string]any{"outcome": input.Outcome, "noteMarkdown": input.Note}, current.Version+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) AddMessage(ctx context.Context, actor Actor, itemID, markdown string, expectedVersion int64) (*WorkItem, error) {
	if strings.TrimSpace(markdown) == "" {
		return nil, &Error{422, "VALIDATION_ERROR", "Message is required"}
	}
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if expectedVersion > 0 && current.Version != expectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		raw, _ := json.Marshal(map[string]any{"markdown": markdown})
		_, err = tx.ExecContext(ctx, "INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) VALUES(?,?,?,?,?,?,?,?,?)", id(), itemID, "message", current.Stage, actor.Type, nullable(actor.ID), string(raw), current.Version, timestamp(m.now()))
		return err
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) SetBlocked(ctx context.Context, actor Actor, itemID string, expectedVersion int64, reason string, blocked bool) (*WorkItem, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.Stage == "completed" || current.Stage == "order_closed" || current.Stage == "abandoned" {
			return &Error{409, "TERMINAL", "Terminal work items cannot change"}
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
		result, err := tx.ExecContext(ctx, "UPDATE work_items SET blocked_at=?,blocked_reason=?,version=version+1,updated_at=? WHERE id=? AND version=?", at, why, stamp, itemID, expectedVersion)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		return timeline(ctx, tx, itemID, kind, current.Stage, actor, map[string]any{"reason": reason}, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) Abandon(ctx context.Context, actor Actor, itemID string, expectedVersion int64, reason string) (*WorkItem, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, &Error{422, "REASON_REQUIRED", "Abandon reason required"}
	}
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.Stage == "completed" || current.Stage == "order_closed" || current.Stage == "abandoned" {
			return &Error{409, "TERMINAL", "Terminal work item cannot change"}
		}
		stamp := timestamp(m.now())
		_, err = tx.ExecContext(ctx, "UPDATE work_items SET stage='abandoned',abandoned_at=?,abandoned_reason=?,abandoned_from_stage=?,version=version+1,updated_at=? WHERE id=?", stamp, reason, current.Stage, stamp, itemID)
		if err != nil {
			return err
		}
		if err = timeline(ctx, tx, itemID, "abandoned", "abandoned", actor, map[string]any{"from": current.Stage, "reason": reason}, expectedVersion+1); err != nil {
			return err
		}
		return timeline(ctx, tx, itemID, "stage_transition", "abandoned", actor, map[string]any{"from": current.Stage, "to": "abandoned"}, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) Assign(ctx context.Context, actor Actor, itemID string, expectedVersion int64, kind, assigneeID, reason string) (*WorkItem, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.Stage == "completed" || current.Stage == "order_closed" || current.Stage == "abandoned" {
			return &Error{409, "TERMINAL", "Terminal work item cannot change"}
		}
		stamp := timestamp(m.now())
		stage := current.Stage
		if stage == "todo" {
			stage = "discussion"
		}
		_, err = tx.ExecContext(ctx, "UPDATE work_items SET assignee_kind=?,assignee_id=?,assignment_state='reserved',stage=?,version=version+1,lease_generation=lease_generation+1,updated_at=? WHERE id=?", kind, assigneeID, stage, stamp, itemID)
		if err != nil {
			return err
		}
		if err = timeline(ctx, tx, itemID, "assignment_changed", stage, actor, map[string]any{"kind": kind, "id": assigneeID, "reason": reason}, expectedVersion+1); err != nil {
			return err
		}
		if stage != current.Stage {
			return timeline(ctx, tx, itemID, "stage_transition", stage, actor, map[string]any{"from": current.Stage, "to": stage}, expectedVersion+1)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

type UpdateInput struct {
	ExpectedVersion                                      int64
	Title, Description, Criteria, Priority, TargetBranch string
	Stage, DiscussionMode, ExecutionMode, AcceptanceMode string
	AssigneeKind, AssigneeID                             string
	FollowerIDs                                          []string
	Configure                                            bool
}

func (m *Module) Update(ctx context.Context, actor Actor, itemID string, input UpdateInput) (*WorkItem, error) {
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != input.ExpectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.Stage == "completed" || current.Stage == "order_closed" || current.Stage == "abandoned" {
			return &Error{409, "TERMINAL", "Terminal work item cannot change"}
		}
		var title, description, criteria, priority, branch, discussion, execution, acceptance string
		err = tx.QueryRowContext(ctx, "SELECT title,description_markdown,acceptance_criteria_markdown,priority,target_branch,discussion_mode,execution_mode,acceptance_mode FROM work_items WHERE id=?", itemID).Scan(&title, &description, &criteria, &priority, &branch, &discussion, &execution, &acceptance)
		if err != nil {
			return err
		}
		if input.Title != "" {
			title = input.Title
		}
		if input.Description != "" {
			description = input.Description
		}
		if input.Criteria != "" {
			criteria = input.Criteria
		}
		if input.Priority != "" {
			priority = input.Priority
		}
		if input.DiscussionMode != "" {
			discussion = input.DiscussionMode
		}
		if input.ExecutionMode != "" {
			execution = input.ExecutionMode
		}
		if input.AcceptanceMode != "" {
			acceptance = input.AcceptanceMode
		}
		stage := current.Stage
		if input.Stage != "" {
			stage = input.Stage
		}
		if input.TargetBranch != "" && input.TargetBranch != branch {
			branch = input.TargetBranch
			if !input.Configure && stage != "todo" {
				stage = "discussion"
			}
		}
		stamp := timestamp(m.now())
		assigneeKind, assigneeID, assignmentState := nullable(pointerValue(current.AssigneeKind)), nullable(pointerValue(current.AssigneeID)), any("reserved")
		if input.Configure {
			assigneeKind, assigneeID = nullable(input.AssigneeKind), nullable(input.AssigneeID)
			if input.AssigneeID == "" {
				assignmentState = nil
			}
			_, _ = tx.ExecContext(ctx, "UPDATE assignments SET ended_at=?,ended_reason='configuration_changed' WHERE work_item_id=? AND ended_at IS NULL", stamp, itemID)
			_, _ = tx.ExecContext(ctx, "UPDATE leases SET released_at=?,release_reason='configuration_changed' WHERE work_item_id=? AND released_at IS NULL", stamp, itemID)
			if input.AssigneeID != "" {
				_, err = tx.ExecContext(ctx, "INSERT INTO assignments(id,work_item_id,assignee_kind,assignee_id,state,generation,created_at) VALUES(?,?,?,?,?,?,?)", id(), itemID, input.AssigneeKind, input.AssigneeID, "reserved", current.Version+1, stamp)
				if err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE work_items SET title=?,description_markdown=?,acceptance_criteria_markdown=?,priority=?,target_branch=?,stage=?,discussion_mode=?,execution_mode=?,acceptance_mode=?,assignee_kind=?,assignee_id=?,assignment_state=?,version=version+1,lease_generation=lease_generation+1,updated_at=? WHERE id=?", title, description, criteria, priority, branch, stage, discussion, execution, acceptance, assigneeKind, assigneeID, assignmentState, stamp, itemID)
		if err != nil {
			return err
		}
		if input.Configure {
			if _, err = tx.ExecContext(ctx, "DELETE FROM work_item_followers WHERE work_item_id=?", itemID); err != nil {
				return err
			}
			for _, followerID := range input.FollowerIDs {
				if followerID == "" {
					continue
				}
				if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO work_item_followers(work_item_id,user_id,created_at) VALUES(?,?,?)", itemID, followerID, stamp); err != nil {
					return err
				}
			}
		}
		return timeline(ctx, tx, itemID, "work_item_configured", stage, actor, map[string]any{"stageChanged": stage != current.Stage, "assigneeChanged": input.Configure && (input.AssigneeKind != pointerValue(current.AssigneeKind) || input.AssigneeID != pointerValue(current.AssigneeID)), "followers": len(input.FollowerIDs)}, input.ExpectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func (m *Module) MoveStage(ctx context.Context, actor Actor, itemID string, expectedVersion int64, targetStage, note string) (*WorkItem, error) {
	allowed := map[string]bool{"todo": true, "discussion": true, "execution": true, "acceptance": true, "completed": true, "order_closed": true}
	if !allowed[targetStage] {
		return nil, &Error{422, "INVALID_STAGE", "Target stage is not supported"}
	}
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.Stage == "abandoned" {
			return &Error{409, "TERMINAL", "Abandoned work item cannot change stage"}
		}
		if current.Stage == targetStage {
			return &Error{422, "UNCHANGED_STAGE", "Choose a different target stage"}
		}
		stamp := timestamp(m.now())
		var completed any
		if targetStage == "completed" || targetStage == "order_closed" {
			completed = stamp
		}
		result, err := tx.ExecContext(ctx, "UPDATE work_items SET stage=?,completed_at=?,version=version+1,lease_generation=lease_generation+1,updated_at=? WHERE id=? AND version=?", targetStage, completed, stamp, itemID, expectedVersion)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if targetStage == "completed" || targetStage == "order_closed" {
			_, _ = tx.ExecContext(ctx, "UPDATE leases SET released_at=?,release_reason='stage_changed' WHERE work_item_id=? AND released_at IS NULL", stamp, itemID)
		}
		return timeline(ctx, tx, itemID, "stage_transition", targetStage, actor, map[string]any{"from": current.Stage, "to": targetStage, "noteMarkdown": note}, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, itemID)
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (m *Module) transition(ctx context.Context, actor Actor, itemID string, version int64, from, to string, inside func(*sql.Tx, *WorkItem, string) error) error {
	return m.store.Write(ctx, func(tx *sql.Tx) error {
		current, err := getTx(ctx, tx, itemID)
		if err != nil {
			return err
		}
		if current.Version != version {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if current.Stage != from {
			return &Error{409, "INVALID_STAGE", "Work item is not in the required stage"}
		}
		now := timestamp(m.now())
		completed := any(nil)
		if to == "completed" {
			completed = now
		}
		result, err := tx.ExecContext(ctx, "UPDATE work_items SET stage=?,version=version+1,updated_at=?,completed_at=COALESCE(?,completed_at) WHERE id=? AND version=?", to, now, completed, itemID, version)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return &Error{409, "VERSION_CONFLICT", "Work item version changed"}
		}
		if err = inside(tx, current, now); err != nil {
			return err
		}
		return timeline(ctx, tx, itemID, "stage_transition", to, actor, map[string]any{"from": from, "to": to}, version+1)
	})
}

func getTx(ctx context.Context, tx *sql.Tx, id string) (*WorkItem, error) {
	var w WorkItem
	err := tx.QueryRowContext(ctx, "SELECT id,project_id,number,title,stage,target_branch,assignee_kind,assignee_id,version FROM work_items WHERE id=?", id).Scan(&w.ID, &w.ProjectID, &w.Number, &w.Title, &w.Stage, &w.TargetBranch, &w.AssigneeKind, &w.AssigneeID, &w.Version)
	return &w, err
}
func timeline(ctx context.Context, tx *sql.Tx, itemID, kind, stage string, actor Actor, payload any, version int64) error {
	raw, _ := json.Marshal(payload)
	now := timestamp(time.Now())
	_, err := tx.ExecContext(ctx, "INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) VALUES(?,?,?,?,?,?,?,?,?)", id(), itemID, kind, stage, actor.Type, nullable(actor.ID), string(raw), version, now)
	return err
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
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
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if rows.Scan(ptrs...) != nil {
			continue
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := values[i].([]byte); ok {
				m[c] = string(b)
			} else {
				m[c] = values[i]
			}
			if strings.HasSuffix(c, "_json") {
				var decoded any
				if s, ok := m[c].(string); ok && json.Unmarshal([]byte(s), &decoded) == nil {
					m[strings.TrimSuffix(c, "_json")] = decoded
				}
			}
		}
		out = append(out, m)
	}
	return out
}

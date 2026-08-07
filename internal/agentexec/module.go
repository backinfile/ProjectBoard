// Package agentexec owns the authenticated Agent task lifecycle. Its interface
// is the only execution surface used by the SSH MCP transport.
package agentexec

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
)

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

type Module struct {
	store *store.Store
	queue *workqueue.Module
}

func New(database *store.Store, queue *workqueue.Module) *Module {
	return &Module{store: database, queue: queue}
}

type TaskSummary struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	ProjectKey    string `json:"projectKey"`
	Number        int64  `json:"number"`
	Title         string `json:"title"`
	Priority      string `json:"priority"`
	Stage         string `json:"stage"`
	TargetBranch  string `json:"targetBranch"`
	Version       int64  `json:"version"`
	Assigned      bool   `json:"assigned"`
	RepositoryURL string `json:"repositoryUrl"`
}

type Execution struct {
	ID             string              `json:"executionId"`
	LeaseID        string              `json:"leaseId"`
	ExpiresAt      string              `json:"expiresAt"`
	TaskBranch     string              `json:"taskBranch"`
	RepositoryURL  string              `json:"repositoryUrl"`
	RepositoryName string              `json:"repositoryName"`
	WorkItem       *workqueue.WorkItem `json:"workItem"`
	ProjectPolicy  map[string]any      `json:"projectPolicy"`
}

func (m *Module) Authorize(agentID, keyID string) error {
	var found int
	err := m.store.DB.QueryRow(`SELECT 1 FROM agent_ssh_keys k JOIN agents a ON a.id=k.agent_id
		WHERE k.id=? AND k.agent_id=? AND k.revoked_at IS NULL AND a.revoked_at IS NULL AND a.status='active'`, keyID, agentID).Scan(&found)
	if err != nil {
		return &Error{Code: "AGENT_AUTH_REVOKED", Message: "Agent or SSH key is no longer authorized"}
	}
	return nil
}

func (m *Module) ListProjects(ctx context.Context, agentID string) ([]map[string]any, error) {
	rows, err := m.store.DB.QueryContext(ctx, `SELECT p.id,p.project_key,p.name,p.repository_url,p.default_target_branch,g.allow_unassigned_claim
		FROM projects p JOIN agent_project_grants g ON g.project_id=p.id
		WHERE g.agent_id=? AND p.archived_at IS NULL ORDER BY p.name`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, key, name, repository, branch string
		var autoClaim int
		if err := rows.Scan(&id, &key, &name, &repository, &branch, &autoClaim); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "key": key, "name": name, "repositoryUrl": repository, "defaultTargetBranch": branch, "allowUnassignedClaim": autoClaim != 0})
	}
	return out, rows.Err()
}

func (m *Module) ListTasks(ctx context.Context, agentID string) ([]TaskSummary, error) {
	if err := m.expireLeases(ctx); err != nil {
		return nil, err
	}
	rows, err := m.store.DB.QueryContext(ctx, `SELECT w.id,w.project_id,p.project_key,w.number,w.title,w.priority,w.stage,w.target_branch,w.version,
		CASE WHEN w.assignee_kind='agent' AND w.assignee_id=? THEN 1 ELSE 0 END,p.repository_url
		FROM work_items w JOIN projects p ON p.id=w.project_id JOIN agent_project_grants g ON g.project_id=w.project_id AND g.agent_id=?
		WHERE p.archived_at IS NULL AND p.allow_agent_execution=1 AND w.blocked_at IS NULL
		AND w.stage NOT IN('completed','order_closed','abandoned')
		AND ((w.assignee_kind='agent' AND w.assignee_id=? AND w.assignment_state='reserved') OR (w.assignee_id IS NULL AND g.allow_unassigned_claim=1))
		AND NOT EXISTS(SELECT 1 FROM work_item_dependencies d JOIN work_items x ON x.id=d.depends_on_id WHERE d.work_item_id=w.id AND x.stage NOT IN('completed','order_closed'))
		ORDER BY CASE WHEN w.assignee_id=? THEN 0 ELSE 1 END,CASE w.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END,w.created_at`, agentID, agentID, agentID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskSummary{}
	for rows.Next() {
		var task TaskSummary
		var assigned int
		if err := rows.Scan(&task.ID, &task.ProjectID, &task.ProjectKey, &task.Number, &task.Title, &task.Priority, &task.Stage, &task.TargetBranch, &task.Version, &assigned, &task.RepositoryURL); err != nil {
			return nil, err
		}
		task.Assigned = assigned != 0
		out = append(out, task)
	}
	return out, rows.Err()
}

func (m *Module) expireLeases(ctx context.Context) error {
	return m.store.Write(ctx, func(tx *sql.Tx) error {
		stamp := now()
		if _, err := tx.ExecContext(ctx, "UPDATE leases SET released_at=?,release_reason='expired' WHERE released_at IS NULL AND expires_at<=?", stamp, stamp); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE agent_executions SET ended_at=?,end_reason='expired' WHERE ended_at IS NULL AND lease_id IN (SELECT id FROM leases WHERE released_at=? AND release_reason='expired')", stamp, stamp); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE assignments SET state='reserved' WHERE id IN (SELECT assignment_id FROM leases WHERE released_at=? AND release_reason='expired')", stamp); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE work_items SET assignment_state='reserved',version=version+1,updated_at=? WHERE id IN (SELECT work_item_id FROM leases WHERE released_at=? AND release_reason='expired')", stamp, stamp)
		return err
	})
}

func (m *Module) WaitForTask(ctx context.Context, agentID string, timeout time.Duration) (*TaskSummary, error) {
	if timeout <= 0 || timeout > 55*time.Second {
		timeout = 55 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		tasks, err := m.ListTasks(ctx, agentID)
		if err != nil {
			return nil, err
		}
		if len(tasks) > 0 {
			return &tasks[0], nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, nil
		case <-ticker.C:
		}
	}
}

func (m *Module) Claim(ctx context.Context, agentID, itemID string, expectedVersion int64) (*Execution, error) {
	if err := m.expireLeases(ctx); err != nil {
		return nil, err
	}
	var executionID, leaseID, expires, projectID, projectKey, repositoryURL, repositoryName, promptsRaw string
	var number int64
	var allowSubtasks, allowAutoClose, allowAutoCloseSubtasks int
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		stamp := now()
		var assigneeKind, assigneeID, assignmentState, stage string
		var assigneeKindPtr, assigneeIDPtr, assignmentStatePtr *string
		var version int64
		var blockedAt *string
		var allowExecution, allowUnassigned int
		if err := tx.QueryRow(`SELECT w.project_id,p.project_key,w.number,w.assignee_kind,w.assignee_id,w.assignment_state,w.stage,w.blocked_at,w.version,
			p.allow_agent_execution,g.allow_unassigned_claim,p.repository_url,p.allow_subtasks,p.allow_agent_auto_close,p.allow_agent_auto_close_subtasks,p.agent_prompts_json
			FROM work_items w JOIN projects p ON p.id=w.project_id JOIN agent_project_grants g ON g.project_id=w.project_id AND g.agent_id=? WHERE w.id=?`, agentID, itemID).
			Scan(&projectID, &projectKey, &number, &assigneeKindPtr, &assigneeIDPtr, &assignmentStatePtr, &stage, &blockedAt, &version, &allowExecution, &allowUnassigned, &repositoryURL, &allowSubtasks, &allowAutoClose, &allowAutoCloseSubtasks, &promptsRaw); err != nil {
			if err == sql.ErrNoRows {
				return &Error{Code: "PROJECT_ACCESS_REQUIRED", Message: "Task is not available to this Agent"}
			}
			return err
		}
		if assigneeKindPtr != nil {
			assigneeKind = *assigneeKindPtr
		}
		if assigneeIDPtr != nil {
			assigneeID = *assigneeIDPtr
		}
		if assignmentStatePtr != nil {
			assignmentState = *assignmentStatePtr
		}
		if version != expectedVersion {
			return &Error{Code: "VERSION_CONFLICT", Message: "Work item version changed"}
		}
		if allowExecution == 0 || blockedAt != nil || terminal(stage) {
			return &Error{Code: "NOT_CLAIMABLE", Message: "Task cannot be claimed"}
		}
		if err := tx.QueryRow(`SELECT g.repository_name FROM project_repository_grants g
			JOIN provider_authorizations a ON a.id=g.authorization_id
			WHERE g.project_id=? AND g.revoked_at IS NULL AND g.access_level='write' AND a.status='active' AND a.provider='github'`, projectID).Scan(&repositoryName); err != nil {
			return &Error{Code: "GITHUB_GRANT_REQUIRED", Message: "A valid GitHub repository grant is required before this task can be claimed"}
		}
		assigned := assigneeKind == "agent" && assigneeID == agentID && assignmentState == "reserved"
		unassigned := assigneeID == "" && allowUnassigned != 0
		if !assigned && !unassigned {
			return &Error{Code: "NOT_ASSIGNED", Message: "Task is neither assigned nor available for automatic claim"}
		}
		var active int
		if tx.QueryRow("SELECT 1 FROM leases WHERE agent_id=? AND released_at IS NULL AND expires_at>?", agentID, stamp).Scan(&active) == nil {
			return &Error{Code: "AGENT_CAPACITY_EXCEEDED", Message: "Agent already has an active task"}
		}
		assignmentID := ""
		if assigned {
			if err := tx.QueryRow("SELECT id FROM assignments WHERE work_item_id=? AND ended_at IS NULL ORDER BY created_at DESC LIMIT 1", itemID).Scan(&assignmentID); err != nil {
				return &Error{Code: "NO_ASSIGNMENT", Message: "Task assignment is missing"}
			}
			if _, err := tx.Exec("UPDATE assignments SET state='active' WHERE id=?", assignmentID); err != nil {
				return err
			}
		} else {
			assignmentID = security.Token(18)
			if _, err := tx.Exec("INSERT INTO assignments(id,work_item_id,assignee_kind,assignee_id,state,generation,created_at) VALUES(?,?,'agent',?,'active',1,?)", assignmentID, itemID, agentID, stamp); err != nil {
				return err
			}
		}
		leaseID, executionID = security.Token(18), security.Token(18)
		expires = time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
		if _, err := tx.Exec("INSERT INTO leases(id,work_item_id,assignment_id,agent_id,generation,expires_at,created_at) VALUES(?,?,?,?,1,?,?)", leaseID, itemID, assignmentID, agentID, expires, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO agent_executions(id,work_item_id,agent_id,lease_id,state,created_at) VALUES(?,?,?,?,?,?)", executionID, itemID, agentID, leaseID, stage, stamp); err != nil {
			return err
		}
		result, err := tx.Exec("UPDATE work_items SET assignee_kind='agent',assignee_id=?,assignment_state='active',version=version+1,updated_at=? WHERE id=? AND version=?", agentID, stamp, itemID, expectedVersion)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return &Error{Code: "VERSION_CONFLICT", Message: "Work item version changed"}
		}
		payload, _ := json.Marshal(map[string]any{"executionId": executionID, "leaseId": leaseID})
		_, err = tx.Exec("INSERT INTO activity_events(id,project_id,actor_type,actor_id,event_type,object_type,object_id,source,payload_json,created_at) VALUES(?,?,'agent',?,'agent.execution_claimed','work_item',?,'ssh_mcp',?,?)", security.Token(18), projectID, agentID, itemID, string(payload), stamp)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	item, err := m.queue.Get(ctx, itemID)
	if err != nil {
		return nil, err
	}
	prompts := []string{}
	_ = json.Unmarshal([]byte(promptsRaw), &prompts)
	return &Execution{ID: executionID, LeaseID: leaseID, ExpiresAt: expires, TaskBranch: fmt.Sprintf("projectboard/%s/%d", strings.ToLower(projectKey), number), RepositoryURL: repositoryURL, RepositoryName: repositoryName, WorkItem: item, ProjectPolicy: map[string]any{"allowSubtasks": allowSubtasks != 0, "allowAgentAutoClose": allowAutoClose != 0, "allowAgentAutoCloseSubtasks": allowAutoCloseSubtasks != 0, "agentPrompts": prompts}}, nil
}

func (m *Module) Active(ctx context.Context, agentID string) (*Execution, error) {
	var result Execution
	var itemID, projectKey string
	var number int64
	err := m.store.DB.QueryRowContext(ctx, `SELECT e.id,e.lease_id,l.expires_at,e.work_item_id,p.project_key,w.number,p.repository_url,COALESCE(g.repository_name,'')
		FROM agent_executions e JOIN leases l ON l.id=e.lease_id JOIN work_items w ON w.id=e.work_item_id JOIN projects p ON p.id=w.project_id
		JOIN agent_project_grants ag ON ag.project_id=w.project_id AND ag.agent_id=e.agent_id
		LEFT JOIN project_repository_grants g ON g.project_id=p.id AND g.revoked_at IS NULL
		WHERE e.agent_id=? AND e.ended_at IS NULL AND l.released_at IS NULL AND l.expires_at>? ORDER BY e.created_at DESC LIMIT 1`, agentID, now()).
		Scan(&result.ID, &result.LeaseID, &result.ExpiresAt, &itemID, &projectKey, &number, &result.RepositoryURL, &result.RepositoryName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result.TaskBranch = fmt.Sprintf("projectboard/%s/%d", strings.ToLower(projectKey), number)
	result.WorkItem, err = m.queue.Get(ctx, itemID)
	return &result, err
}

func (m *Module) Heartbeat(ctx context.Context, agentID, executionID, leaseID string) (string, error) {
	expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	result, err := m.store.DB.ExecContext(ctx, `UPDATE leases SET expires_at=? WHERE id=? AND agent_id=? AND released_at IS NULL
		AND EXISTS(SELECT 1 FROM agent_executions e JOIN work_items w ON w.id=e.work_item_id
		JOIN agent_project_grants g ON g.project_id=w.project_id AND g.agent_id=e.agent_id
		WHERE e.id=? AND e.lease_id=leases.id AND e.ended_at IS NULL)`, expires, leaseID, agentID, executionID)
	if err != nil {
		return "", err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return "", &Error{Code: "LEASE_INVALID", Message: "Lease or execution is no longer valid"}
	}
	return expires, nil
}

func (m *Module) Release(ctx context.Context, agentID, executionID, leaseID, reason string, block bool) error {
	if strings.TrimSpace(reason) == "" {
		reason = "released"
	}
	return m.store.Write(ctx, func(tx *sql.Tx) error {
		var itemID, projectID string
		if err := tx.QueryRow(`SELECT e.work_item_id FROM agent_executions e JOIN leases l ON l.id=e.lease_id
			JOIN work_items w ON w.id=e.work_item_id JOIN agent_project_grants g ON g.project_id=w.project_id AND g.agent_id=e.agent_id
			WHERE e.id=? AND e.agent_id=? AND l.id=? AND e.ended_at IS NULL AND l.released_at IS NULL`, executionID, agentID, leaseID).Scan(&itemID); err != nil {
			return &Error{Code: "LEASE_INVALID", Message: "Execution is no longer active"}
		}
		if err := tx.QueryRow("SELECT project_id FROM work_items WHERE id=?", itemID).Scan(&projectID); err != nil {
			return err
		}
		stamp := now()
		if _, err := tx.Exec("UPDATE leases SET released_at=?,release_reason=? WHERE id=?", stamp, reason, leaseID); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE agent_executions SET ended_at=?,end_reason=? WHERE id=?", stamp, reason, executionID); err != nil {
			return err
		}
		if block {
			if _, err := tx.Exec("UPDATE work_items SET blocked_at=?,blocked_reason=?,assignment_state='reserved',version=version+1,updated_at=? WHERE id=?", stamp, reason, stamp, itemID); err != nil {
				return err
			}
		} else if _, err := tx.Exec("UPDATE work_items SET assignment_state='reserved',version=version+1,updated_at=? WHERE id=?", stamp, itemID); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"executionId": executionID, "leaseId": leaseID, "reason": reason, "blocked": block})
		_, err := tx.Exec("INSERT INTO activity_events(id,project_id,actor_type,actor_id,event_type,object_type,object_id,source,payload_json,created_at) VALUES(?,?,'agent',?,'agent.execution_released','work_item',?,'ssh_mcp',?,?)", security.Token(18), projectID, agentID, itemID, string(payload), stamp)
		return err
	})
}

func (m *Module) RecordEnvironment(ctx context.Context, agentID, executionID, command string, exitCode, durationMS int, summary string) error {
	if err := m.requireExecution(ctx, agentID, executionID); err != nil {
		return err
	}
	_, err := m.store.DB.ExecContext(ctx, "INSERT INTO agent_environment_steps(id,execution_id,command,exit_code,duration_ms,summary,created_at) VALUES(?,?,?,?,?,?,?)", security.Token(18), executionID, command, exitCode, durationMS, summary, now())
	return err
}

func (m *Module) RecordProgress(ctx context.Context, agentID, executionID, markdown string, expectedVersion int64) (*workqueue.WorkItem, error) {
	itemID, err := m.executionItem(ctx, agentID, executionID)
	if err != nil {
		return nil, err
	}
	return m.queue.AddMessage(ctx, workqueue.Actor{Type: "agent", ID: agentID}, itemID, markdown, expectedVersion)
}

func (m *Module) WorkItemID(ctx context.Context, agentID, executionID string) (string, error) {
	return m.executionItem(ctx, agentID, executionID)
}

func (m *Module) GetTask(ctx context.Context, agentID, itemID string) (*workqueue.WorkItem, error) {
	var allowed int
	if m.store.DB.QueryRowContext(ctx, `SELECT 1 FROM work_items w JOIN agent_project_grants g ON g.project_id=w.project_id
		WHERE w.id=? AND g.agent_id=?`, itemID, agentID).Scan(&allowed) != nil {
		return nil, &Error{Code: "PROJECT_ACCESS_REQUIRED", Message: "Task is outside this Agent's project grants"}
	}
	return m.queue.Get(ctx, itemID)
}

func (m *Module) FreezeConclusion(ctx context.Context, agentID, executionID string, input workqueue.ConclusionInput) (*workqueue.WorkItem, error) {
	itemID, err := m.executionItem(ctx, agentID, executionID)
	if err != nil {
		return nil, err
	}
	return m.queue.Freeze(ctx, workqueue.Actor{Type: "agent", ID: agentID}, itemID, input)
}

func (m *Module) MoveStage(ctx context.Context, agentID, executionID string, expectedVersion int64, target, note string) (*workqueue.WorkItem, error) {
	itemID, err := m.executionItem(ctx, agentID, executionID)
	if err != nil {
		return nil, err
	}
	return m.queue.MoveStage(ctx, workqueue.Actor{Type: "agent", ID: agentID}, itemID, expectedVersion, target, note)
}

func (m *Module) SetBlocked(ctx context.Context, agentID, executionID string, expectedVersion int64, reason string, blocked bool) (*workqueue.WorkItem, error) {
	itemID, err := m.executionItem(ctx, agentID, executionID)
	if err != nil {
		return nil, err
	}
	return m.queue.SetBlocked(ctx, workqueue.Actor{Type: "agent", ID: agentID}, itemID, expectedVersion, reason, blocked)
}

func (m *Module) SubmitExecution(ctx context.Context, agentID, executionID, leaseID string, input workqueue.ExecutionInput) (*workqueue.WorkItem, error) {
	itemID, err := m.executionItem(ctx, agentID, executionID)
	if err != nil {
		return nil, err
	}
	var valid int
	if m.store.DB.QueryRowContext(ctx, "SELECT 1 FROM leases WHERE id=? AND agent_id=? AND released_at IS NULL AND expires_at>?", leaseID, agentID, now()).Scan(&valid) != nil {
		return nil, &Error{Code: "LEASE_INVALID", Message: "Active lease required"}
	}
	item, err := m.queue.SubmitExecution(ctx, workqueue.Actor{Type: "agent", ID: agentID}, itemID, input)
	if err != nil {
		return nil, err
	}
	stamp := now()
	_, _ = m.store.DB.ExecContext(ctx, "UPDATE leases SET released_at=?,release_reason='submitted' WHERE id=?", stamp, leaseID)
	_, _ = m.store.DB.ExecContext(ctx, "UPDATE agent_executions SET ended_at=?,end_reason='submitted' WHERE id=?", stamp, executionID)
	return item, nil
}

func (m *Module) SubmitAcceptance(ctx context.Context, agentID, executionID string, input workqueue.AcceptanceInput) (*workqueue.WorkItem, error) {
	itemID, err := m.executionItem(ctx, agentID, executionID)
	if err != nil {
		return nil, err
	}
	return m.queue.Accept(ctx, workqueue.Actor{Type: "agent", ID: agentID}, itemID, input)
}

func (m *Module) requireExecution(ctx context.Context, agentID, executionID string) error {
	_, err := m.executionItem(ctx, agentID, executionID)
	return err
}

func (m *Module) executionItem(ctx context.Context, agentID, executionID string) (string, error) {
	var itemID string
	err := m.store.DB.QueryRowContext(ctx, `SELECT e.work_item_id FROM agent_executions e JOIN leases l ON l.id=e.lease_id
		JOIN work_items w ON w.id=e.work_item_id JOIN agent_project_grants g ON g.project_id=w.project_id AND g.agent_id=e.agent_id
		WHERE e.id=? AND e.agent_id=? AND e.ended_at IS NULL AND l.released_at IS NULL AND l.expires_at>?`, executionID, agentID, now()).Scan(&itemID)
	if err != nil {
		return "", &Error{Code: "ACTIVE_EXECUTION_REQUIRED", Message: "An active execution and lease are required"}
	}
	return itemID, nil
}

func terminal(stage string) bool {
	return stage == "completed" || stage == "order_closed" || stage == "abandoned"
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

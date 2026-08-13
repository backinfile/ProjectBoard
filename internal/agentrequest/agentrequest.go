package agentrequest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/store"
)

type Request struct {
	ID                string  `json:"id"`
	ProjectID         string  `json:"projectId"`
	Number            int64   `json:"number"`
	Kind              string  `json:"kind"`
	SourceWorkItemID  *string `json:"sourceWorkItemId,omitempty"`
	RetryOfID         *string `json:"retryOfId,omitempty"`
	Status            string  `json:"status"`
	Title             string  `json:"title"`
	PromptMarkdown    string  `json:"promptMarkdown,omitempty"`
	AssignedAgentID   *string `json:"assignedAgentId,omitempty"`
	ResultJSON        *string `json:"resultJson,omitempty"`
	OutputJSONL       string  `json:"outputJsonl,omitempty"`
	FinalMessage      *string `json:"finalMessage,omitempty"`
	WorkspacePath     *string `json:"workspacePath,omitempty"`
	ThreadID          *string `json:"threadId,omitempty"`
	CommandJSON       string  `json:"commandJson,omitempty"`
	ErrorMessage      *string `json:"errorMessage,omitempty"`
	Model             string  `json:"model,omitempty"`
	ReasoningEffort   string  `json:"reasoningEffort,omitempty"`
	InputTokens       int64   `json:"inputTokens"`
	CachedInputTokens int64   `json:"cachedInputTokens"`
	OutputTokens      int64   `json:"outputTokens"`
	ReasoningTokens   int64   `json:"reasoningTokens"`
	TotalTokens       int64   `json:"totalTokens"`
	CreatedByType     string  `json:"createdByType"`
	CreatedByID       *string `json:"createdById,omitempty"`
	CreatedAt         string  `json:"createdAt"`
	StartedAt         *string `json:"startedAt,omitempty"`
	EndedAt           *string `json:"endedAt,omitempty"`
}

type CreateInput struct {
	ProjectID, Kind, SourceWorkItemID, RetryOfID, Title, PromptMarkdown string
	CreatedByType, CreatedByID                                          string
}

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string { return e.Message }

type Module struct {
	store *store.Store
	now   func() time.Time
}

func New(s *store.Store) *Module { return &Module{store: s, now: time.Now} }

func (m *Module) Create(ctx context.Context, in CreateInput) (*Request, error) {
	var created *Request
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		var err error
		created, err = m.CreateTx(ctx, tx, in)
		return err
	})
	return created, err
}

func (m *Module) CreateTx(ctx context.Context, tx *sql.Tx, in CreateInput) (*Request, error) {
	if strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("project and title are required")
	}
	if in.CreatedByType == "" {
		in.CreatedByType = "system"
	}
	if (in.Kind == "task_plan" || in.Kind == "task_execution") && in.SourceWorkItemID != "" {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_requests WHERE source_work_item_id=? AND kind IN('task_plan','task_execution') AND status IN('queued','running')`, in.SourceWorkItemID).Scan(&active); err != nil {
			return nil, err
		}
		if active > 0 {
			return nil, &Error{409, "REQUEST_ACTIVE", "The task already has an active Agent request"}
		}
	}
	var projectKey string
	if err := tx.QueryRowContext(ctx, "SELECT project_key FROM projects WHERE id=?", in.ProjectID).Scan(&projectKey); err != nil {
		return nil, err
	}
	var number int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(number),0)+1 FROM agent_requests WHERE project_id=?", in.ProjectID).Scan(&number); err != nil {
		return nil, err
	}
	requestID := fmt.Sprintf("%s-AR-%d", strings.ToUpper(projectKey), number)
	stamp := m.now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_requests(id,project_id,number,kind,source_work_item_id,retry_of_id,status,title,prompt_markdown,created_by_type,created_by_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, requestID, in.ProjectID, number, in.Kind, nullable(in.SourceWorkItemID), nullable(in.RetryOfID), "queued", in.Title, in.PromptMarkdown, in.CreatedByType, nullable(in.CreatedByID), stamp)
	if err != nil {
		return nil, err
	}
	return getTx(ctx, tx, requestID)
}

func (m *Module) List(ctx context.Context, projectID string) ([]Request, error) {
	rows, err := m.store.DB.QueryContext(ctx, requestSelect+` WHERE project_id=? ORDER BY number DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		request, scanErr := scan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *request)
	}
	return out, rows.Err()
}

func (m *Module) Get(ctx context.Context, id string) (*Request, error) {
	request, err := scan(m.store.DB.QueryRowContext(ctx, requestSelect+` WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, &Error{404, "NOT_FOUND", "Agent request not found"}
	}
	return request, err
}

func (m *Module) Cancel(ctx context.Context, id string) (*Request, error) {
	stamp := m.now().UTC().Format(time.RFC3339Nano)
	result, err := m.store.DB.ExecContext(ctx, `UPDATE agent_requests SET status='cancelled',ended_at=? WHERE id=? AND status IN('queued','running')`, stamp, id)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		request, getErr := m.Get(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		return nil, &Error{409, "TERMINAL", "Agent request is already terminal: " + request.Status}
	}
	return m.Get(ctx, id)
}

func (m *Module) Retry(ctx context.Context, id, createdByID string) (*Request, error) {
	original, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if original.Status != "failed" && original.Status != "cancelled" && original.Status != "succeeded" {
		return nil, &Error{409, "REQUEST_ACTIVE", "Only terminal Agent requests can be retried"}
	}
	return m.Create(ctx, CreateInput{
		ProjectID: original.ProjectID, Kind: original.Kind, SourceWorkItemID: value(original.SourceWorkItemID),
		RetryOfID: original.ID, Title: original.Title, PromptMarkdown: original.PromptMarkdown,
		CreatedByType: "human", CreatedByID: createdByID,
	})
}

func (m *Module) ApprovePlan(ctx context.Context, id, createdByID string) (*Request, error) {
	plan, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if plan.Kind != "task_plan" || plan.Status != "succeeded" || plan.SourceWorkItemID == nil {
		return nil, &Error{409, "PLAN_NOT_READY", "Only a succeeded task plan can be approved"}
	}
	contextMarkdown := ""
	if plan.FinalMessage != nil {
		contextMarkdown = *plan.FinalMessage
	}
	return m.Create(ctx, CreateInput{ProjectID: plan.ProjectID, Kind: "task_execution", SourceWorkItemID: *plan.SourceWorkItemID, Title: strings.Replace(plan.Title, "处理任务", "执行计划", 1), PromptMarkdown: contextMarkdown, CreatedByType: "human", CreatedByID: createdByID})
}

func (m *Module) MaybeScheduleCompaction(ctx context.Context, projectID string) (*Request, error) {
	var created *Request
	err := m.store.Write(ctx, func(tx *sql.Tx) error {
		var days, requestCount int
		if err := tx.QueryRowContext(ctx, "SELECT knowledge_compaction_days,knowledge_compaction_request_count FROM projects WHERE id=?", projectID).Scan(&days, &requestCount); err != nil {
			return err
		}
		if days == 0 && requestCount == 0 {
			return nil
		}
		var active int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_requests WHERE project_id=? AND kind='project_knowledge_compaction' AND status IN('queued','running')", projectID).Scan(&active); err != nil || active > 0 {
			return err
		}
		var lastCompaction string
		_ = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(created_at),'') FROM agent_requests WHERE project_id=? AND kind='project_knowledge_compaction'", projectID).Scan(&lastCompaction)
		query := "SELECT COUNT(*),COALESCE(MIN(ended_at),'') FROM agent_requests WHERE project_id=? AND kind='task_knowledge' AND status='succeeded'"
		args := []any{projectID}
		if lastCompaction != "" {
			query += " AND ended_at>?"
			args = append(args, lastCompaction)
		}
		var succeeded int
		var oldest string
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&succeeded, &oldest); err != nil || succeeded == 0 {
			return err
		}
		countDue := requestCount > 0 && succeeded >= requestCount
		timeDue := false
		if days > 0 && oldest != "" {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, oldest); parseErr == nil {
				timeDue = !m.now().Before(parsed.Add(time.Duration(days) * 24 * time.Hour))
			}
		}
		if !countDue && !timeDue {
			return nil
		}
		var err error
		created, err = m.CreateTx(ctx, tx, CreateInput{ProjectID: projectID, Kind: "project_knowledge_compaction", Title: "整理项目知识库", CreatedByType: "system"})
		return err
	})
	return created, err
}

type scanner interface{ Scan(...any) error }

func getTx(ctx context.Context, tx *sql.Tx, id string) (*Request, error) {
	return scan(tx.QueryRowContext(ctx, requestSelect+` WHERE id=?`, id))
}

const requestSelect = `SELECT id,project_id,number,kind,source_work_item_id,retry_of_id,status,title,prompt_markdown,assigned_agent_id,result_json,output_jsonl,final_message,workspace_path,thread_id,command_json,error_message,model,reasoning_effort,input_tokens,cached_input_tokens,output_tokens,reasoning_tokens,total_tokens,created_by_type,created_by_id,created_at,started_at,ended_at FROM agent_requests`

func scan(row scanner) (*Request, error) {
	var request Request
	err := row.Scan(&request.ID, &request.ProjectID, &request.Number, &request.Kind, &request.SourceWorkItemID, &request.RetryOfID, &request.Status, &request.Title, &request.PromptMarkdown, &request.AssignedAgentID, &request.ResultJSON, &request.OutputJSONL, &request.FinalMessage, &request.WorkspacePath, &request.ThreadID, &request.CommandJSON, &request.ErrorMessage, &request.Model, &request.ReasoningEffort, &request.InputTokens, &request.CachedInputTokens, &request.OutputTokens, &request.ReasoningTokens, &request.TotalTokens, &request.CreatedByType, &request.CreatedByID, &request.CreatedAt, &request.StartedAt, &request.EndedAt)
	return &request, err
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}

package domain

import "time"

type TaskStatus string

const (
	TaskInbox       TaskStatus = "inbox"
	TaskPlanning    TaskStatus = "planning"
	TaskReady       TaskStatus = "ready"
	TaskInProgress  TaskStatus = "in_progress"
	TaskWaitingUser TaskStatus = "waiting_user"
	TaskVerifying   TaskStatus = "verifying"
	TaskCompleted   TaskStatus = "completed"
	TaskCancelled   TaskStatus = "cancelled"
)

var TaskStatuses = []TaskStatus{TaskInbox, TaskPlanning, TaskReady, TaskInProgress, TaskWaitingUser, TaskVerifying, TaskCompleted, TaskCancelled}

func (s TaskStatus) Valid() bool {
	for _, candidate := range TaskStatuses {
		if s == candidate {
			return true
		}
	}
	return false
}

type RunStatus string

const (
	RunNotStarted      RunStatus = "not_started"
	RunQueued          RunStatus = "queued"
	RunRunning         RunStatus = "running"
	RunWaitingApproval RunStatus = "waiting_approval"
	RunWaitingInput    RunStatus = "waiting_input"
	RunPaused          RunStatus = "paused"
	RunSucceeded       RunStatus = "succeeded"
	RunFailed          RunStatus = "failed"
	RunCancelled       RunStatus = "cancelled"
	RunInterrupted     RunStatus = "interrupted"
)

type Project struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Path           string    `json:"path"`
	Color          string    `json:"color"`
	DefaultBranch  string    `json:"defaultBranch"`
	DefaultAgentID string    `json:"defaultAgentId,omitempty"`
	Archived       bool      `json:"archived"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Task struct {
	ID          string                `json:"id"`
	ProjectID   string                `json:"projectId"`
	Key         string                `json:"key"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Status      TaskStatus            `json:"status"`
	RunStatus   RunStatus             `json:"runStatus"`
	Priority    string                `json:"priority"`
	Labels      []string              `json:"labels"`
	Acceptance  []AcceptanceCriterion `json:"acceptance"`
	DueAt       *time.Time            `json:"dueAt,omitempty"`
	Version     int64                 `json:"version"`
	DeletedAt   *time.Time            `json:"deletedAt,omitempty"`
	CreatedAt   time.Time             `json:"createdAt"`
	UpdatedAt   time.Time             `json:"updatedAt"`
}

type AcceptanceCriterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type AgentProfile struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"projectId,omitempty"`
	Name         string    `json:"name"`
	Model        string    `json:"model"`
	Reasoning    string    `json:"reasoning"`
	SystemPrompt string    `json:"systemPrompt"`
	Sandbox      string    `json:"sandbox"`
	Network      bool      `json:"network"`
	IsDefault    bool      `json:"isDefault"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Conversation struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"taskId"`
	AgentID       string    `json:"agentId"`
	Title         string    `json:"title"`
	Summary       string    `json:"summary"`
	CodexThreadID string    `json:"codexThreadId,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversationId"`
	Role           string    `json:"role"`
	Kind           string    `json:"kind"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Run struct {
	ID             string     `json:"id"`
	TaskID         string     `json:"taskId"`
	ConversationID string     `json:"conversationId"`
	AgentID        string     `json:"agentId"`
	WorkspaceID    string     `json:"workspaceId,omitempty"`
	Status         RunStatus  `json:"status"`
	Prompt         string     `json:"prompt"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type Event struct {
	ID       int64     `json:"id"`
	Type     string    `json:"type"`
	EntityID string    `json:"entityId,omitempty"`
	At       time.Time `json:"at"`
	Payload  any       `json:"payload,omitempty"`
}

type TaskWorkspace struct {
	ID         string `json:"id"`
	TaskID     string `json:"taskId"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	BaseCommit string `json:"baseCommit"`
	Kind       string `json:"kind"`
	ParentID   string `json:"parentId,omitempty"`
	State      string `json:"state"`
}

type ParallelAgent struct {
	AgentID     string `json:"agentId"`
	Instruction string `json:"instruction"`
}

type ParallelPlan struct {
	ID                 string          `json:"id"`
	TaskID             string          `json:"taskId"`
	MainAgentID        string          `json:"mainAgentId"`
	Prompt             string          `json:"prompt"`
	SnapshotCommit     string          `json:"snapshotCommit,omitempty"`
	Status             string          `json:"status"`
	Agents             []ParallelAgent `json:"agents"`
	AuthorizedChildren int             `json:"authorizedChildren"`
	CreatedAt          time.Time       `json:"createdAt"`
}

type ApprovalRequest struct {
	ID        string    `json:"id"`
	RunID     string    `json:"runId"`
	Method    string    `json:"method"`
	Detail    string    `json:"detail"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type Knowledge struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	Summary    string    `json:"summary"`
	Category   string    `json:"category"`
	Permission string    `json:"permission"`
	Locked     bool      `json:"locked"`
	Sensitive  bool      `json:"sensitive"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

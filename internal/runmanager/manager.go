package runmanager

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/projectboard/projectboard/internal/agent"
	"github.com/projectboard/projectboard/internal/domain"
	"github.com/projectboard/projectboard/internal/events"
	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workspace"
)

type Runner interface {
	Run(context.Context, agent.Request, agent.Handler) (agent.Result, error)
}

type Manager struct {
	store       *store.Store
	events      *events.Bus
	workspaces  *workspace.Manager
	runner      Runner
	queue       chan domain.Run
	limit       int
	mu          sync.Mutex
	active      int
	activeTasks map[string]bool
	approvals   map[string]chan agent.ApprovalDecision
	mcpURL      string
}

func New(st *store.Store, bus *events.Bus, wm *workspace.Manager, runner Runner, limit int, mcpURL ...string) *Manager {
	if limit < 1 {
		limit = 10
	}
	m := &Manager{store: st, events: bus, workspaces: wm, runner: runner, queue: make(chan domain.Run, 256), limit: limit, activeTasks: map[string]bool{}, approvals: map[string]chan agent.ApprovalDecision{}}
	if len(mcpURL) > 0 {
		m.mcpURL = mcpURL[0]
	}
	go m.schedule()
	return m
}
func (m *Manager) Enqueue(run domain.Run) error {
	select {
	case m.queue <- run:
		return nil
	default:
		return fmt.Errorf("run queue is full")
	}
}

func (m *Manager) schedule() {
	pending := []domain.Run{}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case r := <-m.queue:
			pending = append(pending, r)
		case <-ticker.C:
		}
		for len(pending) > 0 {
			m.mu.Lock()
			if m.active >= m.limit {
				m.mu.Unlock()
				break
			}
			index := -1
			for i, r := range pending {
				if !m.activeTasks[r.TaskID] {
					index = i
					break
				}
			}
			if index < 0 {
				m.mu.Unlock()
				break
			}
			run := pending[index]
			pending = append(pending[:index], pending[index+1:]...)
			m.active++
			m.activeTasks[run.TaskID] = true
			m.mu.Unlock()
			go m.execute(run)
		}
	}
}

func (m *Manager) execute(run domain.Run) {
	ctx := context.Background()
	defer func() { m.mu.Lock(); m.active--; delete(m.activeTasks, run.TaskID); m.mu.Unlock() }()
	finish := func(status domain.RunStatus, payload any) {
		_ = m.store.UpdateRun(ctx, run.ID, status)
		_ = m.store.SetTaskRunStatus(ctx, run.TaskID, status)
		_ = m.store.AddRunEvent(ctx, run.ID, "run.finished", payload)
		m.events.Publish(domain.Event{Type: "run." + string(status), EntityID: run.ID, Payload: payload})
	}
	_ = m.store.UpdateRun(ctx, run.ID, domain.RunRunning)
	_ = m.store.SetTaskRunStatus(ctx, run.TaskID, domain.RunRunning)
	m.events.Publish(domain.Event{Type: "run.running", EntityID: run.ID, Payload: run})
	task, err := m.store.GetTask(ctx, run.TaskID)
	if err != nil {
		finish(domain.RunFailed, map[string]any{"error": err.Error()})
		return
	}
	project, err := m.store.GetProject(ctx, task.ProjectID)
	if err != nil {
		finish(domain.RunFailed, map[string]any{"error": err.Error()})
		return
	}
	profile, err := m.store.GetAgent(ctx, run.AgentID)
	if err != nil {
		finish(domain.RunFailed, map[string]any{"error": err.Error()})
		return
	}
	conversation, err := m.store.GetConversation(ctx, run.ConversationID)
	if err != nil {
		finish(domain.RunFailed, map[string]any{"error": err.Error()})
		return
	}
	main, err := m.store.GetMainWorkspace(ctx, task.ID)
	if run.WorkspaceID != "" && !errors.Is(err, store.ErrNotFound) {
		main, err = m.store.GetWorkspace(ctx, run.WorkspaceID)
	}
	if err == store.ErrNotFound {
		created, e := m.workspaces.EnsureTask(ctx, project.Path, project.ID, task.ID, task.Key, project.DefaultBranch)
		err = e
		main = domain.TaskWorkspace{ID: created.ID, TaskID: created.TaskID, Path: created.Path, Branch: created.Branch, BaseCommit: created.BaseCommit, Kind: created.Kind, State: "ready"}
		if err == nil {
			err = m.store.SaveWorkspace(ctx, main)
		}
	}
	if err != nil {
		finish(domain.RunFailed, map[string]any{"error": err.Error()})
		return
	}
	token := ""
	if m.mcpURL != "" {
		raw := make([]byte, 32)
		if _, e := rand.Read(raw); e == nil {
			token = base64.RawURLEncoding.EncodeToString(raw)
			_ = m.store.IssueCapability(ctx, token, project.ID, task.ID, run.ID, []string{"knowledge.search", "knowledge.read", "task.conversations.list", "task.conversations.read"}, time.Now().Add(8*time.Hour))
			defer m.store.RevokeCapabilities(ctx, run.ID)
		}
	}
	h := &handler{manager: m, run: run}
	result, err := m.runner.Run(ctx, agent.Request{CWD: main.Path, Prompt: run.Prompt, ThreadID: conversation.CodexThreadID, MCPURL: m.mcpURL, MCPToken: token, Profile: agent.Profile{Model: profile.Model, Effort: profile.Reasoning, Network: profile.Network}}, h)
	if reply := h.AssistantText(); reply != "" {
		_, _ = m.store.AddMessage(ctx, domain.Message{ConversationID: conversation.ID, Role: "assistant", Content: reply})
	}
	if result.ThreadID != "" {
		_ = m.store.UpdateConversationThread(ctx, conversation.ID, result.ThreadID)
	}
	if err != nil {
		finish(domain.RunFailed, map[string]any{"error": err.Error()})
		return
	}
	finish(domain.RunSucceeded, result)
}

func (m *Manager) Decide(ctx context.Context, id string, decision agent.ApprovalDecision) error {
	m.mu.Lock()
	ch := m.approvals[id]
	m.mu.Unlock()
	if ch == nil {
		return store.ErrNotFound
	}
	if err := m.store.DecideApproval(ctx, id, string(decision)); err != nil {
		return err
	}
	select {
	case ch <- decision:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type handler struct {
	manager *Manager
	run     domain.Run
	mu      sync.Mutex
	text    strings.Builder
}

func (h *handler) OnEvent(e agent.Event) {
	redacted := security.Redact(string(e.Params))
	var payload any = json.RawMessage(redacted)
	_ = h.manager.store.AddRunEvent(context.Background(), h.run.ID, e.Method, payload)
	h.manager.events.Publish(domain.Event{Type: "run.event", EntityID: h.run.ID, Payload: map[string]any{"method": e.Method, "params": payload}})
	if e.Method == "item/agentMessage/delta" || e.Method == "item/agent_message/delta" {
		var value struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(e.Params, &value) == nil && value.Delta != "" {
			h.mu.Lock()
			h.text.WriteString(value.Delta)
			h.mu.Unlock()
		}
	}
}
func (h *handler) AssistantText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.TrimSpace(h.text.String())
}
func (h *handler) OnApproval(ctx context.Context, a agent.Approval) (agent.ApprovalDecision, error) {
	req, err := h.manager.store.CreateApproval(ctx, h.run.ID, a.Method, json.RawMessage(a.Params))
	if err != nil {
		return agent.Decline, err
	}
	ch := make(chan agent.ApprovalDecision, 1)
	h.manager.mu.Lock()
	h.manager.approvals[req.ID] = ch
	h.manager.mu.Unlock()
	defer func() { h.manager.mu.Lock(); delete(h.manager.approvals, req.ID); h.manager.mu.Unlock() }()
	_ = h.manager.store.UpdateRun(ctx, h.run.ID, domain.RunWaitingApproval)
	h.manager.events.Publish(domain.Event{Type: "approval.requested", EntityID: req.ID, Payload: req})
	select {
	case decision := <-ch:
		_ = h.manager.store.UpdateRun(ctx, h.run.ID, domain.RunRunning)
		return decision, nil
	case <-ctx.Done():
		return agent.Cancel, ctx.Err()
	}
}

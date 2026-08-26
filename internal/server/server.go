package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/agent"
	"github.com/projectboard/projectboard/internal/domain"
	"github.com/projectboard/projectboard/internal/events"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workspace"
	"github.com/projectboard/projectboard/web"
)

type Server struct {
	store          *store.Store
	events         *events.Bus
	workspaces     *workspace.Manager
	runs           RunController
	externalListen bool
	handler        http.Handler
}

type RunController interface {
	Enqueue(domain.Run) error
	Decide(context.Context, string, agent.ApprovalDecision) error
}

func New(st *store.Store, bus *events.Bus) *Server {
	return NewWithWorkspaceRoot(st, bus, filepath.Join(os.TempDir(), "ProjectBoard-worktrees"))
}
func NewWithWorkspaceRoot(st *store.Store, bus *events.Bus, root string) *Server {
	return NewWithRuntime(st, bus, workspace.NewManager(root), nil)
}
func NewWithRuntime(st *store.Store, bus *events.Bus, wm *workspace.Manager, runs RunController, listen ...string) *Server {
	s := &Server{store: st, events: bus, workspaces: wm, runs: runs}
	if len(listen) > 0 {
		host, _, err := net.SplitHostPort(listen[0])
		if err == nil {
			s.externalListen = host != "127.0.0.1" && host != "localhost" && host != "::1"
		}
	}
	mux := http.NewServeMux()
	s.routes(mux)
	s.handler = s.withHeaders(mux)
	return s
}
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "product": "ProjectBoard", "time": time.Now().UTC(), "externalListen": s.externalListen})
	})
	mux.HandleFunc("GET /api/v1/projects", s.listProjects)
	mux.HandleFunc("POST /api/v1/projects", s.createProject)
	mux.HandleFunc("GET /api/v1/projects/{projectID}", s.getProject)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/initialize-git", s.initializeGit)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/tasks", s.listTasks)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/tasks", s.createTask)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/dashboard", s.dashboard)
	mux.HandleFunc("GET /api/v1/tasks/{taskID}", s.getTask)
	mux.HandleFunc("PATCH /api/v1/tasks/{taskID}/status", s.updateTaskStatus)
	mux.HandleFunc("GET /api/v1/tasks/{taskID}/conversations", s.listConversations)
	mux.HandleFunc("POST /api/v1/tasks/{taskID}/conversations", s.createConversation)
	mux.HandleFunc("GET /api/v1/conversations/{conversationID}/messages", s.listMessages)
	mux.HandleFunc("POST /api/v1/conversations/{conversationID}/messages", s.createMessage)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/agents", s.listAgents)
	mux.HandleFunc("GET /api/v1/runs", s.listRuns)
	mux.HandleFunc("POST /api/v1/tasks/{taskID}/parallel-plans", s.createParallelPlan)
	mux.HandleFunc("POST /api/v1/parallel-plans/{planID}/approve", s.approveParallelPlan)
	mux.HandleFunc("POST /api/v1/worktrees/{workspaceID}/complete", s.completeChildWorkspace)
	mux.HandleFunc("POST /api/v1/candidates/{candidateID}/integrate", s.integrateCandidate)
	mux.HandleFunc("POST /api/v1/candidates/{candidateID}/reject", s.rejectCandidate)
	mux.HandleFunc("POST /api/v1/tasks/{taskID}/finalize", s.finalizeTask)
	mux.HandleFunc("POST /api/v1/approvals/{approvalID}/decide", s.decideApproval)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/knowledge", s.listKnowledge)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/knowledge", s.createKnowledge)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/automations", s.listAutomations)
	mux.HandleFunc("GET /api/v1/filesystem/drives", s.listDrives)
	mux.HandleFunc("GET /api/v1/filesystem/directories", s.listDirectories)
	mux.HandleFunc("GET /api/v1/search", s.search)
	mux.HandleFunc("GET /api/v1/events", s.streamEvents)
	mux.HandleFunc("GET /api/v1/templates", s.tableList("SELECT id,kind,name,description,body_json AS body,builtin,archived,created_at,updated_at FROM templates WHERE archived=0 ORDER BY builtin DESC,name"))
	mux.HandleFunc("GET /api/v1/audit", s.tableList("SELECT id,project_id AS projectId,task_id AS taskId,run_id AS runId,action,actor,detail_json AS detail,created_at AS createdAt FROM audit_events ORDER BY created_at DESC LIMIT 200"))
	mux.HandleFunc("GET /api/v1/notifications", s.tableList("SELECT id,project_id AS projectId,type,title,body,entity_id AS entityId,read_at AS readAt,created_at AS createdAt FROM notifications ORDER BY created_at DESC LIMIT 200"))
	mux.HandleFunc("POST /api/v1/notifications/read-all", s.markAllNotificationsRead)
	mux.HandleFunc("GET /api/v1/settings", s.tableList("SELECT key,value_json AS value,updated_at AS updatedAt FROM settings"))
	mux.HandleFunc("POST /api/v1/backups", s.createBackup)
	mux.HandleFunc("POST /mcp", s.mcp)
	dist, _ := fs.Sub(web.Dist, "dist")
	files := http.FileServer(http.FS(dist))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/" {
			if _, err := fs.Stat(dist, strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
}

func (s *Server) withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.ListProjects(r.Context())
	respond(w, v, err)
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var p domain.Project
	if !decode(w, r, &p) {
		return
	}
	v, err := s.store.CreateProject(r.Context(), p)
	if err == nil {
		s.events.Publish(domain.Event{Type: "project.created", EntityID: v.ID, Payload: v})
	}
	respondCreated(w, v, err)
}
func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	respond(w, v, err)
}
func (s *Server) initializeGit(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	commit, err := s.workspaces.InitializeRepository(r.Context(), project.Path, project.DefaultBranch)
	if err == nil {
		s.events.Publish(domain.Event{Type: "git.initialized", EntityID: project.ID, Payload: map[string]string{"commit": commit}})
	}
	respond(w, map[string]string{"baselineCommit": commit}, err)
}
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.ListTasks(r.Context(), r.PathValue("projectID"))
	respond(w, v, err)
}
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var t domain.Task
	if !decode(w, r, &t) {
		return
	}
	t.ProjectID = r.PathValue("projectID")
	v, err := s.store.CreateTask(r.Context(), t)
	if err == nil {
		s.events.Publish(domain.Event{Type: "task.created", EntityID: v.ID, Payload: v})
	}
	respondCreated(w, v, err)
}
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.GetTask(r.Context(), r.PathValue("taskID"))
	respond(w, v, err)
}
func (s *Server) updateTaskStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status  domain.TaskStatus `json:"status"`
		Version int64             `json:"version"`
	}
	if !decode(w, r, &body) {
		return
	}
	v, err := s.store.UpdateTaskStatus(r.Context(), r.PathValue("taskID"), body.Status, body.Version)
	if err == nil {
		s.events.Publish(domain.Event{Type: "task.updated", EntityID: v.ID, Payload: v})
	}
	respond(w, v, err)
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.Dashboard(r.Context(), r.PathValue("projectID"))
	respond(w, v, err)
}
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.ListAgents(r.Context(), r.PathValue("projectID"))
	respond(w, v, err)
}
func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.ListConversations(r.Context(), r.PathValue("taskID"))
	respond(w, v, err)
}
func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AgentID string `json:"agentId"`
	}
	if !decode(w, r, &body) {
		return
	}
	v, err := s.store.GetOrCreateConversation(r.Context(), r.PathValue("taskID"), body.AgentID)
	respondCreated(w, v, err)
}
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.ListMessages(r.Context(), r.PathValue("conversationID"))
	respond(w, v, err)
}
func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	var body struct{ TaskID, AgentID, Content string }
	if !decode(w, r, &body) {
		return
	}
	m, err := s.store.AddMessage(r.Context(), domain.Message{ConversationID: r.PathValue("conversationID"), Role: "user", Content: body.Content})
	if err != nil {
		respond(w, nil, err)
		return
	}
	run, err := s.store.CreateRun(r.Context(), domain.Run{TaskID: body.TaskID, ConversationID: m.ConversationID, AgentID: body.AgentID, Prompt: body.Content})
	if err == nil {
		s.events.Publish(domain.Event{Type: "run.queued", EntityID: run.ID, Payload: run})
		if s.runs != nil {
			err = s.runs.Enqueue(run)
		}
	}
	respondCreated(w, map[string]any{"message": m, "run": run}, err)
}
func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	if s.runs == nil {
		writeError(w, http.StatusServiceUnavailable, "执行服务未启用")
		return
	}
	var body struct {
		Decision agent.ApprovalDecision `json:"decision"`
	}
	if !decode(w, r, &body) {
		return
	}
	switch body.Decision {
	case agent.ApproveOnce, agent.ApproveSession, agent.Decline, agent.Cancel:
	default:
		writeError(w, http.StatusBadRequest, "invalid approval decision")
		return
	}
	err := s.runs.Decide(r.Context(), r.PathValue("approvalID"), body.Decision)
	respond(w, map[string]any{"status": "decided"}, err)
}
func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.ListRuns(r.Context(), r.URL.Query().Get("taskId"))
	respond(w, v, err)
}
func (s *Server) createParallelPlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MainAgentID, Prompt string
		Agents              []domain.ParallelAgent
	}
	if !decode(w, r, &body) {
		return
	}
	v, err := s.store.CreateParallelPlan(r.Context(), domain.ParallelPlan{TaskID: r.PathValue("taskID"), MainAgentID: body.MainAgentID, Prompt: body.Prompt, Agents: body.Agents})
	if err == nil {
		s.events.Publish(domain.Event{Type: "parallel_plan.proposed", EntityID: v.ID, Payload: v})
	}
	respondCreated(w, v, err)
}
func (s *Server) approveParallelPlan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	plan, err := s.store.GetParallelPlan(ctx, r.PathValue("planID"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	task, err := s.store.GetTask(ctx, plan.TaskID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	project, err := s.store.GetProject(ctx, task.ProjectID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	main, err := s.store.GetMainWorkspace(ctx, task.ID)
	if errors.Is(err, store.ErrNotFound) {
		created, e := s.workspaces.EnsureTask(ctx, project.Path, project.ID, task.ID, task.Key, project.DefaultBranch)
		err = e
		main = domain.TaskWorkspace{ID: created.ID, TaskID: created.TaskID, Path: created.Path, Branch: created.Branch, BaseCommit: created.BaseCommit, Kind: created.Kind, ParentID: created.ParentID, State: "ready"}
		if err == nil {
			err = s.store.SaveWorkspace(ctx, main)
		}
	}
	if err != nil {
		respond(w, nil, err)
		return
	}
	names := make([]string, 0, len(plan.Agents)-1)
	for _, a := range plan.Agents[1:] {
		names = append(names, a.AgentID)
	}
	snapshot, children, err := s.workspaces.CreateChildren(ctx, toWorkspace(main), plan.ID, names)
	if err != nil {
		respond(w, nil, err)
		return
	}
	for _, child := range children {
		_ = s.store.SaveWorkspace(ctx, domain.TaskWorkspace{ID: child.ID, TaskID: child.TaskID, Path: child.Path, Branch: child.Branch, BaseCommit: child.BaseCommit, Kind: child.Kind, ParentID: child.ParentID, State: "ready"})
	}
	err = s.store.ApproveParallelPlan(ctx, plan.ID, snapshot)
	if err == nil {
		started := []domain.Run{}
		for i, assignment := range plan.Agents {
			conversation, createErr := s.store.GetOrCreateConversation(ctx, task.ID, assignment.AgentID)
			if createErr != nil {
				err = createErr
				break
			}
			prompt := assignment.Instruction
			if strings.TrimSpace(prompt) == "" {
				prompt = plan.Prompt
			}
			_, createErr = s.store.AddMessage(ctx, domain.Message{ConversationID: conversation.ID, Role: "user", Content: prompt})
			if createErr != nil {
				err = createErr
				break
			}
			workspaceID := main.ID
			if i > 0 {
				workspaceID = children[i-1].ID
			}
			run, createErr := s.store.CreateRun(ctx, domain.Run{TaskID: task.ID, ConversationID: conversation.ID, AgentID: assignment.AgentID, WorkspaceID: workspaceID, Prompt: prompt})
			if createErr != nil {
				err = createErr
				break
			}
			started = append(started, run)
			if s.runs != nil {
				createErr = s.runs.Enqueue(run)
			}
			if createErr != nil {
				err = createErr
				break
			}
		}
		s.events.Publish(domain.Event{Type: "parallel_plan.approved", EntityID: plan.ID, Payload: map[string]any{"snapshotCommit": snapshot, "workspaces": children}})
		if err == nil {
			respond(w, map[string]any{"planId": plan.ID, "snapshotCommit": snapshot, "workspaces": children, "runs": started}, nil)
			return
		}
	}
	respond(w, map[string]any{"planId": plan.ID, "snapshotCommit": snapshot, "workspaces": children}, err)
}
func toWorkspace(w domain.TaskWorkspace) workspace.Workspace {
	return workspace.Workspace{ID: w.ID, TaskID: w.TaskID, Path: w.Path, Branch: w.Branch, BaseCommit: w.BaseCommit, Kind: w.Kind, ParentID: w.ParentID}
}
func (s *Server) completeChildWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Summary string `json:"summary"`
	}
	if !decode(w, r, &body) {
		return
	}
	stored, err := s.store.GetWorkspace(r.Context(), r.PathValue("workspaceID"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	candidate, err := s.workspaces.CompleteChild(r.Context(), toWorkspace(stored), body.Summary)
	if err != nil {
		respond(w, nil, err)
		return
	}
	id, err := s.store.SaveCandidate(r.Context(), candidate, body.Summary)
	if err == nil {
		s.events.Publish(domain.Event{Type: "candidate.ready", EntityID: id, Payload: candidate})
	}
	respondCreated(w, map[string]any{"id": id, "commit": candidate.Commit, "patch": candidate.Patch}, err)
}
func (s *Server) integrateCandidate(w http.ResponseWriter, r *http.Request) {
	candidate, _, err := s.store.GetCandidate(r.Context(), r.PathValue("candidateID"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	main, err := s.store.GetMainWorkspace(r.Context(), candidate.Workspace.TaskID)
	if err == nil {
		err = s.workspaces.IntegrateAndCleanup(r.Context(), toWorkspace(main), candidate)
	}
	if err == nil {
		err = s.store.DecideCandidate(r.Context(), r.PathValue("candidateID"), "integrated")
	}
	if err == nil {
		s.events.Publish(domain.Event{Type: "candidate.integrated", EntityID: r.PathValue("candidateID")})
	}
	respond(w, map[string]string{"status": "integrated"}, err)
}
func (s *Server) rejectCandidate(w http.ResponseWriter, r *http.Request) {
	candidate, _, err := s.store.GetCandidate(r.Context(), r.PathValue("candidateID"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	main, err := s.store.GetMainWorkspace(r.Context(), candidate.Workspace.TaskID)
	if err == nil {
		err = s.workspaces.RejectAndCleanup(r.Context(), toWorkspace(main), candidate)
	}
	if err == nil {
		err = s.store.DecideCandidate(r.Context(), r.PathValue("candidateID"), "rejected")
	}
	respond(w, map[string]string{"status": "rejected"}, err)
}
func (s *Server) finalizeTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string `json:"message"`
	}
	if !decode(w, r, &body) {
		return
	}
	task, err := s.store.GetTask(r.Context(), r.PathValue("taskID"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	project, err := s.store.GetProject(r.Context(), task.ProjectID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	main, err := s.store.GetMainWorkspace(r.Context(), task.ID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		body.Message = task.Key + ": " + task.Title
	}
	commit, err := s.workspaces.Finalize(r.Context(), project.Path, toWorkspace(main), project.DefaultBranch, body.Message)
	if err == nil {
		s.events.Publish(domain.Event{Type: "task.merged", EntityID: task.ID, Payload: map[string]string{"commit": commit}})
	}
	respond(w, map[string]string{"commit": commit}, err)
}
func (s *Server) listKnowledge(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.ListKnowledge(r.Context(), r.PathValue("projectID"))
	respond(w, v, err)
}
func (s *Server) createKnowledge(w http.ResponseWriter, r *http.Request) {
	var k domain.Knowledge
	if !decode(w, r, &k) {
		return
	}
	k.ProjectID = r.PathValue("projectID")
	v, err := s.store.CreateKnowledge(r.Context(), k)
	respondCreated(w, v, err)
}
func (s *Server) listAutomations(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.QueryRows(r.Context(), `SELECT id,project_id AS projectId,name,trigger_type AS triggerType,conditions_json AS conditions,actions_json AS actions,enabled,created_at AS createdAt,updated_at AS updatedAt FROM automation_rules WHERE project_id=? ORDER BY updated_at DESC`, r.PathValue("projectID"))
	respond(w, v, err)
}
func (s *Server) listDrives(w http.ResponseWriter, r *http.Request) {
	drives := []string{}
	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			p := string(c) + ":\\"
			if _, err := os.Stat(p); err == nil {
				drives = append(drives, p)
			}
		}
	} else {
		drives = append(drives, string(filepath.Separator))
	}
	respond(w, drives, nil)
}
func (s *Server) listDirectories(w http.ResponseWriter, r *http.Request) {
	rawPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if rawPath == "" || !filepath.IsAbs(rawPath) {
		writeError(w, http.StatusBadRequest, "目录路径必须是绝对路径")
		return
	}
	root := filepath.Clean(rawPath)
	entries, err := os.ReadDir(root)
	if err != nil {
		respond(w, nil, err)
		return
	}
	out := []map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, map[string]any{"name": entry.Name(), "path": filepath.Join(root, entry.Name())})
		}
	}
	respond(w, out, nil)
}
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.Search(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("projectId"))
	respond(w, v, err)
}
func (s *Server) tableList(query string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := s.store.QueryRows(r.Context(), query)
		respond(w, v, err)
	}
}
func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Directory string `json:"directory"`
	}
	if !decode(w, r, &body) {
		return
	}
	path, err := s.store.Backup(r.Context(), body.Directory)
	respondCreated(w, map[string]string{"path": path}, err)
}
func (s *Server) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	err := s.store.MarkAllNotificationsRead(r.Context())
	respond(w, map[string]string{"status": "ok"}, err)
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unavailable")
		return
	}
	after, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	ch, cancel := s.events.Subscribe(after)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprint(w, ": projectboard event stream\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, raw)
			flusher.Flush()
		case <-time.After(20 * time.Second):
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "无效的请求内容")
		return false
	}
	return true
}
func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, store.ErrConflict) {
			status = http.StatusConflict
		} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func respondCreated(w http.ResponseWriter, v any, err error) {
	if err != nil {
		respond(w, v, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": http.StatusText(status), "message": message}})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/agentexec"
	"github.com/projectboard/projectboard/internal/agentrequest"
	"github.com/projectboard/projectboard/internal/knowledge"
	"github.com/projectboard/projectboard/internal/projectrepo"
	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
	webassets "github.com/projectboard/projectboard/web"
)

type Config struct {
	DataDir           string
	DatabasePath      string
	BootstrapUsername string
	BootstrapPassword string
	Production        bool
	LookPath          func(string) (string, error)
	AgentRunner       agentexec.Runner
	AgentStallTimeout time.Duration
	AgentPollInterval time.Duration
}

type domainError struct {
	Status        int
	Code, Message string
	Details       any
}

func (e *domainError) Error() string { return e.Message }

type actor struct{ Type, ID, Role, Name, SessionID string }
type Server struct {
	store     *store.Store
	queue     *workqueue.Module
	config    Config
	static    fs.FS
	exec      *agentexec.Module
	requests  *agentrequest.Module
	knowledge *knowledge.Module
}

type Application struct {
	handler http.Handler
	server  *Server
}

func (a *Application) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.handler.ServeHTTP(w, r) }
func (a *Application) Close() error {
	a.server.exec.Close()
	return a.server.store.Close()
}

func NewTestHandler(dataDir string) *Application {
	handler, err := New(Config{DataDir: dataDir, BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123", LookPath: func(string) (string, error) { return "codex", nil }})
	if err != nil {
		panic(err)
	}
	return handler
}

func New(config Config) (*Application, error) {
	if config.LookPath == nil {
		config.LookPath = exec.LookPath
	}
	if config.DataDir == "" {
		config.DataDir = "./data"
	}
	if config.DatabasePath == "" {
		config.DatabasePath = filepath.Join(config.DataDir, "projectboard.db")
	}
	if err := os.MkdirAll(config.DataDir, 0o700); err != nil {
		return nil, err
	}
	database, err := store.Open(config.DatabasePath)
	if err != nil {
		return nil, err
	}
	if err = bootstrap(database, config); err != nil {
		database.Close()
		return nil, err
	}
	static, _ := fs.Sub(webassets.Files, ".")
	queue := workqueue.New(database)
	requests := agentrequest.New(database)
	knowledgeModule := knowledge.New(database)
	queue.SetOnClosed(func(ctx context.Context, tx *sql.Tx, event workqueue.ClosedEvent) error {
		_, createErr := requests.CreateTx(ctx, tx, agentrequest.CreateInput{
			ProjectID: event.WorkItem.ProjectID, Kind: "task_knowledge", SourceWorkItemID: event.WorkItem.ID,
			Title: "整理任务知识：" + event.WorkItem.ID + " " + event.WorkItem.Title, CreatedByType: "system",
		})
		return createErr
	})
	queue.SetOnAgentRequested(func(ctx context.Context, tx *sql.Tx, event workqueue.AgentRequestedEvent) error {
		_, createErr := requests.CreateTx(ctx, tx, agentrequest.CreateInput{
			ProjectID: event.ProjectID, Kind: event.Kind, SourceWorkItemID: event.WorkItemID,
			Title: "处理任务：" + event.WorkItemID + " " + event.Title, CreatedByType: "system",
		})
		return createErr
	})
	s := &Server{store: database, queue: queue, requests: requests, knowledge: knowledgeModule, config: config, static: static}
	knowledgeCommand, err := os.Executable()
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("resolve ProjectBoard executable for knowledge MCP: %w", err)
	}
	s.exec = agentexec.New(database, queue, requests, knowledgeModule, agentexec.Options{DataDir: config.DataDir, DatabasePath: config.DatabasePath, KnowledgeCommand: knowledgeCommand, Runner: config.AgentRunner, PollInterval: config.AgentPollInterval, StallTimeout: config.AgentStallTimeout})
	if err = s.exec.Start(); err != nil {
		database.Close()
		return nil, fmt.Errorf("start local Agent executor: %w", err)
	}
	return &Application{handler: securityHeaders(s.routes()), server: s}, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handle(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "database": "sqlite", "time": now()})
	}))
	mux.HandleFunc("POST /api/auth/login", s.handle(s.login))
	mux.HandleFunc("POST /api/auth/logout", s.handle(s.logout))
	mux.HandleFunc("GET /api/me", s.handle(s.me))
	mux.HandleFunc("PATCH /api/me", s.handle(s.updateMe))
	mux.HandleFunc("POST /api/me/change-password", s.handle(s.changePassword))
	mux.HandleFunc("GET /api/me/sessions", s.handle(s.sessions))
	mux.HandleFunc("DELETE /api/me/sessions/{id}", s.handle(s.revokeSession))
	mux.HandleFunc("GET /api/system/settings", s.handle(s.getSystemSettings))
	mux.HandleFunc("PUT /api/system/settings", s.handle(s.updateSystemSettings))
	mux.HandleFunc("GET /api/projects", s.handle(s.listProjects))
	mux.HandleFunc("POST /api/projects", s.handle(s.createProject))
	mux.HandleFunc("PATCH /api/projects/{id}", s.handle(s.updateProject))
	mux.HandleFunc("GET /api/projects/{id}/members", s.handle(s.listMembers))
	mux.HandleFunc("POST /api/projects/{id}/members", s.handle(s.addMember))
	mux.HandleFunc("PATCH /api/projects/{id}/members/{userId}", s.handle(s.updateMember))
	mux.HandleFunc("DELETE /api/projects/{id}/members/{userId}", s.handle(s.removeMember))
	mux.HandleFunc("GET /api/users", s.handle(s.listUsers))
	mux.HandleFunc("POST /api/users", s.handle(s.createUser))
	mux.HandleFunc("GET /api/users/{id}", s.handle(s.getUser))
	mux.HandleFunc("PATCH /api/users/{id}", s.handle(s.updateUser))
	mux.HandleFunc("DELETE /api/users/{id}", s.handle(s.deleteUser))
	mux.HandleFunc("POST /api/users/{id}/disable", s.handle(s.disableUser))
	mux.HandleFunc("POST /api/users/{id}/enable", s.handle(s.enableUser))
	mux.HandleFunc("POST /api/users/{id}/reset-password", s.handle(s.resetPassword))
	mux.HandleFunc("POST /api/agents", s.handle(s.createAgent))
	mux.HandleFunc("GET /api/agents", s.handle(s.listAgents))
	mux.HandleFunc("GET /api/agents/{id}/executions/current", s.handle(s.currentAgentExecutions))
	mux.HandleFunc("PATCH /api/agents/{id}", s.handle(s.updateAgent))
	mux.HandleFunc("POST /api/agents/{id}/disable", s.handle(s.disableOrganizationAgent))
	mux.HandleFunc("POST /api/agents/{id}/enable", s.handle(s.enableOrganizationAgent))
	mux.HandleFunc("DELETE /api/agents/{id}", s.handle(s.deleteAgent))
	mux.HandleFunc("GET /api/work-items", s.handle(s.listWorkItems))
	mux.HandleFunc("GET /api/agent-requests", s.handle(s.listAgentRequests))
	mux.HandleFunc("GET /api/agent-requests/{id}", s.handle(s.getAgentRequest))
	mux.HandleFunc("POST /api/agent-requests/{id}/cancel", s.handle(s.cancelAgentRequest))
	mux.HandleFunc("POST /api/agent-requests/{id}/retry", s.handle(s.retryAgentRequest))
	mux.HandleFunc("POST /api/agent-requests/{id}/approve-plan", s.handle(s.approveAgentPlan))
	mux.HandleFunc("POST /api/work-items/{id}/agent-request", s.handle(s.requestAgentForTask))
	mux.HandleFunc("GET /api/projects/{id}/knowledge", s.handle(s.listKnowledge))
	mux.HandleFunc("POST /api/projects/{id}/knowledge", s.handle(s.createKnowledgeNode))
	mux.HandleFunc("GET /api/knowledge/nodes/{id}", s.handle(s.getKnowledgeNode))
	mux.HandleFunc("PATCH /api/knowledge/nodes/{id}", s.handle(s.updateKnowledgeNode))
	mux.HandleFunc("DELETE /api/knowledge/nodes/{id}", s.handle(s.deleteKnowledgeNode))
	mux.HandleFunc("POST /api/knowledge/nodes/{id}/move", s.handle(s.moveKnowledgeNode))
	mux.HandleFunc("POST /api/knowledge/nodes/{id}/lock", s.handle(s.lockKnowledgeNode))
	mux.HandleFunc("GET /api/knowledge/nodes/{id}/revisions", s.handle(s.knowledgeRevisions))
	mux.HandleFunc("POST /api/knowledge/nodes/{id}/restore", s.handle(s.restoreKnowledgeNode))
	mux.HandleFunc("POST /api/work-items", s.handle(s.createWorkItem))
	mux.HandleFunc("GET /api/work-items/{id}", s.handle(s.getWorkItem))
	mux.HandleFunc("PATCH /api/work-items/{id}", s.handle(s.updateWorkItem))
	mux.HandleFunc("POST /api/work-items/{id}/stage", s.handle(s.moveWorkItemStage))
	mux.HandleFunc("POST /api/work-items/{id}/assign", s.handle(s.assignWorkItem))
	mux.HandleFunc("POST /api/work-items/{id}/messages", s.handle(s.postMessage))
	mux.HandleFunc("POST /api/work-items/{id}/agent-action", s.handle(s.agentAction))
	mux.HandleFunc("POST /api/work-items/{id}/attachments", s.handle(s.uploadAttachment))
	mux.HandleFunc("GET /api/attachments/{attachmentId}", s.handle(s.downloadAttachment))
	mux.HandleFunc("PUT /api/work-items/{id}/followers", s.handle(s.updateWorkItemFollowers))
	mux.HandleFunc("POST /api/work-items/{id}/block", s.handle(s.blockWorkItem))
	mux.HandleFunc("POST /api/work-items/{id}/unblock", s.handle(s.unblockWorkItem))
	mux.HandleFunc("DELETE /api/work-items/{id}/workspace", s.handle(s.cleanWorkItemWorkspace))
	mux.HandleFunc("GET /api/activity", s.handle(s.activity))
	mux.HandleFunc("GET /api/notifications", s.handle(s.listNotifications))
	mux.HandleFunc("POST /api/notifications/read-all", s.handle(s.readAllNotifications))
	mux.HandleFunc("POST /api/notifications/{id}/read", s.handle(s.readNotification))
	mux.HandleFunc("GET /api/activity/export", s.handle(s.exportActivity))
	mux.HandleFunc("GET /", s.serveStatic)
	return mux
}

func bootstrap(database *store.Store, config Config) error {
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	username := strings.TrimSpace(config.BootstrapUsername)
	if username == "" {
		username = "admin"
	}
	password := config.BootstrapPassword
	generated := false
	if password == "" {
		password = security.Token(24) + "-Aa1"
		generated = true
	}
	if !security.ValidPassword(password) {
		return fmt.Errorf("PROJECTBOARD_BOOTSTRAP_PASSWORD must be 12-256 characters with letters and numbers")
	}
	id := security.Token(18)
	stamp := now()
	_, err := database.DB.Exec("INSERT INTO users(id,username,display_name,password_hash,system_role,status,must_change_password,created_at,updated_at) VALUES(?,?,?,?,?,'active',0,?,?)", id, username, "Administrator", security.HashPassword(password), "administrator", stamp, stamp)
	if err == nil && generated {
		fmt.Printf("Bootstrap administrator created: %s\nGenerated bootstrap password: %s\n", username, password)
	}
	return err
}

func (s *Server) handle(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if err, ok := recovered.(error); ok {
					writeError(w, err)
					return
				}
				writeError(w, &domainError{500, "INTERNAL_ERROR", "Internal error", nil})
			}
		}()
		fn(w, r)
	}
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	decode(r, &in)
	username, ip := strings.ToLower(strings.TrimSpace(in.Username)), clientIP(r)
	window := time.Now().UTC().Add(-15 * time.Minute).Format(time.RFC3339Nano)
	var failures int
	_ = s.store.DB.QueryRow("SELECT COUNT(*) FROM login_attempts WHERE username=? AND ip=? AND succeeded=0 AND created_at>=?", username, ip, window).Scan(&failures)
	if failures >= 5 {
		w.Header().Set("Retry-After", "900")
		writeError(w, &domainError{429, "LOGIN_RATE_LIMITED", "Too many failed sign-in attempts", nil})
		return
	}
	var id, name, hash, role, status string
	err := s.store.DB.QueryRow("SELECT id,display_name,password_hash,system_role,status FROM users WHERE username=?", username).Scan(&id, &name, &hash, &role, &status)
	if err != nil || status != "active" || !security.VerifyPassword(hash, in.Password) {
		_, _ = s.store.DB.Exec("INSERT INTO login_attempts(id,username,ip,succeeded,created_at) VALUES(?,?,?,0,?)", security.Token(18), username, ip, now())
		writeError(w, &domainError{401, "INVALID_CREDENTIALS", "Invalid username or password", nil})
		return
	}
	_, _ = s.store.DB.Exec("DELETE FROM login_attempts WHERE username=? AND ip=?", username, ip)
	token, csrf := security.Token(32), security.Token(24)
	sessionID := security.Token(18)
	stamp := now()
	expires := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339Nano)
	_, err = s.store.DB.Exec("INSERT INTO sessions(id,user_id,token_hash,csrf_hash,user_agent,ip,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?)", sessionID, id, security.HashOpaque(token), security.HashOpaque(csrf), r.UserAgent(), clientIP(r), expires, stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	secure := s.config.Production
	http.SetCookie(w, &http.Cookie{Name: "pb_session", Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 86400})
	http.SetCookie(w, &http.Cookie{Name: "pb_csrf", Value: csrf, Path: "/", Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 86400})
	writeJSON(w, 200, map[string]any{"user": map[string]any{"id": id, "username": username, "displayName": name, "systemRole": role, "status": status}, "csrfToken": csrf})
}

func (s *Server) authenticate(r *http.Request) (actor, error) {
	cookie, err := r.Cookie("pb_session")
	if err != nil {
		return actor{}, &domainError{401, "AUTHENTICATION_REQUIRED", "Sign in required", nil}
	}
	var a actor
	var expires string
	err = s.store.DB.QueryRow(`SELECT u.id,u.username,u.display_name,u.system_role,se.id,se.expires_at FROM sessions se JOIN users u ON u.id=se.user_id WHERE se.token_hash=? AND se.revoked_at IS NULL AND u.status='active'`, security.HashOpaque(cookie.Value)).Scan(&a.ID, &a.Name, &a.Name, &a.Role, &a.SessionID, &expires)
	if err != nil || expires <= now() {
		return actor{}, &domainError{401, "AUTHENTICATION_REQUIRED", "Session expired", nil}
	}
	a.Type = "human"
	if r.Method != "GET" && r.Method != "HEAD" {
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, parseErr := url.Parse(origin)
			if parseErr != nil || !strings.EqualFold(parsed.Host, r.Host) {
				return actor{}, &domainError{403, "ORIGIN_FAILED", "Origin validation failed", nil}
			}
		}
		csrfCookie, e := r.Cookie("pb_csrf")
		provided := r.Header.Get("X-CSRF-Token")
		if e != nil || provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(csrfCookie.Value)) != 1 {
			return actor{}, &domainError{403, "CSRF_FAILED", "CSRF validation failed", nil}
		}
		var hash string
		_ = s.store.DB.QueryRow("SELECT csrf_hash FROM sessions WHERE id=?", a.SessionID).Scan(&hash)
		if hash != security.HashOpaque(provided) {
			return actor{}, &domainError{403, "CSRF_FAILED", "CSRF validation failed", nil}
		}
	}
	return a, nil
}
func (s *Server) human(w http.ResponseWriter, r *http.Request) (actor, bool) {
	a, err := s.authenticate(r)
	if err != nil {
		writeError(w, err)
		return actor{}, false
	}
	return a, true
}
func requireAdmin(a actor) error {
	if a.Role != "administrator" {
		return &domainError{403, "ADMIN_REQUIRED", "Administrator required", nil}
	}
	return nil
}
func (s *Server) requireDeveloper(projectID string, a actor) error {
	if a.Role == "administrator" {
		return nil
	}
	var role string
	err := s.store.DB.QueryRow("SELECT role FROM project_memberships WHERE project_id=? AND user_id=?", projectID, a.ID).Scan(&role)
	if err != nil || role != "developer" {
		return &domainError{403, "DEVELOPER_REQUIRED", "Project developer required", nil}
	}
	return nil
}
func (s *Server) canView(projectID string, a actor) bool {
	if a.Role == "administrator" {
		return true
	}
	var one int
	return s.store.DB.QueryRow("SELECT 1 FROM project_memberships WHERE project_id=? AND user_id=?", projectID, a.ID).Scan(&one) == nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	_, _ = s.store.DB.Exec("UPDATE sessions SET revoked_at=? WHERE id=?", now(), a.SessionID)
	http.SetCookie(w, &http.Cookie{Name: "pb_session", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "pb_csrf", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	var username, name, status string
	var forced int
	_ = s.store.DB.QueryRow("SELECT username,display_name,status,must_change_password FROM users WHERE id=?", a.ID).Scan(&username, &name, &status, &forced)
	writeJSON(w, 200, map[string]any{"id": a.ID, "username": username, "displayName": name, "systemRole": a.Role, "status": status, "mustChangePassword": forced != 0})
}
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	var in struct {
		DisplayName string `json:"displayName"`
	}
	decode(r, &in)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		writeError(w, &domainError{422, "VALIDATION_ERROR", "Display name required", nil})
		return
	}
	_, _ = s.store.DB.Exec("UPDATE users SET display_name=?,updated_at=? WHERE id=?", in.DisplayName, now(), a.ID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	decode(r, &in)
	if !security.ValidPassword(in.NewPassword) {
		writeError(w, &domainError{422, "WEAK_PASSWORD", "Password must be 12-256 characters with letters and numbers", nil})
		return
	}
	var hash string
	_ = s.store.DB.QueryRow("SELECT password_hash FROM users WHERE id=?", a.ID).Scan(&hash)
	if !security.VerifyPassword(hash, in.CurrentPassword) {
		writeError(w, &domainError{401, "INVALID_CREDENTIALS", "Current password invalid", nil})
		return
	}
	stamp := now()
	_ = s.store.Write(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE users SET password_hash=?,must_change_password=0,updated_at=? WHERE id=?", security.HashPassword(in.NewPassword), stamp, a.ID); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE sessions SET revoked_at=? WHERE user_id=? AND id<>? AND revoked_at IS NULL", stamp, a.ID, a.SessionID)
		return err
	})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	writeRows(w, s.store.DB, "SELECT id,user_agent,ip,expires_at,created_at,revoked_at FROM sessions WHERE user_id=? ORDER BY created_at DESC", a.ID)
}
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	result, _ := s.store.DB.Exec("UPDATE sessions SET revoked_at=? WHERE id=? AND user_id=?", now(), r.PathValue("id"), a.ID)
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, &domainError{404, "NOT_FOUND", "Session not found", nil})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	query := `SELECT p.id,p.project_key,p.name,p.description_markdown,p.archived_at,p.project_path,p.default_target_branch,p.validation_commands_json,p.forbidden_paths_json,p.agent_rules_markdown,p.allow_subtasks,p.agent_prompts_json,p.knowledge_compaction_days,p.knowledge_compaction_request_count,p.config_version FROM projects p`
	args := []any{}
	if a.Role != "administrator" {
		query += " JOIN project_memberships m ON m.project_id=p.id WHERE m.user_id=?"
		args = append(args, a.ID)
	}
	query += " ORDER BY p.name"
	rows, err := s.store.DB.Query(query, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		out = append(out, scanProject(rows))
	}
	writeJSON(w, 200, out)
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	var in struct {
		Key, Name   string
		ProjectPath string `json:"projectPath"`
	}
	decode(r, &in)
	key := strings.ToLower(strings.TrimSpace(in.Key))
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{1,19}$`).MatchString(key) || strings.TrimSpace(in.Name) == "" {
		writeError(w, &domainError{422, "INVALID_PROJECT", "Valid name and 2-20 character key required", nil})
		return
	}
	repository, err := projectrepo.Prepare(r.Context(), in.ProjectPath)
	if err != nil {
		writeError(w, &domainError{422, "INVALID_PROJECT_PATH", err.Error(), nil})
		return
	}
	id := security.Token(18)
	stamp := now()
	err = s.store.Write(r.Context(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO projects(id,project_key,name,project_path,default_target_branch,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, id, key, in.Name, repository.Path, repository.DefaultBranch, stamp, stamp)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO project_memberships(project_id,user_id,role,created_at) VALUES(?,?,?,?)", id, a.ID, "developer", stamp)
		return err
	})
	if err != nil {
		writeError(w, err)
		return
	}
	project, _ := s.project(id)
	writeJSON(w, 201, project)
}
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.requireDeveloper(id, a); err != nil {
		writeError(w, err)
		return
	}
	var in map[string]any
	decode(r, &in)
	current, _ := s.project(id)
	name := stringValue(in, "name", current["name"])
	branch := stringValue(in, "defaultTargetBranch", current["defaultTargetBranch"])
	if exists, branchErr := projectrepo.BranchExists(r.Context(), current["projectPath"].(string), branch); branchErr != nil || !exists {
		writeError(w, &domainError{422, "TARGET_BRANCH_NOT_FOUND", "Default target branch does not exist in the local repository", nil})
		return
	}
	prompts := valueOr(in, "agentPrompts", current["agentPrompts"])
	promptList, validPrompts := prompts.([]any)
	if !validPrompts {
		if strings, ok := prompts.([]string); ok {
			promptList = make([]any, len(strings))
			for index, prompt := range strings {
				promptList[index] = prompt
			}
			validPrompts = true
		}
	}
	if !validPrompts {
		writeError(w, &domainError{422, "INVALID_AGENT_PROMPTS", "Agent prompts must be a list of text entries", nil})
		return
	}
	cleanPrompts := make([]string, 0, len(promptList))
	for _, value := range promptList {
		prompt, ok := value.(string)
		prompt = strings.TrimSpace(prompt)
		if !ok || prompt == "" || len(prompt) > 2000 {
			writeError(w, &domainError{422, "INVALID_AGENT_PROMPT", "Each Agent prompt must contain 1-2000 characters", nil})
			return
		}
		cleanPrompts = append(cleanPrompts, prompt)
	}
	promptsRaw, _ := json.Marshal(cleanPrompts)
	compactionDays := intValue(in, "knowledgeCompactionDays", current["knowledgeCompactionDays"])
	compactionCount := intValue(in, "knowledgeCompactionRequestCount", current["knowledgeCompactionRequestCount"])
	if compactionDays < 0 || compactionCount < 0 {
		writeError(w, &domainError{422, "INVALID_COMPACTION_POLICY", "Knowledge compaction thresholds cannot be negative", nil})
		return
	}
	_, err := s.store.DB.Exec("UPDATE projects SET name=?,default_target_branch=?,allow_subtasks=?,agent_prompts_json=?,knowledge_compaction_days=?,knowledge_compaction_request_count=?,config_version=config_version+1,updated_at=? WHERE id=?", name, branch, boolInt(boolValue(in, "allowSubtasks", current["allowSubtasks"])), string(promptsRaw), compactionDays, compactionCount, now(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	project, _ := s.project(id)
	writeJSON(w, 200, project)
}
func (s *Server) project(id string) (map[string]any, error) {
	rows, err := s.store.DB.Query(`SELECT id,project_key,name,description_markdown,archived_at,project_path,default_target_branch,validation_commands_json,forbidden_paths_json,agent_rules_markdown,allow_subtasks,agent_prompts_json,knowledge_compaction_days,knowledge_compaction_request_count,config_version FROM projects WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanProject(rows), nil
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.store.DB.Query(`SELECT u.id,u.username,u.display_name,u.system_role,u.status,u.last_active_at,(SELECT COUNT(*) FROM project_memberships m WHERE m.user_id=u.id) FROM users u WHERE u.status<>'deleted' ORDER BY u.display_name`)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, username, name, role, status string
		var last *string
		var count int
		_ = rows.Scan(&id, &username, &name, &role, &status, &last, &count)
		out = append(out, map[string]any{"id": id, "username": username, "displayName": name, "systemRole": role, "status": status, "lastActiveAt": last, "membershipCount": count})
	}
	writeJSON(w, 200, out)
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		SystemRole  string `json:"systemRole"`
	}
	decode(r, &in)
	if !regexp.MustCompile(`^[a-z0-9._-]{2,64}$`).MatchString(in.Username) || strings.TrimSpace(in.DisplayName) == "" {
		writeError(w, &domainError{422, "INVALID_USER", "Valid username and display name required", nil})
		return
	}
	if in.SystemRole == "" {
		in.SystemRole = "user"
	}
	password := security.Token(18) + "-Aa1"
	id, stamp := security.Token(18), now()
	_, err := s.store.DB.Exec("INSERT INTO users(id,username,display_name,password_hash,system_role,status,must_change_password,created_at,updated_at) VALUES(?,?,?,?,?,'active',0,?,?)", id, in.Username, in.DisplayName, security.HashPassword(password), in.SystemRole, stamp, stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "account.created", "user", id, "", map[string]any{"username": in.Username, "systemRole": in.SystemRole})
	writeJSON(w, 201, map[string]any{"id": id, "username": in.Username, "displayName": in.DisplayName, "temporaryPassword": password})
}
func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	var username, name, role, status string
	var last *string
	if err := s.store.DB.QueryRow("SELECT username,display_name,system_role,status,last_active_at FROM users WHERE id=?", id).Scan(&username, &name, &role, &status, &last); err != nil {
		writeError(w, err)
		return
	}
	members := rowsAsMaps(s.store.DB, "SELECT m.project_id projectId,p.name,m.role FROM project_memberships m JOIN projects p ON p.id=m.project_id WHERE m.user_id=?", id)
	writeJSON(w, 200, map[string]any{"id": id, "username": username, "displayName": name, "systemRole": role, "status": status, "lastActiveAt": last, "memberships": members})
}
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		DisplayName string `json:"displayName"`
		SystemRole  string `json:"systemRole"`
	}
	decode(r, &in)
	id := r.PathValue("id")
	_, err := s.store.DB.Exec("UPDATE users SET display_name=CASE WHEN ?='' THEN display_name ELSE ? END,system_role=CASE WHEN ?='' THEN system_role ELSE ? END,updated_at=? WHERE id=?", in.DisplayName, in.DisplayName, in.SystemRole, in.SystemRole, now(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "account.updated", "user", id, "", map[string]any{})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	if id == a.ID {
		writeError(w, &domainError{422, "SELF_DELETE_FORBIDDEN", "Cannot delete yourself", nil})
		return
	}
	var role, status string
	if err := s.store.DB.QueryRow("SELECT system_role,status FROM users WHERE id=?", id).Scan(&role, &status); err != nil {
		writeError(w, err)
		return
	}
	if status == "deleted" {
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if role == "administrator" && status == "active" {
		var count int
		_ = s.store.DB.QueryRow("SELECT COUNT(*) FROM users WHERE system_role='administrator' AND status='active'").Scan(&count)
		if count <= 1 {
			writeError(w, &domainError{422, "LAST_ADMIN", "Cannot delete final administrator", nil})
			return
		}
	}
	stamp := now()
	if err := s.store.Write(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE users SET status='deleted',disabled_reason='deleted',updated_at=? WHERE id=?", stamp, id); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", stamp, id)
		return err
	}); err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "account.deleted", "user", id, "", map[string]any{"logical": true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) disableUser(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	if id == a.ID {
		writeError(w, &domainError{422, "SELF_DISABLE_FORBIDDEN", "Cannot disable yourself", nil})
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	decode(r, &in)
	if strings.TrimSpace(in.Reason) == "" {
		writeError(w, &domainError{422, "REASON_REQUIRED", "Reason required", nil})
		return
	}
	var role string
	if err := s.store.DB.QueryRow("SELECT system_role FROM users WHERE id=?", id).Scan(&role); err != nil {
		writeError(w, err)
		return
	}
	if role == "administrator" {
		var count int
		_ = s.store.DB.QueryRow("SELECT COUNT(*) FROM users WHERE system_role='administrator' AND status='active'").Scan(&count)
		if count <= 1 {
			writeError(w, &domainError{422, "LAST_ADMIN", "Cannot disable final administrator", nil})
			return
		}
	}
	stamp := now()
	_ = s.store.Write(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE users SET status='disabled',disabled_reason=?,updated_at=? WHERE id=?", in.Reason, stamp, id); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", stamp, id)
		return err
	})
	s.audit(a, "account.disabled", "user", id, "", map[string]any{"reason": in.Reason})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) enableUser(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	result, err := s.store.DB.Exec("UPDATE users SET status='active',disabled_reason=NULL,updated_at=? WHERE id=? AND status<>'deleted'", now(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		writeError(w, sql.ErrNoRows)
		return
	}
	s.audit(a, "account.enabled", "user", id, "", map[string]any{})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	password := security.Token(18) + "-Aa1"
	stamp := now()
	result, err := s.store.DB.Exec("UPDATE users SET password_hash=?,must_change_password=0,updated_at=? WHERE id=?", security.HashPassword(password), stamp, id)
	if err != nil {
		writeError(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, sql.ErrNoRows)
		return
	}
	_, _ = s.store.DB.Exec("UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", stamp, id)
	s.audit(a, "account.password_reset", "user", id, "", map[string]any{})
	writeJSON(w, 200, map[string]any{"temporaryPassword": password})
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if !s.canView(projectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	rows, err := s.store.DB.Query(`SELECT u.id,u.username,u.display_name,u.system_role,u.status,m.role FROM users u LEFT JOIN project_memberships m ON m.user_id=u.id AND m.project_id=? WHERE u.status<>'deleted' ORDER BY u.display_name`, projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, username, name, systemRole, status string
		var role *string
		_ = rows.Scan(&id, &username, &name, &systemRole, &status, &role)
		out = append(out, map[string]any{"id": id, "username": username, "displayName": name, "systemRole": systemRole, "status": status, "role": role})
	}
	writeJSON(w, 200, out)
}
func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if err := s.requireDeveloper(projectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		UserID string `json:"userId"`
		Role   string `json:"role"`
	}
	decode(r, &in)
	if in.Role != "developer" && in.Role != "viewer" {
		writeError(w, &domainError{422, "INVALID_ROLE", "Role must be developer or viewer", nil})
		return
	}
	_, err := s.store.DB.Exec("INSERT INTO project_memberships(project_id,user_id,role,created_at) VALUES(?,?,?,?)", projectID, in.UserID, in.Role, now())
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "project.member_added", "user", in.UserID, projectID, map[string]any{"role": in.Role})
	writeJSON(w, 201, map[string]bool{"ok": true})
}
func (s *Server) updateMember(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if err := s.requireDeveloper(projectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	decode(r, &in)
	_, err := s.store.DB.Exec("UPDATE project_memberships SET role=? WHERE project_id=? AND user_id=?", in.Role, projectID, r.PathValue("userId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if err := s.requireDeveloper(projectID, a); err != nil {
		writeError(w, err)
		return
	}
	_, err := s.store.DB.Exec("DELETE FROM project_memberships WHERE project_id=? AND user_id=?", projectID, r.PathValue("userId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		Name, Purpose      string
		RuntimeType        string   `json:"runtimeType"`
		Model              string   `json:"model"`
		ReasoningEffort    string   `json:"reasoningEffort"`
		MaxConcurrentTasks int      `json:"maxConcurrentTasks"`
		TurnTimeoutMinutes int      `json:"turnTimeoutMinutes"`
		AcceptTags         []string `json:"acceptTags"`
		RejectTags         []string `json:"rejectTags"`
	}
	decode(r, &in)
	if in.RuntimeType == "" {
		in.RuntimeType = "local_codex_cli"
	}
	if in.MaxConcurrentTasks == 0 {
		in.MaxConcurrentTasks = 1
	}
	if in.TurnTimeoutMinutes == 0 {
		in.TurnTimeoutMinutes = 120
	}
	if in.RuntimeType != "local_codex_cli" || in.MaxConcurrentTasks <= 0 || in.TurnTimeoutMinutes <= 0 {
		writeError(w, &domainError{422, "INVALID_AGENT_CONFIGURATION", "Local Codex CLI, a positive capacity, and a positive timeout are required", nil})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Purpose = strings.TrimSpace(in.Purpose)
	var err error
	in.Model, in.ReasoningEffort, err = normalizeAgentExecutionConfig(in.Model, in.ReasoningEffort)
	if err != nil {
		writeError(w, err)
		return
	}
	if in.Name == "" {
		writeError(w, &domainError{422, "INVALID_AGENT_CONFIGURATION", "Agent name is required", nil})
		return
	}
	acceptTags, err := normalizeAgentTags(in.AcceptTags)
	if err != nil {
		writeError(w, err)
		return
	}
	rejectTags, err := normalizeAgentTags(in.RejectTags)
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err := s.config.LookPath("codex"); err != nil {
		writeError(w, &domainError{422, "CODEX_CLI_NOT_FOUND", "codex is not installed or not available on PATH", nil})
		return
	}
	id, stamp := security.Token(18), now()
	acceptRaw, _ := json.Marshal(acceptTags)
	rejectRaw, _ := json.Marshal(rejectTags)
	_, err = s.store.DB.Exec("INSERT INTO agents(id,name,purpose,runtime_type,model,reasoning_effort,max_concurrent_tasks,turn_timeout_minutes,accept_tags_json,reject_tags_json,status,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,'active',?)", id, in.Name, in.Purpose, in.RuntimeType, in.Model, in.ReasoningEffort, in.MaxConcurrentTasks, in.TurnTimeoutMinutes, string(acceptRaw), string(rejectRaw), stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "agent.created", "agent", id, "", map[string]any{"name": in.Name})
	writeJSON(w, 201, map[string]any{"id": id, "name": in.Name, "purpose": in.Purpose, "runtimeType": in.RuntimeType, "model": in.Model, "reasoningEffort": in.ReasoningEffort, "maxConcurrentTasks": in.MaxConcurrentTasks, "turnTimeoutMinutes": in.TurnTimeoutMinutes, "acceptTags": acceptTags, "rejectTags": rejectTags, "enabled": true, "requestCount": 0, "inputTokens": 0, "cachedInputTokens": 0, "outputTokens": 0, "reasoningTokens": 0, "totalTokens": 0})
}
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	_, ok := s.human(w, r)
	if !ok {
		return
	}
	rows, err := s.store.DB.Query(`SELECT a.id,a.name,a.purpose,a.status,a.runtime_type,a.model,a.reasoning_effort,a.max_concurrent_tasks,a.turn_timeout_minutes,a.accept_tags_json,a.reject_tags_json,
		(SELECT COUNT(*) FROM agent_requests r WHERE r.assigned_agent_id=a.id AND r.status='running'),
		(SELECT COUNT(*) FROM agent_requests r WHERE r.assigned_agent_id=a.id),
		(SELECT COALESCE(SUM(input_tokens),0) FROM agent_requests r WHERE r.assigned_agent_id=a.id),
		(SELECT COALESCE(SUM(cached_input_tokens),0) FROM agent_requests r WHERE r.assigned_agent_id=a.id),
		(SELECT COALESCE(SUM(output_tokens),0) FROM agent_requests r WHERE r.assigned_agent_id=a.id),
		(SELECT COALESCE(SUM(reasoning_tokens),0) FROM agent_requests r WHERE r.assigned_agent_id=a.id),
		(SELECT COALESCE(SUM(total_tokens),0) FROM agent_requests r WHERE r.assigned_agent_id=a.id)
		FROM agents a WHERE a.status<>'deleted' AND a.revoked_at IS NULL ORDER BY a.name`)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, purpose, status, acceptRaw, rejectRaw string
		var runtimeType, model, reasoningEffort string
		var capacity, timeout, running, requestCount int
		var inputTokens, cachedInputTokens, outputTokens, reasoningTokens, totalTokens int64
		if err = rows.Scan(&id, &name, &purpose, &status, &runtimeType, &model, &reasoningEffort, &capacity, &timeout, &acceptRaw, &rejectRaw, &running, &requestCount, &inputTokens, &cachedInputTokens, &outputTokens, &reasoningTokens, &totalTokens); err != nil {
			writeError(w, err)
			return
		}
		acceptTags, rejectTags := []string{}, []string{}
		_ = json.Unmarshal([]byte(acceptRaw), &acceptTags)
		_ = json.Unmarshal([]byte(rejectRaw), &rejectTags)
		out = append(out, map[string]any{"id": id, "name": name, "purpose": purpose, "status": status, "enabled": status == "active", "runtimeType": runtimeType, "model": model, "reasoningEffort": reasoningEffort, "maxConcurrentTasks": capacity, "turnTimeoutMinutes": timeout, "acceptTags": acceptTags, "rejectTags": rejectTags, "currentLoad": running, "requestCount": requestCount, "inputTokens": inputTokens, "cachedInputTokens": cachedInputTokens, "outputTokens": outputTokens, "reasoningTokens": reasoningTokens, "totalTokens": totalTokens})
	}
	writeJSON(w, 200, out)
}

func (s *Server) currentAgentExecutions(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.store.DB.QueryContext(r.Context(), `SELECT r.id,COALESCE(r.source_work_item_id,''),r.title,r.status,r.started_at,substr(r.output_jsonl,-200000),length(r.output_jsonl)>200000
		FROM agent_requests r WHERE r.assigned_agent_id=? AND r.status='running' ORDER BY r.started_at,r.id`, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, itemID, title, state, startedAt, output string
		var truncated bool
		if err = rows.Scan(&id, &itemID, &title, &state, &startedAt, &output, &truncated); err != nil {
			writeError(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "workItemId": itemID, "title": title, "state": state, "startedAt": startedAt, "output": output, "truncated": truncated})
	}
	writeJSON(w, 200, out)
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		Name               string   `json:"name"`
		Purpose            string   `json:"purpose"`
		Model              *string  `json:"model"`
		ReasoningEffort    *string  `json:"reasoningEffort"`
		MaxConcurrentTasks int      `json:"maxConcurrentTasks"`
		TurnTimeoutMinutes int      `json:"turnTimeoutMinutes"`
		AcceptTags         []string `json:"acceptTags"`
		RejectTags         []string `json:"rejectTags"`
	}
	decode(r, &in)
	var name, purpose, model, reasoningEffort string
	var capacity, timeout int
	var err error
	if err := s.store.DB.QueryRow("SELECT name,purpose,model,reasoning_effort,max_concurrent_tasks,turn_timeout_minutes FROM agents WHERE id=? AND status<>'deleted' AND revoked_at IS NULL", r.PathValue("id")).Scan(&name, &purpose, &model, &reasoningEffort, &capacity, &timeout); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(in.Name) != "" {
		name = strings.TrimSpace(in.Name)
	}
	if in.Purpose != "" {
		purpose = strings.TrimSpace(in.Purpose)
	}
	if in.MaxConcurrentTasks > 0 {
		capacity = in.MaxConcurrentTasks
	}
	if in.TurnTimeoutMinutes > 0 {
		timeout = in.TurnTimeoutMinutes
	}
	if in.Model != nil {
		model = *in.Model
	}
	if in.ReasoningEffort != nil {
		reasoningEffort = *in.ReasoningEffort
	}
	model, reasoningEffort, err = normalizeAgentExecutionConfig(model, reasoningEffort)
	if err != nil {
		writeError(w, err)
		return
	}
	acceptTags, err := normalizeAgentTags(in.AcceptTags)
	if err != nil {
		writeError(w, err)
		return
	}
	rejectTags, err := normalizeAgentTags(in.RejectTags)
	if err != nil {
		writeError(w, err)
		return
	}
	acceptRaw, _ := json.Marshal(acceptTags)
	rejectRaw, _ := json.Marshal(rejectTags)
	if _, err = s.store.DB.Exec("UPDATE agents SET name=?,purpose=?,model=?,reasoning_effort=?,max_concurrent_tasks=?,turn_timeout_minutes=?,accept_tags_json=?,reject_tags_json=? WHERE id=?", name, purpose, model, reasoningEffort, capacity, timeout, string(acceptRaw), string(rejectRaw), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	s.exec.Wake()
	writeJSON(w, 200, map[string]any{"id": r.PathValue("id"), "name": name, "purpose": purpose, "runtimeType": "local_codex_cli", "model": model, "reasoningEffort": reasoningEffort, "maxConcurrentTasks": capacity, "turnTimeoutMinutes": timeout, "acceptTags": acceptTags, "rejectTags": rejectTags, "enabled": true})
}

func normalizeAgentExecutionConfig(model, effort string) (string, string, error) {
	model = strings.TrimSpace(model)
	effort = strings.ToLower(strings.TrimSpace(effort))
	if len(model) > 100 || strings.ContainsAny(model, " \t\r\n") {
		return "", "", &domainError{422, "INVALID_MODEL", "Codex model must be a single identifier of at most 100 characters", nil}
	}
	valid := map[string]bool{"": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true}
	if !valid[effort] {
		return "", "", &domainError{422, "INVALID_REASONING_EFFORT", "Reasoning effort must be inherited, minimal, low, medium, high, or xhigh", nil}
	}
	return model, effort, nil
}

func normalizeAgentTags(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, &domainError{422, "TOO_MANY_TAGS", "An Agent tag rule can contain at most 20 tags", nil}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > 30 {
			return nil, &domainError{422, "TAG_TOO_LONG", "Agent tags can contain at most 30 characters", nil}
		}
		key := strings.ToLower(tag)
		if !seen[key] {
			seen[key] = true
			out = append(out, tag)
		}
	}
	return out, nil
}
func (s *Server) disableOrganizationAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	agentID := r.PathValue("id")
	result, err := s.store.DB.Exec("UPDATE agents SET status='disabled' WHERE id=? AND status='active' AND revoked_at IS NULL", agentID)
	if err != nil {
		writeError(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		var status string
		if scanErr := s.store.DB.QueryRow("SELECT status FROM agents WHERE id=? AND revoked_at IS NULL", agentID).Scan(&status); scanErr != nil || status == "deleted" {
			writeError(w, sql.ErrNoRows)
			return
		}
	}
	if err = s.exec.DeactivateAgent(r.Context(), agentID); err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "agent.disabled", "agent", agentID, "", map[string]any{})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) enableOrganizationAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	result, err := s.store.DB.Exec("UPDATE agents SET status='active' WHERE id=? AND status='disabled' AND revoked_at IS NULL", r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		var status string
		if scanErr := s.store.DB.QueryRow("SELECT status FROM agents WHERE id=? AND revoked_at IS NULL", r.PathValue("id")).Scan(&status); scanErr != nil || status == "deleted" {
			writeError(w, sql.ErrNoRows)
			return
		}
	}
	s.exec.Wake()
	s.audit(a, "agent.enabled", "agent", r.PathValue("id"), "", map[string]any{})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	agentID := r.PathValue("id")
	var status string
	var revokedAt *string
	if err := s.store.DB.QueryRow("SELECT status,revoked_at FROM agents WHERE id=?", agentID).Scan(&status, &revokedAt); err != nil {
		writeError(w, err)
		return
	}
	if status == "deleted" || revokedAt != nil {
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	stamp := now()
	if err := s.exec.DeactivateAgent(r.Context(), agentID); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.Write(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE agents SET status='deleted',revoked_at=? WHERE id=?", stamp, agentID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "agent.deleted", "agent", agentID, "", map[string]any{"logical": true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) createWorkItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	var in struct {
		RequestID             string   `json:"requestId"`
		ProjectID             string   `json:"projectId"`
		Title                 string   `json:"title"`
		Description           string   `json:"descriptionMarkdown"`
		Criteria              string   `json:"acceptanceCriteriaMarkdown"`
		Priority              string   `json:"priority"`
		WorkflowType          string   `json:"workflowType"`
		TargetBranch          string   `json:"targetBranch"`
		AssigneeKind          string   `json:"assigneeKind"`
		AssigneeID            string   `json:"assigneeId"`
		ParentID              string   `json:"parentId"`
		FollowerIDs           []string `json:"followerIds"`
		Tags                  []string `json:"tags"`
		IsAgentTask           bool     `json:"isAgentTask"`
		PauseAfterPlan        bool     `json:"pauseAfterPlan"`
		PauseBeforeCompletion bool     `json:"pauseBeforeCompletion"`
	}
	decode(r, &in)
	if err := s.requireDeveloper(in.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	if err := s.validateFollowers(in.ProjectID, in.FollowerIDs); err != nil {
		writeError(w, err)
		return
	}
	if in.WorkflowType == "" {
		in.WorkflowType = "standard"
	}
	var projectPath, defaultBranch string
	if err := s.store.DB.QueryRow("SELECT project_path,default_target_branch FROM projects WHERE id=?", in.ProjectID).Scan(&projectPath, &defaultBranch); err != nil {
		writeError(w, err)
		return
	}
	if in.WorkflowType == "simple_conversation" || strings.TrimSpace(in.TargetBranch) == "" {
		in.TargetBranch = defaultBranch
	}
	if exists, branchErr := projectrepo.BranchExists(r.Context(), projectPath, in.TargetBranch); branchErr != nil || !exists {
		writeError(w, &domainError{422, "TARGET_BRANCH_NOT_FOUND", "Target branch does not exist in the local repository", nil})
		return
	}
	if !in.IsAgentTask && in.AssigneeID != "" {
		var exists int
		if in.AssigneeKind != "human" || s.store.DB.QueryRow("SELECT 1 FROM project_memberships WHERE project_id=? AND user_id=?", in.ProjectID, in.AssigneeID).Scan(&exists) != nil {
			writeError(w, &domainError{422, "INVALID_ASSIGNEE", "Assignee is not enabled for this project", nil})
			return
		}
	}
	item, err := s.queue.Create(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, workqueue.CreateInput{RequestID: in.RequestID, ProjectID: in.ProjectID, Title: in.Title, DescriptionMarkdown: in.Description, AcceptanceCriteriaMarkdown: in.Criteria, Priority: in.Priority, WorkflowType: in.WorkflowType, TargetBranch: in.TargetBranch, AssigneeKind: in.AssigneeKind, AssigneeID: in.AssigneeID, ParentID: in.ParentID, CreatedByUserID: a.ID, FollowerIDs: in.FollowerIDs, Tags: in.Tags, IsAgentTask: in.IsAgentTask, PauseAfterPlan: in.PauseAfterPlan, PauseBeforeCompletion: in.PauseBeforeCompletion})
	if err != nil {
		writeError(w, err)
		return
	}
	if in.AssigneeKind == "human" && in.AssigneeID != "" {
		s.notifyUsers(item, a, "assigned", "你被指派了任务", item.Title, []string{in.AssigneeID})
	}
	s.exec.Wake()
	writeJSON(w, 201, item)
}
func (s *Server) updateWorkItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ExpectedVersion       int64    `json:"expectedVersion"`
		Title                 string   `json:"title"`
		Description           string   `json:"descriptionMarkdown"`
		Criteria              string   `json:"acceptanceCriteriaMarkdown"`
		Priority              string   `json:"priority"`
		TargetBranch          string   `json:"targetBranch"`
		AssigneeKind          string   `json:"assigneeKind"`
		AssigneeID            string   `json:"assigneeId"`
		FollowerIDs           []string `json:"followerIds"`
		Tags                  []string `json:"tags"`
		Configure             bool     `json:"configure"`
		IsAgentTask           bool     `json:"isAgentTask"`
		PauseAfterPlan        bool     `json:"pauseAfterPlan"`
		PauseBeforeCompletion bool     `json:"pauseBeforeCompletion"`
	}
	decode(r, &in)
	if in.Configure {
		if err = s.validateFollowers(item.ProjectID, in.FollowerIDs); err != nil {
			writeError(w, err)
			return
		}
		if !in.IsAgentTask && in.AssigneeID != "" {
			var exists int
			if in.AssigneeKind == "human" {
				err = s.store.DB.QueryRow("SELECT 1 FROM project_memberships WHERE project_id=? AND user_id=?", item.ProjectID, in.AssigneeID).Scan(&exists)
			} else {
				err = sql.ErrNoRows
			}
			if err != nil {
				writeError(w, &domainError{422, "INVALID_ASSIGNEE", "Assignee is not enabled for this project", nil})
				return
			}
		}
	}
	item, err = s.queue.Update(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, workqueue.UpdateInput{ExpectedVersion: in.ExpectedVersion, Title: in.Title, Description: in.Description, Criteria: in.Criteria, Priority: in.Priority, TargetBranch: in.TargetBranch, AssigneeKind: in.AssigneeKind, AssigneeID: in.AssigneeID, FollowerIDs: in.FollowerIDs, Tags: in.Tags, Configure: in.Configure, IsAgentTask: in.IsAgentTask, PauseAfterPlan: in.PauseAfterPlan, PauseBeforeCompletion: in.PauseBeforeCompletion})
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "work_item_updated", "任务已更新", item.Title, nil)
	s.exec.Wake()
	writeJSON(w, 200, item)
}

func (s *Server) moveWorkItemStage(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		TargetStage     string `json:"targetStage"`
		Note            string `json:"noteMarkdown"`
	}
	decode(r, &in)
	item, err = s.queue.MoveStage(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, in.ExpectedVersion, in.TargetStage, in.Note)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "stage_changed", "任务阶段已更新", item.Title, nil)
	writeJSON(w, 200, item)
}
func (s *Server) assignWorkItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		AssigneeKind    string `json:"assigneeKind"`
		AssigneeID      string `json:"assigneeId"`
		Reason          string `json:"reason"`
	}
	decode(r, &in)
	var exists int
	if in.AssigneeKind == "human" {
		err = s.store.DB.QueryRow("SELECT 1 FROM project_memberships WHERE project_id=? AND user_id=?", item.ProjectID, in.AssigneeID).Scan(&exists)
	} else {
		err = s.store.DB.QueryRow("SELECT 1 FROM agents WHERE id=? AND status='active' AND revoked_at IS NULL", in.AssigneeID).Scan(&exists)
	}
	if err != nil {
		writeError(w, &domainError{422, "INVALID_ASSIGNEE", "Assignee is not available", nil})
		return
	}
	item, err = s.queue.Assign(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, in.ExpectedVersion, in.AssigneeKind, in.AssigneeID, in.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "assigned", "任务负责人已更新", item.Title, []string{in.AssigneeID})
	writeJSON(w, 201, item)
}
func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !s.canView(item.ProjectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	var in struct {
		Markdown        string   `json:"markdown"`
		ExpectedVersion int64    `json:"expectedVersion"`
		AttachmentIDs   []string `json:"attachmentIds"`
		ResumeAgent     bool     `json:"resumeAgent"`
	}
	decode(r, &in)
	if strings.TrimSpace(in.Markdown) == "" && len(in.AttachmentIDs) == 0 {
		writeError(w, &domainError{422, "VALIDATION_ERROR", "Message or attachment is required", nil})
		return
	}
	if len(in.AttachmentIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(in.AttachmentIDs)), ",")
		args := []any{item.ID}
		for _, attachmentID := range in.AttachmentIDs {
			args = append(args, attachmentID)
		}
		var count int
		if err = s.store.DB.QueryRow("SELECT COUNT(*) FROM attachments WHERE work_item_id=? AND entry_id IS NULL AND id IN ("+placeholders+")", args...).Scan(&count); err != nil || count != len(in.AttachmentIDs) {
			writeError(w, &domainError{422, "INVALID_ATTACHMENT", "Every attachment must belong to this task and be unattached", nil})
			return
		}
	}
	if strings.TrimSpace(in.Markdown) == "" {
		in.Markdown = "附件"
	}
	item, err = s.queue.AddMessageWithResume(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, in.Markdown, in.ExpectedVersion, in.ResumeAgent)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(in.AttachmentIDs) > 0 && len(item.Conversation) > 0 {
		entryID, _ := item.Conversation[len(item.Conversation)-1]["id"].(string)
		placeholders := strings.TrimRight(strings.Repeat("?,", len(in.AttachmentIDs)), ",")
		args := []any{entryID, item.ID}
		for _, attachmentID := range in.AttachmentIDs {
			args = append(args, attachmentID)
		}
		if _, err = s.store.DB.Exec("UPDATE attachments SET entry_id=? WHERE work_item_id=? AND entry_id IS NULL AND id IN ("+placeholders+")", args...); err != nil {
			writeError(w, err)
			return
		}
		item, _ = s.queue.Get(r.Context(), item.ID)
	}
	s.notifyRelated(item, a, "comment", "任务有新评论", in.Markdown, mentionedUserIDs(s.store.DB, in.Markdown))
	if in.ResumeAgent {
		s.exec.Wake()
	}
	writeJSON(w, 201, item)
}

func (s *Server) agentAction(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Action          string `json:"action"`
		Markdown        string `json:"markdown"`
	}
	decode(r, &in)
	item, err = s.queue.AgentAction(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, in.ExpectedVersion, in.Action, in.Markdown)
	if err != nil {
		writeError(w, err)
		return
	}
	s.exec.Wake()
	writeJSON(w, 200, item)
}

func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	if item.Stage == "closed" {
		writeError(w, &domainError{409, "TERMINAL", "Closed work items are read-only", nil})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	if err = r.ParseMultipartForm(25 << 20); err != nil {
		writeError(w, &domainError{413, "ATTACHMENT_TOO_LARGE", "Attachment must be 25 MB or smaller", nil})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, &domainError{422, "ATTACHMENT_REQUIRED", "Choose a file to attach", nil})
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(content) == 0 {
		writeError(w, &domainError{422, "EMPTY_ATTACHMENT", "Empty attachments are not supported", nil})
		return
	}
	name := filepath.Base(strings.TrimSpace(header.Filename))
	if name == "." || name == "" {
		name = "attachment"
	}
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(content)
	}
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	var existing map[string]any
	var existingID, existingEntry, existingName, existingMime, createdAt string
	var existingSize int64
	if scanErr := s.store.DB.QueryRow("SELECT id,COALESCE(entry_id,''),original_name,mime,size,created_at FROM attachments WHERE work_item_id=? AND sha256=?", item.ID, digestText).Scan(&existingID, &existingEntry, &existingName, &existingMime, &existingSize, &createdAt); scanErr == nil {
		existing = map[string]any{"id": existingID, "entry_id": existingEntry, "original_name": existingName, "mime": existingMime, "size": existingSize, "created_at": createdAt}
		writeJSON(w, 200, existing)
		return
	}
	attachmentID := security.Token(18)
	storageKey := attachmentID + filepath.Ext(name)
	attachmentDir := filepath.Join(s.config.DataDir, "attachments")
	if err = os.MkdirAll(attachmentDir, 0o700); err != nil {
		writeError(w, err)
		return
	}
	if err = os.WriteFile(filepath.Join(attachmentDir, storageKey), content, 0o600); err != nil {
		writeError(w, err)
		return
	}
	stamp := now()
	_, err = s.store.DB.Exec("INSERT INTO attachments(id,work_item_id,original_name,mime,size,sha256,storage_key,uploader_type,uploader_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", attachmentID, item.ID, name, mimeType, len(content), digestText, storageKey, a.Type, a.ID, stamp)
	if err != nil {
		_ = os.Remove(filepath.Join(attachmentDir, storageKey))
		writeError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"id": attachmentID, "entry_id": nil, "original_name": name, "mime": mimeType, "size": len(content), "created_at": stamp})
}

func (s *Server) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	var attachmentID, itemID, projectID, name, mimeType, storageKey string
	var size int64
	err := s.store.DB.QueryRow(`SELECT a.id,a.work_item_id,w.project_id,a.original_name,a.mime,a.storage_key,a.size FROM attachments a JOIN work_items w ON w.id=a.work_item_id WHERE a.id=?`, r.PathValue("attachmentId")).Scan(&attachmentID, &itemID, &projectID, &name, &mimeType, &storageKey, &size)
	if err != nil {
		writeError(w, err)
		return
	}
	if !s.canView(projectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	filePath := filepath.Join(s.config.DataDir, "attachments", filepath.Base(storageKey))
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	disposition := "attachment"
	if attachmentCanPreviewInline(mimeType, name) {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, name))
	http.ServeFile(w, r, filePath)
}

func attachmentCanPreviewInline(mimeType, name string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "text/") {
		return true
	}
	if mimeType == "application/json" || mimeType == "application/xml" || mimeType == "application/yaml" || mimeType == "application/x-yaml" {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".txt", ".json", ".jsonl", ".ndjson", ".csv", ".tsv", ".xml", ".yaml", ".yml", ".log", ".ini", ".cfg", ".conf", ".sql", ".js", ".jsx", ".ts", ".tsx", ".css", ".html", ".htm", ".go", ".py", ".sh", ".ps1", ".toml":
		return true
	default:
		return false
	}
}

func (s *Server) validateFollowers(projectID string, followerIDs []string) error {
	seen := map[string]bool{}
	for _, userID := range followerIDs {
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true
		var exists int
		if err := s.store.DB.QueryRow("SELECT 1 FROM project_memberships m JOIN users u ON u.id=m.user_id WHERE m.project_id=? AND m.user_id=? AND u.status='active'", projectID, userID).Scan(&exists); err != nil {
			return &domainError{422, "INVALID_FOLLOWER", "Follower must be an active project member", nil}
		}
	}
	return nil
}

func (s *Server) updateWorkItemFollowers(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ExpectedVersion int64    `json:"expectedVersion"`
		FollowerIDs     []string `json:"followerIds"`
	}
	decode(r, &in)
	if err = s.validateFollowers(item.ProjectID, in.FollowerIDs); err != nil {
		writeError(w, err)
		return
	}
	err = s.store.Write(r.Context(), func(tx *sql.Tx) error {
		var version int64
		if err := tx.QueryRowContext(r.Context(), "SELECT version FROM work_items WHERE id=?", item.ID).Scan(&version); err != nil {
			return err
		}
		if version != in.ExpectedVersion {
			return &workqueue.Error{Status: 409, Code: "VERSION_CONFLICT", Message: "Work item version changed"}
		}
		if _, err := tx.ExecContext(r.Context(), "DELETE FROM work_item_followers WHERE work_item_id=?", item.ID); err != nil {
			return err
		}
		stamp := now()
		seen := map[string]bool{}
		for _, userID := range in.FollowerIDs {
			if userID == "" || seen[userID] {
				continue
			}
			seen[userID] = true
			if _, err := tx.ExecContext(r.Context(), "INSERT INTO work_item_followers(work_item_id,user_id,created_at) VALUES(?,?,?)", item.ID, userID, stamp); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(r.Context(), "UPDATE work_items SET version=version+1,updated_at=? WHERE id=?", stamp, item.ID); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"followerIds": in.FollowerIDs})
		_, err := tx.ExecContext(r.Context(), "INSERT INTO conversation_entries(id,work_item_id,kind,stage,author_type,author_id,payload_json,related_version,created_at) VALUES(?,?,?,?,?,?,?,?,?)", security.Token(18), item.ID, "followers_changed", item.Stage, a.Type, a.ID, string(payload), version+1, stamp)
		return err
	})
	if err != nil {
		writeError(w, err)
		return
	}
	item, err = s.queue.Get(r.Context(), item.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "followers_changed", "任务关注者已更新", item.Title, in.FollowerIDs)
	writeJSON(w, 200, item)
}
func (s *Server) blockWorkItem(w http.ResponseWriter, r *http.Request)   { s.setBlocked(w, r, true) }
func (s *Server) unblockWorkItem(w http.ResponseWriter, r *http.Request) { s.setBlocked(w, r, false) }
func (s *Server) cleanWorkItemWorkspace(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	if err = s.exec.CleanWorkspace(r.Context(), item.ID); err != nil {
		writeError(w, &domainError{409, "WORKSPACE_CLEAN_FAILED", err.Error(), nil})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) setBlocked(w http.ResponseWriter, r *http.Request, blocked bool) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	decode(r, &in)
	item, err = s.queue.SetBlocked(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, in.ExpectedVersion, in.Reason, blocked)
	if err != nil {
		writeError(w, err)
		return
	}
	if blocked {
		if err = s.exec.BlockWorkItem(r.Context(), item.ID); err != nil {
			writeError(w, err)
			return
		}
		item, err = s.queue.Get(r.Context(), item.ID)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	s.notifyRelated(item, a, "blocked_changed", "任务阻塞状态已更新", item.Title, nil)
	s.exec.Wake()
	writeJSON(w, 201, item)
}
func (s *Server) getWorkItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !s.canView(item.ProjectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	if item.Stage == "closed" && item.WorkspacePath != nil {
		item.WorkspaceSizeBytes = directorySize(*item.WorkspacePath)
	}
	writeJSON(w, 200, item)
}

func directorySize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
func (s *Server) listWorkItems(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.URL.Query().Get("projectId")
	if !s.canView(projectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	query := `SELECT id FROM work_items WHERE project_id=?`
	args := []any{projectID}
	if stage := r.URL.Query().Get("stage"); stage != "" {
		query += " AND stage=?"
		args = append(args, stage)
	}
	if q := r.URL.Query().Get("query"); q != "" {
		query += " AND (title LIKE ? OR id LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	query += " ORDER BY CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END,updated_at DESC"
	rows, err := s.store.DB.Query(query, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	itemIDs := []string{}
	for rows.Next() {
		var itemID string
		if err = rows.Scan(&itemID); err == nil {
			itemIDs = append(itemIDs, itemID)
		}
	}
	_ = rows.Close()
	out := []workqueue.WorkItem{}
	for _, itemID := range itemIDs {
		if item, getErr := s.queue.Get(r.Context(), itemID); getErr == nil {
			out = append(out, *item)
		}
	}
	writeJSON(w, 200, out)
}
func (s *Server) listAgentRequests(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.URL.Query().Get("projectId")
	if !s.canView(projectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	requests, err := s.requests.List(r.Context(), projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	if s.requireDeveloper(projectID, a) != nil {
		for index := range requests {
			redactAgentRequest(&requests[index])
		}
	}
	writeJSON(w, 200, requests)
}
func (s *Server) getAgentRequest(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	request, err := s.requests.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !s.canView(request.ProjectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	if s.requireDeveloper(request.ProjectID, a) != nil {
		redactAgentRequest(request)
	}
	writeJSON(w, 200, request)
}
func redactAgentRequest(request *agentrequest.Request) {
	request.OutputJSONL = ""
	request.PromptMarkdown = ""
	request.CommandJSON = ""
	request.WorkspacePath = nil
	request.ThreadID = nil
}
func (s *Server) cancelAgentRequest(w http.ResponseWriter, r *http.Request) {
	s.changeAgentRequest(w, r, false)
}
func (s *Server) retryAgentRequest(w http.ResponseWriter, r *http.Request) {
	s.changeAgentRequest(w, r, true)
}
func (s *Server) approveAgentPlan(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	plan, err := s.requests.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(plan.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	request, err := s.requests.ApprovePlan(r.Context(), plan.ID, a.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	s.exec.Wake()
	writeJSON(w, 201, request)
}
func (s *Server) requestAgentForTask(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	item, err := s.queue.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(item.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	if item.Stage == "closed" {
		writeError(w, &domainError{409, "TERMINAL", "Closed work items are read-only", nil})
		return
	}
	var in struct {
		PlanFirst bool `json:"planFirst"`
	}
	decode(r, &in)
	kind := "task_execution"
	if in.PlanFirst {
		kind = "task_plan"
	}
	request, err := s.requests.Create(r.Context(), agentrequest.CreateInput{ProjectID: item.ProjectID, Kind: kind, SourceWorkItemID: item.ID, Title: "处理任务：" + item.ID + " " + item.Title, CreatedByType: "system"})
	if err != nil {
		writeError(w, err)
		return
	}
	s.exec.Wake()
	writeJSON(w, 201, request)
}
func (s *Server) changeAgentRequest(w http.ResponseWriter, r *http.Request, retry bool) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	request, err := s.requests.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.requireDeveloper(request.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	if retry {
		request, err = s.requests.Retry(r.Context(), request.ID, a.ID)
	} else {
		requestID := request.ID
		request, err = s.requests.Cancel(r.Context(), requestID)
		if err == nil {
			s.exec.Cancel(requestID)
		}
	}
	if err != nil {
		writeError(w, err)
		return
	}
	s.exec.Wake()
	writeJSON(w, 200, request)
}
func (s *Server) listKnowledge(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if !s.canView(projectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	nodes, err := s.knowledge.List(r.Context(), projectID, r.URL.Query().Get("query"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, nodes)
}
func (s *Server) createKnowledgeNode(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if err := s.requireDeveloper(projectID, a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		ParentID           string `json:"parentId"`
		Title              string `json:"title"`
		Markdown           string `json:"markdown"`
		Summary            string `json:"summary"`
		TriggerDescription string `json:"triggerDescription"`
		SortOrder          int    `json:"sortOrder"`
	}
	decode(r, &in)
	node, err := s.knowledge.Create(r.Context(), knowledge.Actor{Type: "human", ID: a.ID}, knowledge.CreateInput{ProjectID: projectID, ParentID: in.ParentID, Title: in.Title, Markdown: in.Markdown, Summary: in.Summary, TriggerDescription: in.TriggerDescription, SortOrder: in.SortOrder})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, node)
}
func (s *Server) knowledgeNode(w http.ResponseWriter, r *http.Request, developer bool) (actor, *knowledge.Node, bool) {
	a, ok := s.human(w, r)
	if !ok {
		return actor{}, nil, false
	}
	node, err := s.knowledge.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return actor{}, nil, false
	}
	if developer {
		err = s.requireDeveloper(node.ProjectID, a)
	} else if !s.canView(node.ProjectID, a) {
		err = &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil}
	}
	if err != nil {
		writeError(w, err)
		return actor{}, nil, false
	}
	return a, node, true
}
func (s *Server) getKnowledgeNode(w http.ResponseWriter, r *http.Request) {
	_, node, ok := s.knowledgeNode(w, r, false)
	if ok {
		writeJSON(w, 200, node)
	}
}
func (s *Server) updateKnowledgeNode(w http.ResponseWriter, r *http.Request) {
	a, node, ok := s.knowledgeNode(w, r, true)
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion    int64   `json:"expectedVersion"`
		Title              string  `json:"title"`
		Markdown           string  `json:"markdown"`
		Summary            *string `json:"summary"`
		TriggerDescription *string `json:"triggerDescription"`
	}
	decode(r, &in)
	summary, trigger := node.Summary, node.TriggerDescription
	if in.Summary != nil {
		summary = *in.Summary
	}
	if in.TriggerDescription != nil {
		trigger = *in.TriggerDescription
	}
	node, err := s.knowledge.Update(r.Context(), knowledge.Actor{Type: "human", ID: a.ID}, knowledge.UpdateInput{ID: node.ID, ExpectedVersion: in.ExpectedVersion, Title: in.Title, Markdown: in.Markdown, Summary: summary, TriggerDescription: trigger})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, node)
}
func (s *Server) moveKnowledgeNode(w http.ResponseWriter, r *http.Request) {
	a, node, ok := s.knowledgeNode(w, r, true)
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		ParentID        string `json:"parentId"`
		SortOrder       int    `json:"sortOrder"`
	}
	decode(r, &in)
	node, err := s.knowledge.Move(r.Context(), knowledge.Actor{Type: "human", ID: a.ID}, node.ID, in.ExpectedVersion, in.ParentID, in.SortOrder)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, node)
}
func (s *Server) deleteKnowledgeNode(w http.ResponseWriter, r *http.Request) {
	a, node, ok := s.knowledgeNode(w, r, true)
	if !ok {
		return
	}
	expectedVersion, err := strconv.ParseInt(r.URL.Query().Get("expectedVersion"), 10, 64)
	if err != nil {
		writeError(w, &domainError{422, "VALIDATION_ERROR", "expectedVersion is required", nil})
		return
	}
	if err = s.knowledge.Delete(r.Context(), knowledge.Actor{Type: "human", ID: a.ID}, node.ID, expectedVersion); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) lockKnowledgeNode(w http.ResponseWriter, r *http.Request) {
	a, node, ok := s.knowledgeNode(w, r, true)
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64 `json:"expectedVersion"`
		Locked          bool  `json:"locked"`
	}
	decode(r, &in)
	node, err := s.knowledge.SetLock(r.Context(), knowledge.Actor{Type: "human", ID: a.ID}, node.ID, in.ExpectedVersion, in.Locked)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, node)
}
func (s *Server) knowledgeRevisions(w http.ResponseWriter, r *http.Request) {
	_, node, ok := s.knowledgeNode(w, r, false)
	if !ok {
		return
	}
	revisions, err := s.knowledge.Revisions(r.Context(), node.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, revisions)
}
func (s *Server) restoreKnowledgeNode(w http.ResponseWriter, r *http.Request) {
	a, node, ok := s.knowledgeNode(w, r, true)
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64 `json:"expectedVersion"`
		RevisionVersion int64 `json:"revisionVersion"`
	}
	decode(r, &in)
	node, err := s.knowledge.Restore(r.Context(), knowledge.Actor{Type: "human", ID: a.ID}, node.ID, in.ExpectedVersion, in.RevisionVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, node)
}
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.URL.Query().Get("projectId")
	if projectID != "" && !s.canView(projectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	query := "SELECT id,project_id,actor_type,actor_id,event_type,object_type,object_id,source,payload_json,created_at FROM activity_events"
	args := []any{}
	countQuery := "SELECT COUNT(*) FROM activity_events"
	countArgs := []any{}
	if projectID != "" {
		query += " WHERE project_id=?"
		args = append(args, projectID)
		countQuery += " WHERE project_id=?"
		countArgs = append(countArgs, projectID)
	}
	page, pageSize := 1, 20
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("pageSize")); err == nil && value > 0 && value <= 100 {
		pageSize = value
	}
	if !r.URL.Query().Has("page") && !r.URL.Query().Has("pageSize") {
		query += " ORDER BY created_at DESC LIMIT 500"
		writeRows(w, s.store.DB, query, args...)
		return
	}
	var total int
	if err := s.store.DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		writeError(w, err)
		return
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, (page-1)*pageSize)
	writeJSON(w, 200, map[string]any{"items": readRows(s.store.DB, query, args...), "page": page, "pageSize": pageSize, "total": total})
}

func mentionedUserIDs(db *sql.DB, markdown string) []string {
	matches := regexp.MustCompile(`@([A-Za-z0-9._-]{2,64})`).FindAllStringSubmatch(markdown, -1)
	ids := []string{}
	seen := map[string]bool{}
	for _, match := range matches {
		var userID string
		if len(match) > 1 && db.QueryRow("SELECT id FROM users WHERE username=? COLLATE NOCASE AND status='active'", match[1]).Scan(&userID) == nil && !seen[userID] {
			seen[userID] = true
			ids = append(ids, userID)
		}
	}
	return ids
}

func (s *Server) notifyRelated(item *workqueue.WorkItem, a actor, kind, title, body string, extra []string) {
	recipients := append([]string{}, extra...)
	if item.CreatedByUserID != nil {
		recipients = append(recipients, *item.CreatedByUserID)
	}
	if item.AssigneeKind != nil && item.AssigneeID != nil && *item.AssigneeKind == "human" {
		recipients = append(recipients, *item.AssigneeID)
	}
	recipients = append(recipients, item.FollowerIDs...)
	s.notifyUsers(item, a, kind, title, body, recipients)
}

func (s *Server) notifyUsers(item *workqueue.WorkItem, a actor, kind, title, body string, recipients []string) {
	seen := map[string]bool{}
	if runes := []rune(body); len(runes) > 240 {
		body = string(runes[:240])
	}
	for _, userID := range recipients {
		if userID == "" || userID == a.ID || seen[userID] {
			continue
		}
		seen[userID] = true
		_, _ = s.store.DB.Exec("INSERT INTO notifications(id,user_id,project_id,work_item_id,kind,title,body,actor_type,actor_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", security.Token(18), userID, item.ProjectID, item.ID, kind, title, body, a.Type, nullableString(a.ID), now())
	}
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
	page, pageSize := 1, 20
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("pageSize")); err == nil && value > 0 && value <= 100 {
		pageSize = value
	}
	paginated := r.URL.Query().Has("page") || r.URL.Query().Has("pageSize")
	query := `SELECT n.id,n.project_id,n.work_item_id,n.kind,n.title,n.body,n.actor_type,n.actor_id,n.read_at,n.created_at,w.title AS work_item_title,p.name AS project_name,p.project_key FROM notifications n LEFT JOIN work_items w ON w.id=n.work_item_id LEFT JOIN projects p ON p.id=n.project_id WHERE n.user_id=?`
	args := []any{a.ID}
	if projectID != "" {
		query += " AND n.project_id=?"
		args = append(args, projectID)
	}
	if !paginated {
		query += " ORDER BY n.created_at DESC LIMIT 200"
		writeJSON(w, 200, readRows(s.store.DB, query, args...))
		return
	}
	var total, unreadCount, filteredUnreadCount int
	countQuery := "SELECT COUNT(*) FROM notifications WHERE user_id=?"
	countArgs := []any{a.ID}
	if projectID != "" {
		countQuery += " AND project_id=?"
		countArgs = append(countArgs, projectID)
	}
	if err := s.store.DB.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.DB.QueryRow("SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL", a.ID).Scan(&unreadCount); err != nil {
		writeError(w, err)
		return
	}
	unreadQuery := countQuery + " AND read_at IS NULL"
	if err := s.store.DB.QueryRow(unreadQuery, countArgs...).Scan(&filteredUnreadCount); err != nil {
		writeError(w, err)
		return
	}
	query += " ORDER BY n.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, (page-1)*pageSize)
	writeJSON(w, 200, map[string]any{"items": readRows(s.store.DB, query, args...), "page": page, "pageSize": pageSize, "total": total, "unreadCount": unreadCount, "filteredUnreadCount": filteredUnreadCount})
}

func readRows(db *sql.DB, query string, args ...any) []map[string]any {
	rows, err := db.Query(query, args...)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	columns, _ := rows.Columns()
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if rows.Scan(pointers...) != nil {
			continue
		}
		entry := map[string]any{}
		for index, column := range columns {
			if raw, ok := values[index].([]byte); ok {
				entry[column] = string(raw)
			} else {
				entry[column] = values[index]
			}
		}
		out = append(out, entry)
	}
	return out
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	_, err := s.store.DB.Exec("UPDATE notifications SET read_at=COALESCE(read_at,?) WHERE id=? AND user_id=?", now(), r.PathValue("id"), a.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	query, args := "UPDATE notifications SET read_at=COALESCE(read_at,?) WHERE user_id=?", []any{now(), a.ID}
	if projectID := strings.TrimSpace(r.URL.Query().Get("projectId")); projectID != "" {
		query += " AND project_id=?"
		args = append(args, projectID)
	}
	_, err := s.store.DB.Exec(query, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) exportActivity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", "attachment; filename=projectboard-audit.ndjson")
	rows, err := s.store.DB.Query("SELECT id,project_id,actor_type,actor_id,event_type,object_type,object_id,source,payload_json,created_at FROM activity_events ORDER BY created_at")
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	enc := json.NewEncoder(w)
	for rows.Next() {
		values := make([]any, 10)
		ptrs := make([]any, 10)
		for i := range values {
			ptrs[i] = &values[i]
		}
		_ = rows.Scan(ptrs...)
		_ = enc.Encode(map[string]any{"id": values[0], "projectId": values[1], "actorType": values[2], "actorId": values[3], "eventType": values[4], "objectType": values[5], "objectId": values[6], "source": values[7], "payload": values[8], "createdAt": values[9]})
	}
}

func (s *Server) audit(a actor, eventType, objectType, objectID, projectID string, payload any) {
	raw, _ := json.Marshal(redact(payload))
	_, _ = s.store.DB.Exec("INSERT INTO activity_events(id,project_id,actor_type,actor_id,event_type,object_type,object_id,source,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", security.Token(18), nullableString(projectID), a.Type, nullableString(a.ID), eventType, objectType, objectID, "web", string(raw), now())
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/mcp") {
		http.NotFound(w, r)
		return
	}
	files := http.FileServerFS(s.static)
	if r.URL.Path != "/" {
		if _, err := fs.Stat(s.static, strings.TrimPrefix(path.Clean(r.URL.Path), "/")); err != nil {
			r.URL.Path = "/"
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	files.ServeHTTP(w, r)
}
func decode(r *http.Request, out any) {
	decoder := json.NewDecoder(io.LimitReader(r.Body, (2<<20)+1))
	if err := decoder.Decode(out); err != nil {
		panic(&domainError{422, "VALIDATION_ERROR", "Invalid JSON body", nil})
	}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	var de *domainError
	var we *workqueue.Error
	var are *agentrequest.Error
	var ke *knowledge.Error
	switch {
	case errors.As(err, &de):
		writeJSON(w, de.Status, map[string]any{"error": map[string]any{"code": de.Code, "message": de.Message, "details": de.Details}})
	case errors.As(err, &we):
		writeJSON(w, we.Status, map[string]any{"error": map[string]any{"code": we.Code, "message": we.Message}})
	case errors.As(err, &are):
		writeJSON(w, are.Status, map[string]any{"error": map[string]any{"code": are.Code, "message": are.Message}})
	case errors.As(err, &ke):
		writeJSON(w, ke.Status, map[string]any{"error": map[string]any{"code": ke.Code, "message": ke.Message}})
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "NOT_FOUND", "message": "Not found"}})
	default:
		writeJSON(w, 500, map[string]any{"error": map[string]any{"code": "INTERNAL_ERROR", "message": "Internal error"}})
	}
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if index := strings.LastIndex(host, ":"); index > 0 {
		host = host[:index]
	}
	return host
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type rowScanner interface{ Scan(...any) error }

func scanProject(row rowScanner) map[string]any {
	var id, key, name, description, projectPath, branch, validations, forbidden, rules, prompts string
	var archived *string
	var allowSubtasks, compactionDays, compactionCount int
	var version int64
	if row.Scan(&id, &key, &name, &description, &archived, &projectPath, &branch, &validations, &forbidden, &rules, &allowSubtasks, &prompts, &compactionDays, &compactionCount, &version) != nil {
		return map[string]any{}
	}
	var validationsV, forbiddenV any
	promptValues := []string{}
	_ = json.Unmarshal([]byte(validations), &validationsV)
	_ = json.Unmarshal([]byte(forbidden), &forbiddenV)
	_ = json.Unmarshal([]byte(prompts), &promptValues)
	return map[string]any{"id": id, "key": key, "name": name, "descriptionMarkdown": description, "archivedAt": archived, "projectPath": projectPath, "defaultTargetBranch": branch, "validationCommands": validationsV, "forbiddenPaths": forbiddenV, "agentRulesMarkdown": rules, "allowSubtasks": allowSubtasks != 0, "agentPrompts": promptValues, "knowledgeCompactionDays": compactionDays, "knowledgeCompactionRequestCount": compactionCount, "configVersion": version}
}
func stringValue(m map[string]any, key string, fallback any) string {
	if value, ok := m[key].(string); ok {
		return value
	}
	value, _ := fallback.(string)
	return value
}
func valueOr(m map[string]any, key string, fallback any) any {
	if value, ok := m[key]; ok {
		return value
	}
	return fallback
}
func boolValue(m map[string]any, key string, fallback any) bool {
	if value, ok := m[key].(bool); ok {
		return value
	}
	value, _ := fallback.(bool)
	return value
}
func intValue(m map[string]any, key string, fallback any) int {
	if value, ok := m[key].(float64); ok {
		return int(value)
	}
	switch value := fallback.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func rowsAsMaps(db *sql.DB, query string, args ...any) []map[string]any {
	rows, err := db.Query(query, args...)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range ptrs {
			ptrs[i] = &values[i]
		}
		_ = rows.Scan(ptrs...)
		item := map[string]any{}
		for i, col := range cols {
			if raw, ok := values[i].([]byte); ok {
				item[col] = string(raw)
			} else {
				item[col] = values[i]
			}
		}
		out = append(out, item)
	}
	return out
}
func redact(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range current {
			if regexp.MustCompile(`(?i)token|secret|password|authorization|cookie|private.?key|credential`).MatchString(key) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redact(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(current))
		for i, item := range current {
			out[i] = redact(item)
		}
		return out
	case string:
		re := regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~-]+`)
		return re.ReplaceAllString(current, "${1}[REDACTED]")
	default:
		return value
	}
}
func writeRows(w http.ResponseWriter, db *sql.DB, query string, args ...any) {
	rows, err := db.Query(query, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range ptrs {
			ptrs[i] = &values[i]
		}
		_ = rows.Scan(ptrs...)
		entry := map[string]any{}
		for i, col := range cols {
			if raw, ok := values[i].([]byte); ok {
				entry[col] = string(raw)
			} else {
				entry[col] = values[i]
			}
		}
		out = append(out, entry)
	}
	writeJSON(w, 200, out)
}

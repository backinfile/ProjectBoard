package server

import (
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
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/projectboard/projectboard/internal/providers"
	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
	webassets "github.com/projectboard/projectboard/web"
)

type Config struct {
	DataDir           string
	DatabasePath      string
	RunnerDownloadDir string
	BootstrapUsername string
	BootstrapPassword string
	Production        bool
	GitConnect        providers.GitConnectConfig
}

type domainError struct {
	Status        int
	Code, Message string
	Details       any
}

func (e *domainError) Error() string { return e.Message }

type actor struct{ Type, ID, Role, Name, SessionID string }
type Server struct {
	store  *store.Store
	queue  *workqueue.Module
	config Config
	static fs.FS
	vault  *providers.Vault
	git    *providers.GitConnector
	gitMu  sync.RWMutex
}

type Application struct {
	handler http.Handler
	server  *Server
}

func (a *Application) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.handler.ServeHTTP(w, r) }
func (a *Application) Close() error                                     { return a.server.store.Close() }

func NewTestHandler(dataDir string) *Application {
	handler, err := New(Config{DataDir: dataDir, BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123"})
	if err != nil {
		panic(err)
	}
	return handler
}

func New(config Config) (*Application, error) {
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
	vault, err := providers.OpenVault(config.DataDir)
	if err != nil {
		database.Close()
		return nil, err
	}
	s := &Server{store: database, queue: workqueue.New(database), config: config, static: static, vault: vault, git: providers.NewGitConnector(config.GitConnect)}
	if err = s.reloadGitConnector(); err != nil {
		database.Close()
		return nil, fmt.Errorf("load web-managed Git provider settings: %w", err)
	}
	return &Application{handler: securityHeaders(s.routes()), server: s}, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handle(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "database": "sqlite", "time": now()})
	}))
	mux.HandleFunc("POST /mcp", s.handle(s.mcpHTTP))
	mux.HandleFunc("POST /api/auth/login", s.handle(s.login))
	mux.HandleFunc("POST /api/auth/logout", s.handle(s.logout))
	mux.HandleFunc("POST /api/agent/pair", s.handle(s.pairAgent))
	mux.HandleFunc("POST /api/agent/poll", s.handle(s.pollAgent))
	mux.HandleFunc("POST /api/agent/assignments/{id}/accept", s.handle(s.claimAssignment))
	mux.HandleFunc("POST /api/agent/work-items/{id}/subtasks", s.handle(s.createAgentSubtask))
	mux.HandleFunc("POST /api/agent/work-items/{id}/stage", s.handle(s.moveAgentWorkItemStage))
	mux.HandleFunc("POST /api/agent/runs/{id}/heartbeat", s.handle(s.heartbeatRun))
	mux.HandleFunc("POST /api/agent/runs/{id}/release", s.handle(s.releaseRun))
	mux.HandleFunc("POST /api/agent/projects/{id}/sync-commits", s.handle(s.syncProjectCommitsAgent))
	mux.HandleFunc("POST /api/agent/runs/{id}/events", s.handle(s.runnerEvent))
	mux.HandleFunc("POST /api/agent/runs/{id}/submit", s.handle(s.runnerSubmit))
	mux.HandleFunc("GET /api/me", s.handle(s.me))
	mux.HandleFunc("PATCH /api/me", s.handle(s.updateMe))
	mux.HandleFunc("POST /api/me/change-password", s.handle(s.changePassword))
	mux.HandleFunc("GET /api/me/sessions", s.handle(s.sessions))
	mux.HandleFunc("DELETE /api/me/sessions/{id}", s.handle(s.revokeSession))
	mux.HandleFunc("GET /api/runner/downloads", s.handle(s.listRunnerDownloads))
	mux.HandleFunc("GET /api/runner/downloads/{name}", s.handle(s.downloadRunner))
	mux.HandleFunc("GET /api/system/settings", s.handle(s.getSystemSettings))
	mux.HandleFunc("PUT /api/system/settings", s.handle(s.updateSystemSettings))
	mux.HandleFunc("GET /api/projects", s.handle(s.listProjects))
	mux.HandleFunc("POST /api/projects", s.handle(s.createProject))
	mux.HandleFunc("PATCH /api/projects/{id}", s.handle(s.updateProject))
	mux.HandleFunc("GET /api/projects/{id}/members", s.handle(s.listMembers))
	mux.HandleFunc("POST /api/projects/{id}/members", s.handle(s.addMember))
	mux.HandleFunc("PATCH /api/projects/{id}/members/{userId}", s.handle(s.updateMember))
	mux.HandleFunc("DELETE /api/projects/{id}/members/{userId}", s.handle(s.removeMember))
	mux.HandleFunc("GET /api/projects/{id}/agents", s.handle(s.listProjectAgents))
	mux.HandleFunc("PUT /api/projects/{id}/agents/{agentId}", s.handle(s.enableAgent))
	mux.HandleFunc("DELETE /api/projects/{id}/agents/{agentId}", s.handle(s.disableAgent))
	mux.HandleFunc("GET /api/users", s.handle(s.listUsers))
	mux.HandleFunc("POST /api/users", s.handle(s.createUser))
	mux.HandleFunc("GET /api/users/{id}", s.handle(s.getUser))
	mux.HandleFunc("PATCH /api/users/{id}", s.handle(s.updateUser))
	mux.HandleFunc("POST /api/users/{id}/disable", s.handle(s.disableUser))
	mux.HandleFunc("POST /api/users/{id}/enable", s.handle(s.enableUser))
	mux.HandleFunc("POST /api/users/{id}/reset-password", s.handle(s.resetPassword))
	mux.HandleFunc("POST /api/agents", s.handle(s.createAgent))
	mux.HandleFunc("GET /api/agents", s.handle(s.listAgents))
	mux.HandleFunc("POST /api/agents/{id}/pairing-codes", s.handle(s.createPairingCode))
	mux.HandleFunc("GET /api/provider-authorizations", s.handle(s.listAuthorizations))
	mux.HandleFunc("POST /api/provider-authorizations", s.handle(s.createAuthorization))
	mux.HandleFunc("GET /api/provider-authorizations/{id}/repositories", s.handle(s.authorizationRepositories))
	mux.HandleFunc("DELETE /api/provider-authorizations/{id}", s.handle(s.revokeAuthorization))
	mux.HandleFunc("GET /api/projects/{id}/repository-grant", s.handle(s.getRepositoryGrant))
	mux.HandleFunc("POST /api/projects/{id}/repository-grant", s.handle(s.bindRepositoryGrant))
	mux.HandleFunc("DELETE /api/projects/{id}/repository-grant", s.handle(s.revokeRepositoryGrant))
	mux.HandleFunc("POST /api/projects/{id}/sync-commits", s.handle(s.syncProjectCommitsHuman))
	mux.HandleFunc("GET /api/git/connections", s.handle(s.gitConnectionConfiguration))
	mux.HandleFunc("GET /api/git/provider-settings", s.handle(s.getGitProviderSettings))
	mux.HandleFunc("PUT /api/git/provider-settings/{provider}", s.handle(s.updateGitProviderSettings))
	mux.HandleFunc("POST /api/git/provider-settings/github/manifest/start", s.handle(s.startGitHubManifest))
	mux.HandleFunc("GET /api/git/provider-settings/github/manifest/callback", s.handle(s.gitHubManifestCallback))
	mux.HandleFunc("GET /api/git/provider-settings/github/manifest-flows/{id}", s.handle(s.getGitHubManifestFlow))
	mux.HandleFunc("POST /api/git/provider-settings/github/manifest-flows/{id}/cancel", s.handle(s.cancelGitHubManifestFlow))
	mux.HandleFunc("POST /api/git/connections/{provider}/start", s.handle(s.startGitConnection))
	mux.HandleFunc("GET /api/git/connections/{provider}/callback", s.handle(s.gitConnectionCallback))
	mux.HandleFunc("GET /api/git/connection-flows/{id}", s.handle(s.getGitConnection))
	mux.HandleFunc("POST /api/git/connection-flows/{id}/complete", s.handle(s.completeGitConnection))
	mux.HandleFunc("POST /api/git/connection-flows/{id}/cancel", s.handle(s.cancelGitConnection))
	mux.HandleFunc("GET /api/work-items", s.handle(s.listWorkItems))
	mux.HandleFunc("POST /api/work-items", s.handle(s.createWorkItem))
	mux.HandleFunc("GET /api/work-items/{id}", s.handle(s.getWorkItem))
	mux.HandleFunc("PATCH /api/work-items/{id}", s.handle(s.updateWorkItem))
	mux.HandleFunc("POST /api/work-items/{id}/stage", s.handle(s.moveWorkItemStage))
	mux.HandleFunc("POST /api/work-items/{id}/assign", s.handle(s.assignWorkItem))
	mux.HandleFunc("POST /api/work-items/{id}/messages", s.handle(s.postMessage))
	mux.HandleFunc("POST /api/work-items/{id}/attachments", s.handle(s.uploadAttachment))
	mux.HandleFunc("GET /api/attachments/{attachmentId}", s.handle(s.downloadAttachment))
	mux.HandleFunc("PUT /api/work-items/{id}/followers", s.handle(s.updateWorkItemFollowers))
	mux.HandleFunc("POST /api/work-items/{id}/block", s.handle(s.blockWorkItem))
	mux.HandleFunc("POST /api/work-items/{id}/unblock", s.handle(s.unblockWorkItem))
	mux.HandleFunc("POST /api/work-items/{id}/abandon", s.handle(s.abandonWorkItem))
	mux.HandleFunc("POST /api/work-items/{id}/discussion-conclusions", s.handle(s.freezeConclusion))
	mux.HandleFunc("POST /api/work-items/{id}/submit-execution", s.handle(s.submitExecution))
	mux.HandleFunc("POST /api/work-items/{id}/acceptance-attempts", s.handle(s.submitAcceptance))
	mux.HandleFunc("GET /api/activity", s.handle(s.activity))
	mux.HandleFunc("GET /api/notifications", s.handle(s.listNotifications))
	mux.HandleFunc("POST /api/notifications/read-all", s.handle(s.readAllNotifications))
	mux.HandleFunc("POST /api/notifications/{id}/read", s.handle(s.readNotification))
	mux.HandleFunc("GET /api/activity/export", s.handle(s.exportActivity))
	mux.HandleFunc("GET /", s.serveStatic)
	return mux
}

func (s *Server) mcpHTTP(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	var request struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	decode(r, &request)
	response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	switch request.Method {
	case "initialize":
		response["result"] = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "projectboard", "version": "go-rewrite"}}
	case "ping":
		response["result"] = map[string]any{}
	case "tools/list":
		response["result"] = map[string]any{"tools": []any{
			map[string]any{"name": "poll_assignments", "description": "List unblocked work assigned to this Agent.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			map[string]any{"name": "claim_assignment", "description": "Claim one assigned work item and create a lease.", "inputSchema": map[string]any{"type": "object", "required": []string{"workItemId", "expectedVersion"}, "properties": map[string]any{"workItemId": map[string]any{"type": "string"}, "expectedVersion": map[string]any{"type": "integer"}}}},
			map[string]any{"name": "create_subtask", "description": "Create a child task when the active project's subtask policy allows it.", "inputSchema": map[string]any{"type": "object", "required": []string{"parentWorkItemId", "title"}, "properties": map[string]any{"parentWorkItemId": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "descriptionMarkdown": map[string]any{"type": "string"}, "acceptanceCriteriaMarkdown": map[string]any{"type": "string"}, "priority": map[string]any{"type": "string"}, "targetBranch": map[string]any{"type": "string"}}}},
			map[string]any{"name": "move_task_stage", "description": "Move an actively leased task through discussion, execution, acceptance, or completion.", "inputSchema": map[string]any{"type": "object", "required": []string{"workItemId", "expectedVersion", "targetStage"}, "properties": map[string]any{"workItemId": map[string]any{"type": "string"}, "expectedVersion": map[string]any{"type": "integer"}, "targetStage": map[string]any{"type": "string"}, "noteMarkdown": map[string]any{"type": "string"}}}},
		}}
	case "tools/call":
		if request.Params.Name == "poll_assignments" {
			items, err := s.pollAssignments(a.ID)
			if err != nil {
				response["error"] = map[string]any{"code": -32000, "message": "query failed"}
				break
			}
			content, _ := json.MarshalIndent(map[string]any{"assignments": items}, "", "  ")
			response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(content)}}}
		} else if request.Params.Name == "claim_assignment" {
			itemID, _ := request.Params.Arguments["workItemId"].(string)
			if itemID == "" {
				response["error"] = map[string]any{"code": -32602, "message": "workItemId is required"}
				break
			}
			body, _ := json.Marshal(map[string]any{"expectedVersion": request.Params.Arguments["expectedVersion"]})
			subrequest := httptest.NewRequest(http.MethodPost, "/api/agent/assignments/"+itemID+"/accept", strings.NewReader(string(body)))
			subrequest.Header.Set("Authorization", r.Header.Get("Authorization"))
			subrequest.Header.Set("Content-Type", "application/json")
			subrequest.SetPathValue("id", itemID)
			recorder := httptest.NewRecorder()
			s.claimAssignment(recorder, subrequest)
			if recorder.Code >= 300 {
				var failure any
				_ = json.Unmarshal(recorder.Body.Bytes(), &failure)
				response["error"] = map[string]any{"code": -32000, "message": fmt.Sprint(failure)}
				break
			}
			var value any
			_ = json.Unmarshal(recorder.Body.Bytes(), &value)
			content, _ := json.MarshalIndent(value, "", "  ")
			response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(content)}}}
		} else if request.Params.Name == "create_subtask" {
			parentID, _ := request.Params.Arguments["parentWorkItemId"].(string)
			title, _ := request.Params.Arguments["title"].(string)
			if parentID == "" || strings.TrimSpace(title) == "" {
				response["error"] = map[string]any{"code": -32602, "message": "parentWorkItemId and title are required"}
				break
			}
			body, _ := json.Marshal(request.Params.Arguments)
			subrequest := httptest.NewRequest(http.MethodPost, "/api/agent/work-items/"+parentID+"/subtasks", strings.NewReader(string(body)))
			subrequest.Header.Set("Authorization", r.Header.Get("Authorization"))
			subrequest.Header.Set("Content-Type", "application/json")
			subrequest.SetPathValue("id", parentID)
			recorder := httptest.NewRecorder()
			s.createAgentSubtask(recorder, subrequest)
			if recorder.Code >= 300 {
				var failure any
				_ = json.Unmarshal(recorder.Body.Bytes(), &failure)
				response["error"] = map[string]any{"code": -32000, "message": fmt.Sprint(failure)}
				break
			}
			var value any
			_ = json.Unmarshal(recorder.Body.Bytes(), &value)
			content, _ := json.MarshalIndent(value, "", "  ")
			response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(content)}}}
		} else if request.Params.Name == "move_task_stage" {
			itemID, _ := request.Params.Arguments["workItemId"].(string)
			if itemID == "" {
				response["error"] = map[string]any{"code": -32602, "message": "workItemId is required"}
				break
			}
			body, _ := json.Marshal(request.Params.Arguments)
			subrequest := httptest.NewRequest(http.MethodPost, "/api/agent/work-items/"+itemID+"/stage", strings.NewReader(string(body)))
			subrequest.Header.Set("Authorization", r.Header.Get("Authorization"))
			subrequest.Header.Set("Content-Type", "application/json")
			subrequest.SetPathValue("id", itemID)
			recorder := httptest.NewRecorder()
			s.moveAgentWorkItemStage(recorder, subrequest)
			if recorder.Code >= 300 {
				var failure any
				_ = json.Unmarshal(recorder.Body.Bytes(), &failure)
				response["error"] = map[string]any{"code": -32000, "message": fmt.Sprint(failure)}
				break
			}
			var value any
			_ = json.Unmarshal(recorder.Body.Bytes(), &value)
			content, _ := json.MarshalIndent(value, "", "  ")
			response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(content)}}}
		} else {
			response["error"] = map[string]any{"code": -32601, "message": "unknown tool"}
		}
	default:
		response["error"] = map[string]any{"code": -32601, "message": "method not found"}
	}
	writeJSON(w, 200, response)
}

func (s *Server) pollAssignments(agentID string) ([]map[string]any, error) {
	rows, err := s.store.DB.Query(`SELECT w.id,w.project_id,w.number,w.title,w.priority,w.stage,w.target_branch,w.version,p.allow_subtasks,p.allow_agent_auto_close,p.allow_agent_auto_close_subtasks,p.agent_prompts_json FROM work_items w JOIN agent_project_grants g ON g.project_id=w.project_id JOIN projects p ON p.id=w.project_id WHERE g.agent_id=? AND p.allow_agent_execution=1 AND w.assignee_kind='agent' AND w.assignee_id=? AND w.assignment_state='reserved' AND w.blocked_at IS NULL AND w.stage NOT IN('completed','order_closed','abandoned') AND NOT EXISTS(SELECT 1 FROM work_item_dependencies d JOIN work_items x ON x.id=d.depends_on_id WHERE d.work_item_id=w.id AND x.stage NOT IN('completed','order_closed')) ORDER BY CASE w.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END,w.created_at`, agentID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, projectID, title, priority, stage, branch, promptsRaw string
		var number, version int64
		var allowSubtasks, allowAutoClose, allowAutoCloseSubtasks int
		if err = rows.Scan(&id, &projectID, &number, &title, &priority, &stage, &branch, &version, &allowSubtasks, &allowAutoClose, &allowAutoCloseSubtasks, &promptsRaw); err != nil {
			return nil, err
		}
		prompts := []string{}
		_ = json.Unmarshal([]byte(promptsRaw), &prompts)
		items = append(items, map[string]any{"id": id, "project_id": projectID, "number": number, "title": title, "priority": priority, "stage": stage, "target_branch": branch, "version": version, "projectPolicy": map[string]any{"allowSubtasks": allowSubtasks != 0, "allowAgentAutoClose": allowAutoClose != 0, "allowAgentAutoCloseSubtasks": allowAutoCloseSubtasks != 0, "agentPrompts": prompts}})
	}
	return items, rows.Err()
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
	query := `SELECT p.id,p.project_key,p.name,p.description_markdown,p.archived_at,p.repository_url,p.remote_name,p.default_target_branch,p.allowed_target_branches_json,p.validation_commands_json,p.forbidden_paths_json,p.agent_rules_markdown,p.discussion_mode,p.execution_mode,p.acceptance_mode,p.allow_agent_execution,p.allow_agent_auto_close,p.allow_subtasks,p.allow_agent_auto_close_subtasks,p.agent_prompts_json,p.config_version FROM projects p`
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
		Key, Name     string
		RepositoryURL string `json:"repositoryUrl"`
	}
	decode(r, &in)
	key := strings.ToLower(strings.TrimSpace(in.Key))
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{1,19}$`).MatchString(key) || strings.TrimSpace(in.Name) == "" {
		writeError(w, &domainError{422, "INVALID_PROJECT", "Valid name and 2-20 character key required", nil})
		return
	}
	id := security.Token(18)
	stamp := now()
	err := s.store.Write(r.Context(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES(?,?,?,?,?,?)`, id, key, in.Name, in.RepositoryURL, stamp, stamp)
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
	repo := stringValue(in, "repositoryUrl", current["repositoryUrl"])
	branch := stringValue(in, "defaultTargetBranch", current["defaultTargetBranch"])
	allowed := valueOr(in, "allowedTargetBranches", current["allowedTargetBranches"])
	allowedRaw, _ := json.Marshal(allowed)
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
	_, err := s.store.DB.Exec("UPDATE projects SET name=?,repository_url=?,default_target_branch=?,allowed_target_branches_json=?,allow_agent_execution=?,allow_agent_auto_close=?,allow_subtasks=?,allow_agent_auto_close_subtasks=?,agent_prompts_json=?,config_version=config_version+1,updated_at=? WHERE id=?", name, repo, branch, string(allowedRaw), boolInt(boolValue(in, "allowAgentExecution", current["allowAgentExecution"])), boolInt(boolValue(in, "allowAgentAutoClose", current["allowAgentAutoClose"])), boolInt(boolValue(in, "allowSubtasks", current["allowSubtasks"])), boolInt(boolValue(in, "allowAgentAutoCloseSubtasks", current["allowAgentAutoCloseSubtasks"])), string(promptsRaw), now(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	project, _ := s.project(id)
	writeJSON(w, 200, project)
}
func (s *Server) project(id string) (map[string]any, error) {
	rows, err := s.store.DB.Query(`SELECT id,project_key,name,description_markdown,archived_at,repository_url,remote_name,default_target_branch,allowed_target_branches_json,validation_commands_json,forbidden_paths_json,agent_rules_markdown,discussion_mode,execution_mode,acceptance_mode,allow_agent_execution,allow_agent_auto_close,allow_subtasks,allow_agent_auto_close_subtasks,agent_prompts_json,config_version FROM projects WHERE id=?`, id)
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
	rows, err := s.store.DB.Query(`SELECT u.id,u.username,u.display_name,u.system_role,u.status,u.last_active_at,(SELECT COUNT(*) FROM project_memberships m WHERE m.user_id=u.id) FROM users u ORDER BY u.display_name`)
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
	_, err := s.store.DB.Exec("UPDATE users SET status='active',disabled_reason=NULL,updated_at=? WHERE id=?", now(), id)
	if err != nil {
		writeError(w, err)
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
	rows, err := s.store.DB.Query(`SELECT u.id,u.username,u.display_name,u.system_role,u.status,m.role FROM users u LEFT JOIN project_memberships m ON m.user_id=u.id AND m.project_id=? ORDER BY u.display_name`, projectID)
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
	var in struct{ Name, Purpose string }
	decode(r, &in)
	id, stamp := security.Token(18), now()
	_, err := s.store.DB.Exec("INSERT INTO agents(id,name,purpose,status,created_at) VALUES(?,?,?,'active',?)", id, in.Name, in.Purpose, stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "agent.created", "agent", id, "", map[string]any{"name": in.Name})
	writeJSON(w, 201, map[string]any{"id": id, "name": in.Name, "purpose": in.Purpose, "enabled": true})
}
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.store.DB.Query(`SELECT a.id,a.name,a.purpose,a.status,COALESCE(k.expiry_policy,'permanent'),(SELECT COUNT(*) FROM agent_tokens t WHERE t.agent_id=a.id AND t.revoked_at IS NULL),(SELECT MAX(created_at) FROM runner_pairing_codes c WHERE c.agent_id=a.id) FROM agents a LEFT JOIN agent_key_settings k ON k.agent_id=a.id ORDER BY a.name`)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, purpose, status, policy string
		var tokens int
		var lastKey *string
		if err = rows.Scan(&id, &name, &purpose, &status, &policy, &tokens, &lastKey); err != nil {
			writeError(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "purpose": purpose, "status": status, "keyPolicy": policy, "activeTokenCount": tokens, "lastKeyCreatedAt": lastKey})
	}
	writeJSON(w, 200, out)
}
func (s *Server) createPairingCode(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	agentID := r.PathValue("id")
	var in struct {
		ExpiryPolicy string `json:"expiryPolicy"`
	}
	decode(r, &in)
	if in.ExpiryPolicy == "" {
		in.ExpiryPolicy = "permanent"
	}
	allowed := map[string]bool{"disabled": true, "1h": true, "1d": true, "1mo": true, "permanent": true}
	if !allowed[in.ExpiryPolicy] {
		writeError(w, &domainError{422, "INVALID_EXPIRY_POLICY", "Unsupported Agent Key expiry policy", nil})
		return
	}
	stamp := now()
	err := s.store.Write(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO agent_key_settings(agent_id,expiry_policy,updated_at) VALUES(?,?,?) ON CONFLICT(agent_id) DO UPDATE SET expiry_policy=excluded.expiry_policy,updated_at=excluded.updated_at", agentID, in.ExpiryPolicy, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE runner_pairing_codes SET invalidated_at=? WHERE agent_id=? AND consumed_at IS NULL AND invalidated_at IS NULL", stamp, agentID); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE agent_tokens SET revoked_at=? WHERE agent_id=? AND revoked_at IS NULL", stamp, agentID)
		return err
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if in.ExpiryPolicy == "disabled" {
		s.audit(a, "agent.key_disabled", "agent", agentID, "", map[string]any{})
		writeJSON(w, 200, map[string]any{"disabled": true, "expiryPolicy": in.ExpiryPolicy})
		return
	}
	code := "PB-" + strings.ToUpper(security.Token(6))
	code = strings.ReplaceAll(code, "_", "A")
	code = strings.ReplaceAll(code, "-", "-")
	expiresAt := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	switch in.ExpiryPolicy {
	case "1h":
		expiresAt = time.Now().UTC().Add(time.Hour)
	case "1d":
		expiresAt = time.Now().UTC().Add(24 * time.Hour)
	case "1mo":
		expiresAt = time.Now().UTC().AddDate(0, 1, 0)
	}
	expires := expiresAt.Format(time.RFC3339Nano)
	_, err = s.store.DB.Exec("INSERT INTO runner_pairing_codes(id,agent_id,code_hash,expires_at,created_at) VALUES(?,?,?,?,?)", security.Token(18), agentID, security.HashOpaque(code), expires, stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "agent.key_reset", "agent", agentID, "", map[string]any{"expiryPolicy": in.ExpiryPolicy})
	writeJSON(w, 201, map[string]any{"code": code, "expiresAt": expires, "expiryPolicy": in.ExpiryPolicy, "command": "projectboard-runner connect " + code})
}
func (s *Server) pairAgent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code            string `json:"code"`
		DeviceName      string `json:"deviceName"`
		OS              string `json:"os"`
		Version         string `json:"version"`
		PublicKeyDigest string `json:"publicKeyDigest"`
	}
	decode(r, &in)
	var codeID, agentID, expires string
	err := s.store.DB.QueryRow("SELECT id,agent_id,expires_at FROM runner_pairing_codes WHERE code_hash=? AND consumed_at IS NULL AND invalidated_at IS NULL", security.HashOpaque(in.Code)).Scan(&codeID, &agentID, &expires)
	if err != nil || expires <= now() {
		writeError(w, &domainError{401, "PAIRING_CODE_INVALID", "Agent Key is invalid or expired", nil})
		return
	}
	token, deviceID := security.Token(32), security.Token(18)
	stamp := now()
	err = s.store.Write(r.Context(), func(tx *sql.Tx) error {
		result, err := tx.Exec("UPDATE runner_pairing_codes SET consumed_at=? WHERE id=? AND consumed_at IS NULL", stamp, codeID)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return &domainError{409, "PAIRING_CODE_USED", "Agent Key has already been used", nil}
		}
		if _, err = tx.Exec("INSERT INTO agent_tokens(id,agent_id,token_hash,created_at) VALUES(?,?,?,?)", security.Token(18), agentID, security.HashOpaque(token), stamp); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO runner_devices(id,agent_id,device_name,os,version,public_key_digest,paired_at,status) VALUES(?,?,?,?,?,?,?,'idle')", deviceID, agentID, in.DeviceName, in.OS, in.Version, in.PublicKeyDigest, stamp); err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO runner_status(agent_id,device_id,status,paused,updated_at) VALUES(?,?, 'idle',0,?) ON CONFLICT(agent_id) DO UPDATE SET device_id=excluded.device_id,status='idle',paused=0,updated_at=excluded.updated_at", agentID, deviceID, stamp)
		return err
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"agentToken": token, "deviceId": deviceID})
}
func (s *Server) authenticateAgent(r *http.Request) (actor, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return actor{}, &domainError{401, "AGENT_REQUIRED", "Agent authentication required", nil}
	}
	token := strings.TrimPrefix(header, "Bearer ")
	var a actor
	err := s.store.DB.QueryRow(`SELECT a.id,a.name FROM agent_tokens t JOIN agents a ON a.id=t.agent_id WHERE t.token_hash=? AND t.revoked_at IS NULL AND a.revoked_at IS NULL AND a.status='active'`, security.HashOpaque(token)).Scan(&a.ID, &a.Name)
	if err != nil {
		return actor{}, &domainError{401, "AGENT_REQUIRED", "Agent authentication required", nil}
	}
	a.Type = "agent"
	return a, nil
}
func (s *Server) agent(w http.ResponseWriter, r *http.Request) (actor, bool) {
	a, err := s.authenticateAgent(r)
	if err != nil {
		writeError(w, err)
		return actor{}, false
	}
	return a, true
}
func (s *Server) pollAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	items, err := s.pollAssignments(a.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	_, _ = s.store.DB.Exec("UPDATE runner_devices SET last_heartbeat_at=? WHERE agent_id=?", now(), a.ID)
	writeJSON(w, 200, map[string]any{"assignments": items})
}
func (s *Server) claimAssignment(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	itemID := r.PathValue("id")
	var in struct {
		ExpectedVersion int64 `json:"expectedVersion"`
	}
	decode(r, &in)
	var leaseID, runID, expires, promptsRaw string
	var allowSubtasks, allowAutoClose, allowAutoCloseSubtasks int
	err := s.store.Write(r.Context(), func(tx *sql.Tx) error {
		stamp := now()
		_, _ = tx.Exec("UPDATE leases SET released_at=?,release_reason='expired' WHERE released_at IS NULL AND expires_at<=?", stamp, stamp)
		var projectID, kind, assignee, stage, blocked string
		var version int64
		var blockedPtr *string
		if err := tx.QueryRow("SELECT project_id,assignee_kind,assignee_id,stage,blocked_at,version FROM work_items WHERE id=?", itemID).Scan(&projectID, &kind, &assignee, &stage, &blockedPtr, &version); err != nil {
			return err
		}
		_ = blocked
		if version != in.ExpectedVersion {
			return &domainError{409, "VERSION_CONFLICT", "Work item version changed", nil}
		}
		if kind != "agent" || assignee != a.ID {
			return &domainError{409, "NOT_ASSIGNED", "Work item is not assigned to this agent", nil}
		}
		var allowExecution int
		if err := tx.QueryRow("SELECT allow_agent_execution,allow_subtasks,allow_agent_auto_close,allow_agent_auto_close_subtasks,agent_prompts_json FROM projects WHERE id=?", projectID).Scan(&allowExecution, &allowSubtasks, &allowAutoClose, &allowAutoCloseSubtasks, &promptsRaw); err != nil {
			return err
		}
		if allowExecution == 0 {
			return &domainError{409, "AGENT_EXECUTION_DISABLED", "Agent execution is disabled for this project", nil}
		}
		if blockedPtr != nil || stage == "completed" || stage == "order_closed" || stage == "abandoned" {
			return &domainError{409, "NOT_CLAIMABLE", "Work item cannot be claimed", nil}
		}
		var active int
		if tx.QueryRow("SELECT 1 FROM leases WHERE agent_id=? AND released_at IS NULL AND expires_at>?", a.ID, stamp).Scan(&active) == nil {
			return &domainError{409, "AGENT_CAPACITY_EXCEEDED", "Agent already has an active lease", nil}
		}
		var assignmentID string
		if err := tx.QueryRow("SELECT id FROM assignments WHERE work_item_id=? AND ended_at IS NULL ORDER BY created_at DESC LIMIT 1", itemID).Scan(&assignmentID); err != nil {
			return &domainError{409, "NO_ASSIGNMENT", "Assignment missing", nil}
		}
		leaseID, runID = security.Token(18), security.Token(18)
		expires = time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
		if _, err := tx.Exec("INSERT INTO leases(id,work_item_id,assignment_id,agent_id,generation,expires_at,created_at) VALUES(?,?,?,?,1,?,?)", leaseID, itemID, assignmentID, a.ID, expires, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO runner_runs(id,work_item_id,agent_id,lease_id,state,created_at) VALUES(?,?,?,?,?,?)", runID, itemID, a.ID, leaseID, stage, stamp); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE work_items SET assignment_state='active',version=version+1,updated_at=? WHERE id=?", stamp, itemID); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO runner_status(agent_id,status,current_run_id,paused,updated_at) VALUES(?, ?, ?,0,?) ON CONFLICT(agent_id) DO UPDATE SET status=excluded.status,current_run_id=excluded.current_run_id,updated_at=excluded.updated_at", a.ID, stage, runID, stamp)
		return err
	})
	if err != nil {
		writeError(w, err)
		return
	}
	item, _ := s.queue.Get(r.Context(), itemID)
	prompts := []string{}
	_ = json.Unmarshal([]byte(promptsRaw), &prompts)
	writeJSON(w, 201, map[string]any{"id": leaseID, "leaseId": leaseID, "expiresAt": expires, "runId": runID, "workItem": item, "projectPolicy": map[string]any{"allowSubtasks": allowSubtasks != 0, "allowAgentAutoClose": allowAutoClose != 0, "allowAgentAutoCloseSubtasks": allowAutoCloseSubtasks != 0, "agentPrompts": prompts}})
}

func (s *Server) createAgentSubtask(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	parentID := r.PathValue("id")
	var in struct {
		Title                      string `json:"title"`
		DescriptionMarkdown        string `json:"descriptionMarkdown"`
		AcceptanceCriteriaMarkdown string `json:"acceptanceCriteriaMarkdown"`
		Priority                   string `json:"priority"`
		TargetBranch               string `json:"targetBranch"`
	}
	decode(r, &in)
	if strings.TrimSpace(in.Title) == "" {
		writeError(w, &domainError{422, "TITLE_REQUIRED", "Subtask title is required", nil})
		return
	}
	if in.Priority == "" {
		in.Priority = "medium"
	}
	if !map[string]bool{"urgent": true, "high": true, "medium": true, "low": true}[in.Priority] {
		writeError(w, &domainError{422, "INVALID_PRIORITY", "Unsupported subtask priority", nil})
		return
	}
	var projectID, stage string
	var allowExecution, allowSubtasks, childCount int
	err := s.store.DB.QueryRow(`SELECT w.project_id,w.stage,p.allow_agent_execution,p.allow_subtasks,
		(SELECT COUNT(*) FROM work_items c WHERE c.parent_id=w.id AND c.stage NOT IN('completed','order_closed','abandoned'))
		FROM work_items w JOIN projects p ON p.id=w.project_id
		WHERE w.id=? AND w.assignee_kind='agent' AND w.assignee_id=?
		AND EXISTS(SELECT 1 FROM leases l WHERE l.work_item_id=w.id AND l.agent_id=? AND l.released_at IS NULL AND l.expires_at>?)`, parentID, a.ID, a.ID, now()).Scan(&projectID, &stage, &allowExecution, &allowSubtasks, &childCount)
	if err == sql.ErrNoRows {
		writeError(w, &domainError{409, "ACTIVE_PARENT_REQUIRED", "An active lease on the assigned parent task is required", nil})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if allowExecution == 0 || allowSubtasks == 0 {
		writeError(w, &domainError{409, "SUBTASKS_DISABLED", "Agent-created subtasks are disabled for this project", nil})
		return
	}
	if stage == "completed" || stage == "order_closed" || stage == "abandoned" {
		writeError(w, &domainError{409, "TERMINAL", "A terminal task cannot create subtasks", nil})
		return
	}
	if childCount >= 20 {
		writeError(w, &domainError{409, "SUBTASK_LIMIT", "The active child task limit has been reached", nil})
		return
	}
	item, err := s.queue.Create(r.Context(), workqueue.Actor{Type: "agent", ID: a.ID}, workqueue.CreateInput{
		ProjectID:                  projectID,
		ParentID:                   parentID,
		Title:                      strings.TrimSpace(in.Title),
		DescriptionMarkdown:        in.DescriptionMarkdown,
		AcceptanceCriteriaMarkdown: in.AcceptanceCriteriaMarkdown,
		Priority:                   in.Priority,
		TargetBranch:               in.TargetBranch,
		AssigneeKind:               "agent",
		AssigneeID:                 a.ID,
		Stage:                      "todo",
	})
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "work_item.subtask_created", "work_item", item.ID, projectID, map[string]any{"parentId": parentID})
	writeJSON(w, 201, item)
}

func (s *Server) moveAgentWorkItemStage(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	itemID := r.PathValue("id")
	var in struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		TargetStage     string `json:"targetStage"`
		NoteMarkdown    string `json:"noteMarkdown"`
	}
	decode(r, &in)
	if !map[string]bool{"discussion": true, "execution": true, "acceptance": true, "completed": true}[in.TargetStage] {
		writeError(w, &domainError{422, "INVALID_AGENT_STAGE", "Agent stage must be discussion, execution, acceptance, or completed", nil})
		return
	}
	var allowExecution, allowTaskClose, allowSubtaskClose int
	var parentID *string
	err := s.store.DB.QueryRow(`SELECT p.allow_agent_execution,p.allow_agent_auto_close,p.allow_agent_auto_close_subtasks,w.parent_id
		FROM work_items w JOIN projects p ON p.id=w.project_id
		WHERE w.id=? AND w.assignee_kind='agent' AND w.assignee_id=?
		AND EXISTS(SELECT 1 FROM leases l WHERE l.work_item_id=w.id AND l.agent_id=? AND l.released_at IS NULL AND l.expires_at>?)`, itemID, a.ID, a.ID, now()).Scan(&allowExecution, &allowTaskClose, &allowSubtaskClose, &parentID)
	if err == sql.ErrNoRows {
		writeError(w, &domainError{409, "ACTIVE_LEASE_REQUIRED", "An active lease on the assigned task is required", nil})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if allowExecution == 0 {
		writeError(w, &domainError{409, "AGENT_EXECUTION_DISABLED", "Agent execution is disabled for this project", nil})
		return
	}
	target := in.TargetStage
	if target == "completed" && ((parentID == nil && allowTaskClose != 0) || (parentID != nil && allowSubtaskClose != 0)) {
		target = "order_closed"
	}
	item, err := s.queue.MoveStage(r.Context(), workqueue.Actor{Type: "agent", ID: a.ID}, itemID, in.ExpectedVersion, target, in.NoteMarkdown)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "work_item.stage_moved", "work_item", item.ID, item.ProjectID, map[string]any{"targetStage": target})
	writeJSON(w, 200, item)
}
func (s *Server) heartbeatRun(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	var in struct {
		LeaseID string `json:"leaseId"`
	}
	decode(r, &in)
	expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	result, err := s.store.DB.Exec(`UPDATE leases SET expires_at=? WHERE id=? AND agent_id=? AND released_at IS NULL AND EXISTS(SELECT 1 FROM runner_runs WHERE id=? AND lease_id=leases.id AND ended_at IS NULL)`, expires, in.LeaseID, a.ID, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		writeError(w, &domainError{409, "LEASE_INVALID", "Lease or run is no longer valid", nil})
		return
	}
	writeJSON(w, 200, map[string]any{"valid": true, "expiresAt": expires})
}
func (s *Server) releaseRun(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkItemID string `json:"workItemId"`
		LeaseID    string `json:"leaseId"`
		Reason     string `json:"reason"`
	}
	decode(r, &in)
	stamp := now()
	err := s.store.Write(r.Context(), func(tx *sql.Tx) error {
		result, err := tx.Exec("UPDATE leases SET released_at=?,release_reason=? WHERE id=? AND agent_id=? AND released_at IS NULL", stamp, in.Reason, in.LeaseID, a.ID)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return &domainError{409, "LEASE_INVALID", "Lease already inactive", nil}
		}
		_, _ = tx.Exec("UPDATE runner_runs SET ended_at=?,end_reason=? WHERE id=? AND lease_id=?", stamp, in.Reason, r.PathValue("id"), in.LeaseID)
		_, _ = tx.Exec("UPDATE work_items SET assignment_state='reserved',version=version+1,updated_at=? WHERE id=?", stamp, in.WorkItemID)
		_, err = tx.Exec("UPDATE runner_status SET status='idle',current_run_id=NULL,updated_at=? WHERE agent_id=?", stamp, a.ID)
		return err
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"released": true})
}
func (s *Server) runnerEvent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkItemID      string `json:"workItemId"`
		Markdown        string `json:"markdown"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	decode(r, &in)
	item, err := s.queue.AddMessage(r.Context(), workqueue.Actor{Type: "agent", ID: a.ID}, in.WorkItemID, in.Markdown, in.ExpectedVersion)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "comment", "任务有新的 Agent 消息", in.Markdown, mentionedUserIDs(s.store.DB, in.Markdown))
	writeJSON(w, 201, item)
}
func (s *Server) runnerSubmit(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agent(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkItemID          string                 `json:"workItemId"`
		LeaseID             string                 `json:"leaseId"`
		ExpectedVersion     int64                  `json:"expectedVersion"`
		Summary             string                 `json:"summaryMarkdown"`
		Pushed              bool                   `json:"pushed"`
		WorktreeClean       bool                   `json:"worktreeClean"`
		ForbiddenPathsClean bool                   `json:"forbiddenPathsClean"`
		Commits             []string               `json:"commits"`
		ChangedFiles        []string               `json:"changedFiles"`
		Validations         []workqueue.Validation `json:"validations"`
		RemainingRisks      string                 `json:"remainingRisksMarkdown"`
	}
	decode(r, &in)
	var valid int
	if s.store.DB.QueryRow(`SELECT 1 FROM leases l JOIN runner_runs rr ON rr.lease_id=l.id WHERE rr.id=? AND l.id=? AND l.agent_id=? AND l.released_at IS NULL AND l.expires_at>?`, r.PathValue("id"), in.LeaseID, a.ID, now()).Scan(&valid) != nil {
		writeError(w, &domainError{409, "LEASE_INVALID", "Active lease required", nil})
		return
	}
	item, err := s.queue.SubmitExecution(r.Context(), workqueue.Actor{Type: "agent", ID: a.ID}, in.WorkItemID, workqueue.ExecutionInput{ExpectedVersion: in.ExpectedVersion, Summary: in.Summary, Pushed: in.Pushed, WorktreeClean: in.WorktreeClean, ForbiddenPathsClean: in.ForbiddenPathsClean, Commits: in.Commits, ChangedFiles: in.ChangedFiles, Validations: in.Validations, RemainingRisks: in.RemainingRisks})
	if err != nil {
		writeError(w, err)
		return
	}
	stamp := now()
	_, _ = s.store.DB.Exec("UPDATE leases SET released_at=?,release_reason='submitted' WHERE id=?", stamp, in.LeaseID)
	_, _ = s.store.DB.Exec("UPDATE runner_runs SET ended_at=?,end_reason='submitted' WHERE id=?", stamp, r.PathValue("id"))
	s.notifyRelated(item, a, "stage_changed", "Agent 已提交执行结果，任务进入验收", item.Title, nil)
	writeJSON(w, 200, item)
}
func (s *Server) listProjectAgents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if !s.canView(projectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	heartbeatCutoff := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano)
	rows, err := s.store.DB.Query(`SELECT a.id,a.name,a.purpose,a.status,CASE WHEN g.agent_id IS NULL THEN 0 ELSE 1 END,CASE WHEN COALESCE(k.expiry_policy,'permanent')<>'disabled' AND EXISTS(SELECT 1 FROM agent_tokens t WHERE t.agent_id=a.id AND t.revoked_at IS NULL) THEN 1 ELSE 0 END,CASE WHEN EXISTS(SELECT 1 FROM runner_devices d WHERE d.agent_id=a.id AND d.last_heartbeat_at>=?) THEN 1 ELSE 0 END FROM agents a LEFT JOIN agent_project_grants g ON g.agent_id=a.id AND g.project_id=? LEFT JOIN agent_key_settings k ON k.agent_id=a.id ORDER BY a.name`, heartbeatCutoff, projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, purpose, status string
		var enabled, keyActive, runnerConnected int
		_ = rows.Scan(&id, &name, &purpose, &status, &enabled, &keyActive, &runnerConnected)
		out = append(out, map[string]any{"id": id, "name": name, "purpose": purpose, "status": status, "enabled": enabled != 0, "keyActive": keyActive != 0, "runnerConnected": runnerConnected != 0})
	}
	writeJSON(w, 200, out)
}
func (s *Server) enableAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if err := s.requireDeveloper(projectID, a); err != nil {
		writeError(w, err)
		return
	}
	_, err := s.store.DB.Exec("INSERT OR IGNORE INTO agent_project_grants(agent_id,project_id,created_at) VALUES(?,?,?)", r.PathValue("agentId"), projectID, now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) disableAgent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if err := s.requireDeveloper(projectID, a); err != nil {
		writeError(w, err)
		return
	}
	_, err := s.store.DB.Exec("DELETE FROM agent_project_grants WHERE agent_id=? AND project_id=?", r.PathValue("agentId"), projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listAuthorizations(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.store.DB.Query(`SELECT p.id,p.provider,p.name,p.base_url,p.app_id,p.installation_id,p.status,p.created_at,(SELECT COUNT(*) FROM project_repository_grants g WHERE g.authorization_id=p.id AND g.revoked_at IS NULL),m.external_account,m.webhook_status FROM provider_authorizations p LEFT JOIN provider_authorization_metadata m ON m.authorization_id=p.id ORDER BY p.name`)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, provider, name, base, status, created string
		var appID, installation, externalAccount, webhookStatus *string
		var count int
		_ = rows.Scan(&id, &provider, &name, &base, &appID, &installation, &status, &created, &count, &externalAccount, &webhookStatus)
		out = append(out, map[string]any{"id": id, "provider": provider, "name": name, "baseUrl": base, "appId": appID, "installationId": installation, "status": status, "createdAt": created, "bindingCount": count, "externalAccount": externalAccount, "webhookStatus": webhookStatus})
	}
	writeJSON(w, 200, out)
}
func (s *Server) createAuthorization(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		Provider       string `json:"provider"`
		Name           string `json:"name"`
		BaseURL        string `json:"baseUrl"`
		AppID          string `json:"appId"`
		InstallationID string `json:"installationId"`
		Secret         string `json:"secret"`
		WebhookSecret  string `json:"webhookSecret"`
	}
	decode(r, &in)
	if in.Provider == "github" {
		writeError(w, &domainError{422, "GITHUB_BROWSER_SECRET_FORBIDDEN", "GitHub App credentials must be configured by the deployment; connect GitHub from project settings", nil})
		return
	}
	if in.Provider != "github" && in.Provider != "gitlab" {
		writeError(w, &domainError{422, "INVALID_PROVIDER", "Provider must be github or gitlab", nil})
		return
	}
	if in.BaseURL == "" {
		if in.Provider == "github" {
			in.BaseURL = "https://api.github.com"
		} else {
			in.BaseURL = "https://gitlab.com"
		}
	}
	sealed, err := s.vault.Seal(in.Secret)
	if err != nil {
		writeError(w, err)
		return
	}
	webhook, _ := s.vault.Seal(in.WebhookSecret)
	id, stamp := security.Token(18), now()
	_, err = s.store.DB.Exec("INSERT INTO provider_authorizations(id,provider,name,base_url,app_id,installation_id,encrypted_secret,webhook_secret,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?, 'active',?,?)", id, in.Provider, in.Name, in.BaseURL, nullableString(in.AppID), nullableString(in.InstallationID), sealed, webhook, stamp, stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "provider.authorization_created", "provider_authorization", id, "", map[string]any{"provider": in.Provider})
	writeJSON(w, 201, map[string]any{"id": id, "provider": in.Provider, "name": in.Name, "status": "active"})
}
func (s *Server) revokeAuthorization(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	var count int
	_ = s.store.DB.QueryRow("SELECT COUNT(*) FROM project_repository_grants WHERE authorization_id=? AND revoked_at IS NULL", id).Scan(&count)
	if count > 0 {
		writeError(w, &domainError{409, "AUTHORIZATION_IN_USE", "Revoke project grants first", map[string]any{"bindingCount": count}})
		return
	}
	_, err := s.store.DB.Exec("UPDATE provider_authorizations SET status='revoked',updated_at=? WHERE id=?", now(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) getRepositoryGrant(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if !s.canView(projectID, a) {
		writeError(w, &domainError{403, "PROJECT_ACCESS_REQUIRED", "Project access required", nil})
		return
	}
	grant, err := s.repositoryGrant(projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, grant)
}
func (s *Server) bindRepositoryGrant(w http.ResponseWriter, r *http.Request) {
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
		AuthorizationID string `json:"authorizationId"`
		RepositoryID    string `json:"repositoryId"`
		RepositoryName  string `json:"repositoryName"`
		CloneURL        string `json:"cloneUrl"`
		DefaultBranch   string `json:"defaultBranch"`
		AccessLevel     string `json:"accessLevel"`
	}
	decode(r, &in)
	if in.AccessLevel == "" {
		in.AccessLevel = "write"
	}
	if in.AccessLevel != "read" && in.AccessLevel != "write" {
		writeError(w, &domainError{422, "INVALID_ACCESS_LEVEL", "Access level must be read or write", nil})
		return
	}
	var authorizationStatus string
	if err := s.store.DB.QueryRow("SELECT status FROM provider_authorizations WHERE id=?", in.AuthorizationID).Scan(&authorizationStatus); err != nil || authorizationStatus != "active" {
		writeError(w, &domainError{422, "AUTHORIZATION_UNAVAILABLE", "Provider authorization is missing or revoked", nil})
		return
	}
	var catalogCount int
	if err := s.store.DB.QueryRow("SELECT COUNT(*) FROM provider_authorization_repositories WHERE authorization_id=?", in.AuthorizationID).Scan(&catalogCount); err != nil {
		writeError(w, err)
		return
	}
	if catalogCount > 0 {
		var canWrite bool
		err := s.store.DB.QueryRow("SELECT repository_name,clone_url,default_branch,can_write FROM provider_authorization_repositories WHERE authorization_id=? AND repository_id=?", in.AuthorizationID, in.RepositoryID).Scan(&in.RepositoryName, &in.CloneURL, &in.DefaultBranch, &canWrite)
		if err == sql.ErrNoRows {
			writeError(w, &domainError{422, "REPOSITORY_NOT_AUTHORIZED", "Repository is not available to this verified provider connection", nil})
			return
		}
		if err != nil {
			writeError(w, err)
			return
		}
		if in.AccessLevel == "write" && !canWrite {
			writeError(w, &domainError{422, "INSUFFICIENT_REPOSITORY_PERMISSION", "The provider connection does not have write permission for this repository", nil})
			return
		}
	}
	id, stamp := security.Token(18), now()
	_, err := s.store.DB.Exec(`INSERT INTO project_repository_grants(id,project_id,authorization_id,repository_id,repository_name,clone_url,default_branch,access_level,approved_by,approved_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET authorization_id=excluded.authorization_id,repository_id=excluded.repository_id,repository_name=excluded.repository_name,clone_url=excluded.clone_url,default_branch=excluded.default_branch,access_level=excluded.access_level,approved_by=excluded.approved_by,approved_at=excluded.approved_at,revoked_at=NULL`, id, projectID, in.AuthorizationID, in.RepositoryID, in.RepositoryName, in.CloneURL, in.DefaultBranch, in.AccessLevel, a.ID, stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "project.repository_granted", "repository", in.RepositoryID, projectID, map[string]any{"authorizationId": in.AuthorizationID, "accessLevel": in.AccessLevel})
	grant, _ := s.repositoryGrant(projectID)
	writeJSON(w, 201, grant)
}
func (s *Server) revokeRepositoryGrant(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if err := s.requireDeveloper(projectID, a); err != nil {
		writeError(w, err)
		return
	}
	_, err := s.store.DB.Exec("UPDATE project_repository_grants SET revoked_at=? WHERE project_id=? AND revoked_at IS NULL", now(), projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "project.repository_revoked", "project", projectID, projectID, map[string]any{})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) repositoryGrant(projectID string) (map[string]any, error) {
	var id, authorizationID, repositoryID, name, cloneURL, branch, access, approvedBy, approvedAt, provider, authorizationName string
	err := s.store.DB.QueryRow(`SELECT g.id,g.authorization_id,g.repository_id,g.repository_name,g.clone_url,g.default_branch,g.access_level,g.approved_by,g.approved_at,a.provider,a.name FROM project_repository_grants g JOIN provider_authorizations a ON a.id=g.authorization_id WHERE g.project_id=? AND g.revoked_at IS NULL`, projectID).Scan(&id, &authorizationID, &repositoryID, &name, &cloneURL, &branch, &access, &approvedBy, &approvedAt, &provider, &authorizationName)
	if err != nil {
		return nil, err
	}
	grant := map[string]any{"id": id, "projectId": projectID, "authorizationId": authorizationID, "authorizationName": authorizationName, "provider": provider, "repositoryId": repositoryID, "repositoryName": name, "cloneUrl": cloneURL, "defaultBranch": branch, "accessLevel": access, "approvedBy": approvedBy, "approvedAt": approvedAt}
	var lastSync string
	if s.store.DB.QueryRow("SELECT last_synced_at FROM project_commit_sync_state WHERE project_id=?", projectID).Scan(&lastSync) == nil {
		grant["lastSyncAt"] = lastSync
	}
	return grant, nil
}

func (s *Server) createWorkItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	var in struct {
		RequestID      string   `json:"requestId"`
		ProjectID      string   `json:"projectId"`
		Title          string   `json:"title"`
		Description    string   `json:"descriptionMarkdown"`
		Criteria       string   `json:"acceptanceCriteriaMarkdown"`
		Priority       string   `json:"priority"`
		TargetBranch   string   `json:"targetBranch"`
		AssigneeKind   string   `json:"assigneeKind"`
		AssigneeID     string   `json:"assigneeId"`
		ParentID       string   `json:"parentId"`
		FollowerIDs    []string `json:"followerIds"`
		Stage          string   `json:"stage"`
		DiscussionMode string   `json:"discussionMode"`
		ExecutionMode  string   `json:"executionMode"`
		AcceptanceMode string   `json:"acceptanceMode"`
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
	if !validTaskConfiguration(in.Stage, in.DiscussionMode, in.ExecutionMode, in.AcceptanceMode) {
		writeError(w, &domainError{422, "INVALID_TASK_CONFIGURATION", "Invalid task stage or automatic progression mode", nil})
		return
	}
	item, err := s.queue.Create(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, workqueue.CreateInput{RequestID: in.RequestID, ProjectID: in.ProjectID, Title: in.Title, DescriptionMarkdown: in.Description, AcceptanceCriteriaMarkdown: in.Criteria, Priority: in.Priority, TargetBranch: in.TargetBranch, AssigneeKind: in.AssigneeKind, AssigneeID: in.AssigneeID, ParentID: in.ParentID, CreatedByUserID: a.ID, FollowerIDs: in.FollowerIDs, Stage: in.Stage, DiscussionMode: in.DiscussionMode, ExecutionMode: in.ExecutionMode, AcceptanceMode: in.AcceptanceMode})
	if err != nil {
		writeError(w, err)
		return
	}
	if in.AssigneeKind == "human" && in.AssigneeID != "" {
		s.notifyUsers(item, a, "assigned", "你被指派了任务", item.Title, []string{in.AssigneeID})
	}
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
		ExpectedVersion int64    `json:"expectedVersion"`
		Title           string   `json:"title"`
		Description     string   `json:"descriptionMarkdown"`
		Criteria        string   `json:"acceptanceCriteriaMarkdown"`
		Priority        string   `json:"priority"`
		TargetBranch    string   `json:"targetBranch"`
		Stage           string   `json:"stage"`
		DiscussionMode  string   `json:"discussionMode"`
		ExecutionMode   string   `json:"executionMode"`
		AcceptanceMode  string   `json:"acceptanceMode"`
		AssigneeKind    string   `json:"assigneeKind"`
		AssigneeID      string   `json:"assigneeId"`
		FollowerIDs     []string `json:"followerIds"`
		Configure       bool     `json:"configure"`
	}
	decode(r, &in)
	if in.Configure {
		if !validTaskConfiguration(in.Stage, in.DiscussionMode, in.ExecutionMode, in.AcceptanceMode) {
			writeError(w, &domainError{422, "INVALID_TASK_CONFIGURATION", "Invalid task stage or automatic progression mode", nil})
			return
		}
		if err = s.validateFollowers(item.ProjectID, in.FollowerIDs); err != nil {
			writeError(w, err)
			return
		}
		if in.AssigneeID != "" {
			var exists int
			if in.AssigneeKind == "human" {
				err = s.store.DB.QueryRow("SELECT 1 FROM project_memberships WHERE project_id=? AND user_id=?", item.ProjectID, in.AssigneeID).Scan(&exists)
			} else if in.AssigneeKind == "agent" {
				err = s.store.DB.QueryRow("SELECT 1 FROM agent_project_grants WHERE project_id=? AND agent_id=?", item.ProjectID, in.AssigneeID).Scan(&exists)
			} else {
				err = sql.ErrNoRows
			}
			if err != nil {
				writeError(w, &domainError{422, "INVALID_ASSIGNEE", "Assignee is not enabled for this project", nil})
				return
			}
		}
	}
	item, err = s.queue.Update(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, workqueue.UpdateInput{ExpectedVersion: in.ExpectedVersion, Title: in.Title, Description: in.Description, Criteria: in.Criteria, Priority: in.Priority, TargetBranch: in.TargetBranch, Stage: in.Stage, DiscussionMode: in.DiscussionMode, ExecutionMode: in.ExecutionMode, AcceptanceMode: in.AcceptanceMode, AssigneeKind: in.AssigneeKind, AssigneeID: in.AssigneeID, FollowerIDs: in.FollowerIDs, Configure: in.Configure})
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "work_item_updated", "任务已更新", item.Title, nil)
	writeJSON(w, 200, item)
}

func validTaskConfiguration(stage, discussion, execution, acceptance string) bool {
	allowedStage := map[string]bool{"": true, "todo": true, "discussion": true, "execution": true, "acceptance": true, "completed": true, "order_closed": true}
	allowedDiscussion := map[string]bool{"": true, "manual": true, "auto": true}
	allowedExecution := map[string]bool{"": true, "manual": true, "auto": true}
	allowedAcceptance := map[string]bool{"": true, "human": true, "agent": true, "auto": true}
	return allowedStage[stage] && allowedDiscussion[discussion] && allowedExecution[execution] && allowedAcceptance[acceptance]
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
		err = s.store.DB.QueryRow("SELECT 1 FROM agent_project_grants WHERE project_id=? AND agent_id=?", item.ProjectID, in.AssigneeID).Scan(&exists)
	}
	if err != nil {
		writeError(w, &domainError{422, "INVALID_ASSIGNEE", "Assignee is not enabled for this project", nil})
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
	item, err = s.queue.AddMessage(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, in.Markdown, in.ExpectedVersion)
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
	writeJSON(w, 201, item)
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
	if strings.HasPrefix(mimeType, "image/") || mimeType == "text/markdown" || strings.EqualFold(filepath.Ext(name), ".md") {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, name))
	http.ServeFile(w, r, filePath)
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
	s.notifyRelated(item, a, "blocked_changed", "任务阻塞状态已更新", item.Title, nil)
	writeJSON(w, 201, item)
}
func (s *Server) abandonWorkItem(w http.ResponseWriter, r *http.Request) {
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
	item, err = s.queue.Abandon(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, in.ExpectedVersion, in.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "abandoned", "任务已放弃", item.Title, nil)
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
	writeJSON(w, 200, item)
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
	query := `SELECT id,project_id,number,parent_id,title,description_markdown,acceptance_criteria_markdown,priority,stage,discussion_mode,execution_mode,acceptance_mode,target_branch,assignee_kind,assignee_id,assignment_state,blocked_at,blocked_reason,version,created_at,updated_at,completed_at,created_by_user_id FROM work_items WHERE project_id=?`
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
	out := []workqueue.WorkItem{}
	for rows.Next() {
		var item workqueue.WorkItem
		if err = rows.Scan(&item.ID, &item.ProjectID, &item.Number, &item.ParentID, &item.Title, &item.Description, &item.AcceptanceCriteria, &item.Priority, &item.Stage, &item.DiscussionMode, &item.ExecutionMode, &item.AcceptanceMode, &item.TargetBranch, &item.AssigneeKind, &item.AssigneeID, &item.AssignmentState, &item.BlockedAt, &item.BlockedReason, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.CompletedAt, &item.CreatedByUserID); err == nil {
			out = append(out, item)
		}
	}
	_ = rows.Close()
	for index := range out {
		out[index].FollowerIDs = []string{}
		followerRows, followerErr := s.store.DB.Query("SELECT user_id FROM work_item_followers WHERE work_item_id=? ORDER BY created_at,user_id", out[index].ID)
		if followerErr != nil {
			continue
		}
		for followerRows.Next() {
			var followerID string
			if followerRows.Scan(&followerID) == nil {
				out[index].FollowerIDs = append(out[index].FollowerIDs, followerID)
			}
		}
		_ = followerRows.Close()
	}
	writeJSON(w, 200, out)
}
func (s *Server) freezeConclusion(w http.ResponseWriter, r *http.Request) {
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
		Goal            string `json:"goalMarkdown"`
		Scope           string `json:"scopeMarkdown"`
		OutOfScope      string `json:"outOfScopeMarkdown"`
		Plan            string `json:"implementationPlanMarkdown"`
		Criteria        string `json:"acceptanceCriteriaMarkdown"`
		Risks           string `json:"risksMarkdown"`
	}
	decode(r, &in)
	item, err = s.queue.Freeze(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, workqueue.ConclusionInput{ExpectedVersion: in.ExpectedVersion, Goal: in.Goal, Scope: in.Scope, OutOfScope: in.OutOfScope, Plan: in.Plan, Criteria: in.Criteria, Risks: in.Risks})
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "stage_changed", "任务已进入执行阶段", item.Title, nil)
	writeJSON(w, 201, item)
}
func (s *Server) submitExecution(w http.ResponseWriter, r *http.Request) {
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
		ExpectedVersion     int64                  `json:"expectedVersion"`
		Summary             string                 `json:"summaryMarkdown"`
		Pushed              bool                   `json:"pushed"`
		WorktreeClean       bool                   `json:"worktreeClean"`
		ForbiddenPathsClean bool                   `json:"forbiddenPathsClean"`
		Commits             []string               `json:"commits"`
		ChangedFiles        []string               `json:"changedFiles"`
		Validations         []workqueue.Validation `json:"validations"`
		RemainingRisks      string                 `json:"remainingRisksMarkdown"`
	}
	decode(r, &in)
	item, err = s.queue.SubmitExecution(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, workqueue.ExecutionInput{ExpectedVersion: in.ExpectedVersion, Summary: in.Summary, Pushed: in.Pushed, WorktreeClean: in.WorktreeClean, ForbiddenPathsClean: in.ForbiddenPathsClean, Commits: in.Commits, ChangedFiles: in.ChangedFiles, Validations: in.Validations, RemainingRisks: in.RemainingRisks})
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "stage_changed", "任务已进入验收阶段", item.Title, nil)
	writeJSON(w, 201, item)
}
func (s *Server) submitAcceptance(w http.ResponseWriter, r *http.Request) {
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
		Outcome         string `json:"outcome"`
		Note            string `json:"noteMarkdown"`
		Criteria        any    `json:"criteriaResults"`
	}
	decode(r, &in)
	item, err = s.queue.Accept(r.Context(), workqueue.Actor{Type: a.Type, ID: a.ID}, item.ID, workqueue.AcceptanceInput{ExpectedVersion: in.ExpectedVersion, Outcome: in.Outcome, Note: in.Note, CriteriaResults: in.Criteria})
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyRelated(item, a, "acceptance_result", "任务验收结果已更新", item.Title, nil)
	writeJSON(w, 201, item)
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
	if projectID != "" {
		query += " WHERE project_id=?"
		args = append(args, projectID)
	}
	query += " ORDER BY created_at DESC LIMIT 500"
	writeRows(w, s.store.DB, query, args...)
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
	query := `SELECT n.id,n.project_id,n.work_item_id,n.kind,n.title,n.body,n.actor_type,n.actor_id,n.read_at,n.created_at,w.title AS work_item_title FROM notifications n LEFT JOIN work_items w ON w.id=n.work_item_id WHERE n.user_id=?`
	args := []any{a.ID}
	if projectID := r.URL.Query().Get("projectId"); projectID != "" {
		query += " AND n.project_id=?"
		args = append(args, projectID)
	}
	query += " ORDER BY n.created_at DESC LIMIT 200"
	writeJSON(w, 200, readRows(s.store.DB, query, args...))
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
	_, err := s.store.DB.Exec("UPDATE notifications SET read_at=COALESCE(read_at,?) WHERE user_id=?", now(), a.ID)
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
	if r.URL.Path == "/" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
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
	switch {
	case errors.As(err, &de):
		writeJSON(w, de.Status, map[string]any{"error": map[string]any{"code": de.Code, "message": de.Message, "details": de.Details}})
	case errors.As(err, &we):
		writeJSON(w, we.Status, map[string]any{"error": map[string]any{"code": we.Code, "message": we.Message}})
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
	var id, key, name, description, repo, remote, branch, allowed, validations, forbidden, rules, discussion, execution, acceptance, prompts string
	var archived *string
	var allowAgentExecution, allowAgentAutoClose, allowSubtasks, allowAgentAutoCloseSubtasks int
	var version int64
	if row.Scan(&id, &key, &name, &description, &archived, &repo, &remote, &branch, &allowed, &validations, &forbidden, &rules, &discussion, &execution, &acceptance, &allowAgentExecution, &allowAgentAutoClose, &allowSubtasks, &allowAgentAutoCloseSubtasks, &prompts, &version) != nil {
		return map[string]any{}
	}
	var allowedV, validationsV, forbiddenV any
	promptValues := []string{}
	_ = json.Unmarshal([]byte(allowed), &allowedV)
	_ = json.Unmarshal([]byte(validations), &validationsV)
	_ = json.Unmarshal([]byte(forbidden), &forbiddenV)
	_ = json.Unmarshal([]byte(prompts), &promptValues)
	return map[string]any{"id": id, "key": key, "name": name, "descriptionMarkdown": description, "archivedAt": archived, "repositoryUrl": repo, "remoteName": remote, "defaultTargetBranch": branch, "allowedTargetBranches": allowedV, "validationCommands": validationsV, "forbiddenPaths": forbiddenV, "agentRulesMarkdown": rules, "discussionMode": discussion, "executionMode": execution, "acceptanceMode": acceptance, "allowAgentExecution": allowAgentExecution != 0, "allowAgentAutoClose": allowAgentAutoClose != 0, "allowSubtasks": allowSubtasks != 0, "allowAgentAutoCloseSubtasks": allowAgentAutoCloseSubtasks != 0, "agentPrompts": promptValues, "configVersion": version}
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

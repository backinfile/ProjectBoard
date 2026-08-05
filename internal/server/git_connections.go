package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/providers"
	"github.com/projectboard/projectboard/internal/security"
)

const gitConnectionLifetime = 15 * time.Minute

func (s *Server) gitConnectionConfiguration(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"github": map[string]any{"configured": s.gitClient().Configured("github"), "label": "Connect GitHub"},
		"gitlab": map[string]any{"configured": s.gitClient().Configured("gitlab"), "label": "Connect GitLab", "manualFallback": true},
	})
}

func (s *Server) startGitConnection(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	provider := r.PathValue("provider")
	git := s.gitClient()
	if !git.Configured(provider) {
		writeError(w, &domainError{503, "PROVIDER_NOT_CONFIGURED", "The deployment has not configured this provider", map[string]any{"provider": provider}})
		return
	}
	var in struct {
		ProjectID string `json:"projectId"`
	}
	decode(r, &in)
	if err := s.requireDeveloper(in.ProjectID, a); err != nil {
		writeError(w, err)
		return
	}
	var projectURL string
	if err := s.store.DB.QueryRow("SELECT repository_url FROM projects WHERE id=?", in.ProjectID).Scan(&projectURL); err != nil {
		writeError(w, err)
		return
	}
	flowID, secretPart, noncePart := security.Token(18), security.Token(32), security.Token(24)
	verifier := security.Token(48)
	sealedVerifier, err := s.vault.Seal(verifier)
	if err != nil {
		writeError(w, err)
		return
	}
	challengeDigest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	state := flowID + "." + secretPart + "." + noncePart
	authorizationURL, err := git.AuthorizationURL(provider, state, challenge)
	if err != nil {
		writeError(w, &domainError{503, "PROVIDER_NOT_CONFIGURED", err.Error(), nil})
		return
	}
	stamp := now()
	expires := time.Now().UTC().Add(gitConnectionLifetime).Format(time.RFC3339Nano)
	_, err = s.store.DB.Exec(`INSERT INTO git_connection_flows(id,provider,project_id,user_id,state_hash,nonce_hash,sealed_verifier,status,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'waiting_for_provider',?,?,?)`, flowID, provider, in.ProjectID, a.ID, security.HashOpaque(secretPart), security.HashOpaque(noncePart), sealedVerifier, expires, stamp, stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "git.connection_started", "git_connection", flowID, in.ProjectID, map[string]any{"provider": provider, "expiresAt": expires, "repositoryUrl": projectURL})
	writeJSON(w, 201, map[string]any{"id": flowID, "provider": provider, "status": "waiting_for_provider", "authorizationUrl": authorizationURL, "expiresAt": expires})
}

func (s *Server) gitConnectionCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	flowID, err := s.validateGitState(provider, r.URL.Query().Get("state"))
	if err != nil {
		s.writeGitCallback(w, "Authorization rejected", "The callback state is invalid or expired. Return to ProjectBoard and start again.", false)
		return
	}
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		s.failGitFlow(flowID, "canceled", "AUTHORIZATION_CANCELED", providerError)
		s.writeGitCallback(w, "Authorization canceled", "No repository access was changed. You can close this window and retry from ProjectBoard.", false)
		return
	}
	var result *providers.ConnectionResult
	git := s.gitClient()
	if provider == "github" {
		installationID := r.URL.Query().Get("installation_id")
		if installationID == "" {
			s.failGitFlow(flowID, "canceled", "AUTHORIZATION_CANCELED", "GitHub installation was not completed")
			s.writeGitCallback(w, "Installation not completed", "No repository access was changed. You can close this window and retry.", false)
			return
		}
		result, err = git.CompleteGitHub(r.Context(), installationID)
	} else if provider == "gitlab" {
		var sealedVerifier string
		if scanErr := s.store.DB.QueryRow("SELECT sealed_verifier FROM git_connection_flows WHERE id=?", flowID).Scan(&sealedVerifier); scanErr != nil {
			err = scanErr
		} else {
			var verifier string
			verifier, err = s.vault.Open(sealedVerifier)
			if err == nil {
				result, err = git.CompleteGitLab(r.Context(), r.URL.Query().Get("code"), verifier)
			}
		}
	} else {
		err = fmt.Errorf("unsupported provider")
	}
	if err != nil {
		s.failGitFlow(flowID, "failed", providerErrorCode(err), err.Error())
		s.writeGitCallback(w, "Authorization needs attention", "ProjectBoard could not verify the provider response. Close this window to review the recovery details.", false)
		return
	}
	if err = s.saveConnectedAuthorization(r.Context(), flowID, result); err != nil {
		s.failGitFlow(flowID, "failed", "SAVE_FAILED", err.Error())
		s.writeGitCallback(w, "Authorization could not be saved", "Close this window and retry from ProjectBoard.", false)
		return
	}
	s.auditGitFlow(flowID, "git.connection_callback_verified", map[string]any{"provider": provider, "repositoryCount": len(result.Repositories), "webhookStatus": result.WebhookStatus})
	s.writeGitCallback(w, "Provider connected", "Return to ProjectBoard to review the repository, access level, and final project binding.", true)
}

func (s *Server) validateGitState(provider, state string) (string, error) {
	parts := strings.Split(state, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid state")
	}
	var storedProvider, stateHash, nonceHash, status, expires string
	if err := s.store.DB.QueryRow("SELECT provider,state_hash,nonce_hash,status,expires_at FROM git_connection_flows WHERE id=?", parts[0]).Scan(&storedProvider, &stateHash, &nonceHash, &status, &expires); err != nil {
		return "", err
	}
	if storedProvider != provider || status != "waiting_for_provider" || expires <= now() || subtle.ConstantTimeCompare([]byte(stateHash), []byte(security.HashOpaque(parts[1]))) != 1 || subtle.ConstantTimeCompare([]byte(nonceHash), []byte(security.HashOpaque(parts[2]))) != 1 {
		return "", fmt.Errorf("invalid or expired state")
	}
	return parts[0], nil
}

func (s *Server) saveConnectedAuthorization(ctx context.Context, flowID string, result *providers.ConnectionResult) error {
	credential, err := s.vault.Seal(result.Credential)
	if err != nil {
		return err
	}
	authorizationID, stamp := security.Token(18), now()
	repositories, _ := json.Marshal(result.Repositories)
	permissions, _ := json.Marshal(result.Permissions)
	var projectID, projectURL string
	if err = s.store.DB.QueryRow(`SELECT f.project_id,p.repository_url FROM git_connection_flows f JOIN projects p ON p.id=f.project_id WHERE f.id=?`, flowID).Scan(&projectID, &projectURL); err != nil {
		return err
	}
	matched := matchRepository(projectURL, result.Repositories)
	return s.store.Write(ctx, func(tx *sql.Tx) error {
		if _, err = tx.Exec("INSERT INTO provider_authorizations(id,provider,name,base_url,app_id,installation_id,encrypted_secret,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'active',?,?)", authorizationID, result.Provider, result.Name, result.BaseURL, nullableString(result.AppID), nullableString(result.InstallationID), credential, stamp, stamp); err != nil {
			return err
		}
		credentialKind := result.Provider + "-oauth"
		if result.Provider == "github" {
			credentialKind = "github-app"
		}
		if _, err = tx.Exec("INSERT INTO provider_authorization_metadata(authorization_id,credential_kind,external_account,permissions_json,webhook_status,webhook_url,updated_at) VALUES(?,?,?,?,?,?,?)", authorizationID, credentialKind, nullableString(result.ExternalAccount), string(permissions), result.WebhookStatus, nullableString(result.WebhookURL), stamp); err != nil {
			return err
		}
		for _, repository := range result.Repositories {
			if _, err = tx.Exec("INSERT INTO provider_authorization_repositories(authorization_id,repository_id,repository_name,clone_url,default_branch,web_url,can_write,verified_at) VALUES(?,?,?,?,?,?,?,?)", authorizationID, repository.ID, repository.Name, repository.CloneURL, repository.DefaultBranch, nullableString(repository.WebURL), repository.CanWrite, stamp); err != nil {
				return err
			}
		}
		status, code, message := "ready", "", ""
		if len(result.Repositories) == 0 {
			status, code, message = "failed", "NO_ACCESSIBLE_REPOSITORIES", "The provider returned no repositories available to this installation or account"
		} else if matched == "" {
			code, message = "NO_MATCHING_REPOSITORY", "No repository matched the current project URL; choose one explicitly"
		}
		_, err = tx.Exec("UPDATE git_connection_flows SET status=?,error_code=?,error_message=?,authorization_id=?,repositories_json=?,matched_repository_id=?,permissions_json=?,webhook_status=?,webhook_url=?,updated_at=? WHERE id=?", status, nullableString(code), nullableString(message), authorizationID, string(repositories), nullableString(matched), string(permissions), result.WebhookStatus, nullableString(result.WebhookURL), stamp, flowID)
		return err
	})
}

func (s *Server) authorizationRepositories(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.store.DB.Query("SELECT repository_id,repository_name,clone_url,default_branch,web_url,can_write,verified_at FROM provider_authorization_repositories WHERE authorization_id=? ORDER BY repository_name", r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, cloneURL, branch, verified string
		var webURL *string
		var canWrite bool
		if err = rows.Scan(&id, &name, &cloneURL, &branch, &webURL, &canWrite, &verified); err != nil {
			writeError(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "cloneUrl": cloneURL, "defaultBranch": branch, "webUrl": webURL, "canWrite": canWrite, "verifiedAt": verified})
	}
	writeJSON(w, 200, out)
}

func (s *Server) getGitConnection(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	flow, err := s.gitFlow(r.PathValue("id"), a)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, flow)
}

func (s *Server) gitFlow(id string, a actor) (map[string]any, error) {
	var provider, projectID, userID, status, expires, created, updated string
	var errorCode, errorMessage, authorizationID, repositoriesJSON, matchedID, permissionsJSON, webhookStatus, webhookURL *string
	err := s.store.DB.QueryRow(`SELECT provider,project_id,user_id,status,error_code,error_message,authorization_id,repositories_json,matched_repository_id,permissions_json,webhook_status,webhook_url,expires_at,created_at,updated_at FROM git_connection_flows WHERE id=?`, id).Scan(&provider, &projectID, &userID, &status, &errorCode, &errorMessage, &authorizationID, &repositoriesJSON, &matchedID, &permissionsJSON, &webhookStatus, &webhookURL, &expires, &created, &updated)
	if err != nil {
		return nil, err
	}
	if a.Role != "administrator" && a.ID != userID {
		return nil, &domainError{403, "FORBIDDEN", "Connection flow is not available to this account", nil}
	}
	if status == "waiting_for_provider" && expires <= now() {
		status, errorCode, errorMessage = "expired", stringPointer("CALLBACK_EXPIRED"), stringPointer("The authorization window expired; start a new connection")
		_, _ = s.store.DB.Exec("UPDATE git_connection_flows SET status='expired',error_code='CALLBACK_EXPIRED',error_message=?,updated_at=? WHERE id=?", *errorMessage, now(), id)
	}
	repositories := []providers.Repository{}
	permissions := map[string]any{}
	if repositoriesJSON != nil {
		_ = json.Unmarshal([]byte(*repositoriesJSON), &repositories)
	}
	if permissionsJSON != nil {
		_ = json.Unmarshal([]byte(*permissionsJSON), &permissions)
	}
	return map[string]any{"id": id, "provider": provider, "projectId": projectID, "status": status, "errorCode": errorCode, "errorMessage": errorMessage, "authorizationId": authorizationID, "repositories": repositories, "matchedRepositoryId": matchedID, "permissions": permissions, "webhookStatus": webhookStatus, "webhookUrl": webhookURL, "expiresAt": expires, "createdAt": created, "updatedAt": updated}, nil
}

func (s *Server) completeGitConnection(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	flow, err := s.gitFlow(r.PathValue("id"), a)
	if err != nil {
		writeError(w, err)
		return
	}
	if flow["status"] != "ready" {
		writeError(w, &domainError{409, "CONNECTION_NOT_READY", "Complete provider authorization before binding a repository", flow})
		return
	}
	var in struct {
		RepositoryID string `json:"repositoryId"`
		AccessLevel  string `json:"accessLevel"`
	}
	decode(r, &in)
	if in.AccessLevel != "read" && in.AccessLevel != "write" {
		writeError(w, &domainError{422, "INVALID_ACCESS_LEVEL", "Access level must be read or write", nil})
		return
	}
	repositories := flow["repositories"].([]providers.Repository)
	var selected *providers.Repository
	for index := range repositories {
		if repositories[index].ID == in.RepositoryID {
			selected = &repositories[index]
			break
		}
	}
	if selected == nil {
		writeError(w, &domainError{422, "REPOSITORY_NOT_AUTHORIZED", "Select a repository returned by the connected provider", nil})
		return
	}
	if in.AccessLevel == "write" && !selected.CanWrite {
		writeError(w, &domainError{422, "INSUFFICIENT_PROVIDER_PERMISSION", "The provider account cannot grant write access to this repository", nil})
		return
	}
	webhookStatus := pointerValue(flow["webhookStatus"])
	webhookURL := pointerValue(flow["webhookUrl"])
	if webhookStatus != "on_demand" {
		writeError(w, &domainError{409, "SYNC_MODE_NOT_READY", "Provider authorization must use on-demand commit synchronization", nil})
		return
	}
	grant, err := s.bindConnectedRepository(r.Context(), a, flow["projectId"].(string), pointerValue(flow["authorizationId"]), *selected, in.AccessLevel, webhookStatus, webhookURL)
	if err != nil {
		writeError(w, err)
		return
	}
	_, _ = s.store.DB.Exec("UPDATE git_connection_flows SET status='completed',error_code=NULL,error_message=NULL,webhook_status=?,webhook_url=?,updated_at=? WHERE id=?", webhookStatus, nullableString(webhookURL), now(), r.PathValue("id"))
	writeJSON(w, 201, grant)
}

func (s *Server) bindConnectedRepository(_ context.Context, a actor, projectID, authorizationID string, repository providers.Repository, accessLevel, webhookStatus, webhookURL string) (map[string]any, error) {
	id, stamp := security.Token(18), now()
	_, err := s.store.DB.Exec(`INSERT INTO project_repository_grants(id,project_id,authorization_id,repository_id,repository_name,clone_url,default_branch,access_level,approved_by,approved_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET authorization_id=excluded.authorization_id,repository_id=excluded.repository_id,repository_name=excluded.repository_name,clone_url=excluded.clone_url,default_branch=excluded.default_branch,access_level=excluded.access_level,approved_by=excluded.approved_by,approved_at=excluded.approved_at,revoked_at=NULL`, id, projectID, authorizationID, repository.ID, repository.Name, repository.CloneURL, repository.DefaultBranch, accessLevel, a.ID, stamp)
	if err != nil {
		return nil, err
	}
	_, _ = s.store.DB.Exec("UPDATE provider_authorization_metadata SET webhook_status=?,webhook_url=?,updated_at=? WHERE authorization_id=?", webhookStatus, nullableString(webhookURL), stamp, authorizationID)
	s.audit(a, "project.repository_granted", "repository", repository.ID, projectID, map[string]any{"authorizationId": authorizationID, "accessLevel": accessLevel, "source": "automatic_connection"})
	return s.repositoryGrant(projectID)
}

func (s *Server) cancelGitConnection(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	flow, err := s.gitFlow(r.PathValue("id"), a)
	if err != nil {
		writeError(w, err)
		return
	}
	if flow["status"] == "completed" {
		writeError(w, &domainError{409, "ALREADY_COMPLETED", "Completed connections cannot be canceled", nil})
		return
	}
	s.failGitFlow(r.PathValue("id"), "canceled", "AUTHORIZATION_CANCELED", "Canceled in ProjectBoard")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) failGitFlow(id, status, code, message string) {
	_, _ = s.store.DB.Exec("UPDATE git_connection_flows SET status=?,error_code=?,error_message=?,updated_at=? WHERE id=?", status, code, message, now(), id)
	s.auditGitFlow(id, "git.connection_"+status, map[string]any{"code": code})
}

func (s *Server) auditGitFlow(flowID, eventType string, payload any) {
	var userID, projectID string
	if s.store.DB.QueryRow("SELECT user_id,project_id FROM git_connection_flows WHERE id=?", flowID).Scan(&userID, &projectID) == nil {
		s.audit(actor{Type: "human", ID: userID}, eventType, "git_connection", flowID, projectID, payload)
	}
}

func (s *Server) writeGitCallback(w http.ResponseWriter, title, message string, success bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	state := "Needs attention"
	if success {
		state = "Connected"
	}
	_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s · ProjectBoard</title><link rel="stylesheet" href="/assets/reference-theme.css"></head><body class="git-callback-page"><main class="git-callback-panel"><div class="git-callback-state">%s</div><h1>%s</h1><p>%s</p></main></body></html>`, html.EscapeString(title), state, html.EscapeString(title), html.EscapeString(message))
}

func matchRepository(projectURL string, repositories []providers.Repository) string {
	want := normalizedRepository(projectURL)
	for _, repository := range repositories {
		if normalizedRepository(repository.CloneURL) == want || normalizedRepository(repository.WebURL) == want {
			return repository.ID
		}
	}
	return ""
}

func normalizedRepository(raw string) string {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, ".git"))
	if strings.HasPrefix(raw, "git@") {
		raw = strings.Replace(raw, ":", "/", 1)
		raw = "ssh://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return strings.ToLower(strings.Trim(raw, "/"))
	}
	return strings.ToLower(parsed.Host + "/" + strings.Trim(parsed.Path, "/"))
}

func providerErrorCode(err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "permission") || strings.Contains(message, "scope") {
		return "INSUFFICIENT_PROVIDER_PERMISSION"
	}
	return "PROVIDER_AUTHORIZATION_FAILED"
}

func pointerValue(value any) string {
	if pointer, ok := value.(*string); ok && pointer != nil {
		return *pointer
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func stringPointer(value string) *string { return &value }

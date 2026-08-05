package server

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/projectboard/projectboard/internal/security"
)

var githubOrganizationPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

func (s *Server) startGitHubManifest(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		Organization      string `json:"organization"`
		DetectedPublicURL string `json:"detectedPublicUrl"`
	}
	decode(r, &in)
	publicURL, err := s.systemPublicURL()
	if err == sql.ErrNoRows {
		publicURL, err = validateSystemPublicURL(in.DetectedPublicURL)
		if err == nil {
			stamp := now()
			_, err = s.store.DB.Exec(`INSERT INTO system_settings(key,value,updated_by,updated_at) VALUES(?,?,?,?)`, publicURLSetting, publicURL, a.ID, stamp)
			if err == nil {
				s.audit(a, "system.settings_initialized", "system_settings", publicURLSetting, "", map[string]any{"publicUrl": publicURL})
			}
		}
	}
	if err != nil {
		writeError(w, err)
		return
	}
	organization := strings.TrimSpace(in.Organization)
	if organization != "" && !githubOrganizationPattern.MatchString(organization) {
		writeError(w, &domainError{422, "INVALID_GITHUB_ORGANIZATION", "GitHub organization must be its account slug", nil})
		return
	}
	flowID, secret := security.Token(18), security.Token(24)
	state := flowID + "." + secret
	stamp := now()
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	if _, err = s.store.DB.Exec(`INSERT INTO git_provider_setup_flows(id,provider,user_id,public_url,state_hash,status,expires_at,created_at,updated_at) VALUES(?,'github',?,?,?,'waiting_for_github',?,?,?)`, flowID, a.ID, publicURL, security.HashOpaque(secret), expires, stamp, stamp); err != nil {
		writeError(w, err)
		return
	}
	manifest := map[string]any{
		"name":                     "ProjectBoard-" + security.Token(5),
		"url":                      publicURL,
		"description":              "Project-scoped Git authorization for ProjectBoard",
		"redirect_url":             publicURL + "/api/git/provider-settings/github/manifest/callback",
		"setup_url":                publicURL + "/api/git/connections/github/callback",
		"setup_on_update":          false,
		"public":                   false,
		"request_oauth_on_install": false,
		"default_permissions":      map[string]string{"contents": "write"},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": flowID, "registrationUrl": s.gitClient().GitHubManifestRegistrationURL(organization, state),
		"manifest": string(encoded), "expiresAt": expires,
	})
}

func (s *Server) gitHubManifestCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		s.writeGitManifestCallback(w, "Configuration not accepted", "The GitHub state was missing. Return to ProjectBoard and try again.", false)
		return
	}
	var userID, publicURL, stateHash, status, expires string
	err := s.store.DB.QueryRow(`SELECT user_id,public_url,state_hash,status,expires_at FROM git_provider_setup_flows WHERE id=?`, parts[0]).Scan(&userID, &publicURL, &stateHash, &status, &expires)
	if err != nil || status != "waiting_for_github" || subtle.ConstantTimeCompare([]byte(stateHash), []byte(security.HashOpaque(parts[1]))) != 1 {
		s.writeGitManifestCallback(w, "Configuration not accepted", "The setup request could not be verified. No provider settings were changed.", false)
		return
	}
	a := actor{Type: "human", ID: userID}
	expiry, parseErr := time.Parse(time.RFC3339Nano, expires)
	if parseErr != nil || time.Now().UTC().After(expiry) {
		s.failGitHubManifestFlow(parts[0], "CALLBACK_EXPIRED", "The GitHub App setup expired. Start again from ProjectBoard.")
		s.writeGitManifestCallback(w, "Setup expired", "Return to ProjectBoard and start the one-click setup again.", false)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		s.failGitHubManifestFlow(parts[0], "GITHUB_SETUP_CANCELED", "GitHub did not return an App manifest code.")
		s.writeGitManifestCallback(w, "Setup canceled", "No provider settings were changed. You can close this window and retry.", false)
		return
	}
	conversion, err := s.gitClient().ConvertGitHubManifest(r.Context(), code)
	if err != nil {
		s.failGitHubManifestFlow(parts[0], "MANIFEST_EXCHANGE_FAILED", err.Error())
		s.writeGitManifestCallback(w, "GitHub setup failed", "ProjectBoard could not retrieve the generated App configuration. Close this window and retry.", false)
		return
	}
	stored := storedGitProviderSettings{
		PublicURL: publicURL, GitHubAppID: conversion.AppID, GitHubAppSlug: conversion.AppSlug,
		GitHubPrivateKey: conversion.PrivateKey,
	}
	if _, err = s.saveGitProviderSettings(a, "github", stored); err != nil {
		s.failGitHubManifestFlow(parts[0], "SAVE_CONFIGURATION_FAILED", err.Error())
		s.writeGitManifestCallback(w, "Could not save configuration", "ProjectBoard rejected the generated settings. Close this window and retry.", false)
		return
	}
	_, _ = s.store.DB.Exec(`UPDATE git_provider_setup_flows SET status='completed',error_code=NULL,error_message=NULL,updated_at=? WHERE id=?`, now(), parts[0])
	s.audit(a, "git.manifest_setup_completed", "git_provider_setup_flow", parts[0], "", map[string]any{"provider": "github"})
	s.writeGitManifestCallback(w, "GitHub App configured", "ProjectBoard securely saved the generated App settings. This window can be closed.", true)
}

func (s *Server) getGitHubManifestFlow(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var userID, status, expires string
	var code, message sql.NullString
	err := s.store.DB.QueryRow(`SELECT user_id,status,error_code,error_message,expires_at FROM git_provider_setup_flows WHERE id=?`, r.PathValue("id")).Scan(&userID, &status, &code, &message, &expires)
	if err == sql.ErrNoRows || userID != a.ID {
		writeError(w, &domainError{404, "SETUP_FLOW_NOT_FOUND", "GitHub setup flow not found", nil})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if expiry, parseErr := time.Parse(time.RFC3339Nano, expires); status == "waiting_for_github" && (parseErr != nil || time.Now().UTC().After(expiry)) {
		status = "failed"
		code = sql.NullString{String: "CALLBACK_EXPIRED", Valid: true}
		message = sql.NullString{String: "The GitHub App setup expired. Start again.", Valid: true}
		_, _ = s.store.DB.Exec(`UPDATE git_provider_setup_flows SET status=?,error_code=?,error_message=?,updated_at=? WHERE id=?`, status, code.String, message.String, now(), r.PathValue("id"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "status": status, "errorCode": nullStringValue(code), "errorMessage": nullStringValue(message), "expiresAt": expires})
}

func (s *Server) cancelGitHubManifestFlow(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	result, err := s.store.DB.Exec(`UPDATE git_provider_setup_flows SET status='failed',error_code='GITHUB_SETUP_CANCELED',error_message='The setup was canceled from ProjectBoard.',updated_at=? WHERE id=? AND user_id=? AND status='waiting_for_github'`, now(), r.PathValue("id"), a.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		writeError(w, &domainError{409, "SETUP_FLOW_NOT_ACTIVE", "GitHub setup is no longer waiting", nil})
		return
	}
	s.audit(a, "git.manifest_setup_canceled", "git_provider_setup_flow", r.PathValue("id"), "", map[string]any{"provider": "github"})
	writeJSON(w, http.StatusOK, map[string]any{"status": "failed", "errorCode": "GITHUB_SETUP_CANCELED"})
}

func nullStringValue(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func (s *Server) failGitHubManifestFlow(id, code, message string) {
	_, _ = s.store.DB.Exec(`UPDATE git_provider_setup_flows SET status='failed',error_code=?,error_message=?,updated_at=? WHERE id=?`, code, message, now(), id)
}

func (s *Server) writeGitManifestCallback(w http.ResponseWriter, title, message string, success bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	state := "Needs attention"
	if success {
		state = "Configured"
	}
	_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s · ProjectBoard</title><link rel="stylesheet" href="/assets/reference-theme.css"></head><body class="git-callback-page"><main class="git-callback-panel"><div class="git-callback-state">%s</div><h1>%s</h1><p>%s</p></main></body></html>`, html.EscapeString(title), state, html.EscapeString(title), html.EscapeString(message))
}

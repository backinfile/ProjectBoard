package server

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
)

const publicURLSetting = "public_url"

func (s *Server) systemPublicURL() (string, error) {
	var value string
	err := s.store.DB.QueryRow(`SELECT value FROM system_settings WHERE key=?`, publicURLSetting).Scan(&value)
	return value, err
}

func validateSystemPublicURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", &domainError{422, "INVALID_PUBLIC_URL", "Enter the full ProjectBoard origin, without a path, query, or fragment", nil}
	}
	host := strings.ToLower(parsed.Hostname())
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if !loopback && parsed.Scheme != "https" {
		return "", &domainError{422, "PUBLIC_HTTPS_REQUIRED", "Non-local ProjectBoard addresses must use HTTPS", nil}
	}
	return value, nil
}

func (s *Server) getSystemSettings(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	value, err := s.systemPublicURL()
	if err == sql.ErrNoRows {
		value = ""
	} else if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicUrl": value, "gitSyncMode": "on_demand"})
}

func (s *Server) updateSystemSettings(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		PublicURL string `json:"publicUrl"`
	}
	decode(r, &in)
	value, err := validateSystemPublicURL(in.PublicURL)
	if err != nil {
		writeError(w, err)
		return
	}
	stamp := now()
	if _, err = s.store.DB.Exec(`INSERT INTO system_settings(key,value,updated_by,updated_at) VALUES(?,?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, publicURLSetting, value, a.ID, stamp); err != nil {
		writeError(w, err)
		return
	}
	s.audit(a, "system.settings_updated", "system_settings", publicURLSetting, "", map[string]any{"publicUrl": value, "gitSyncMode": "on_demand"})
	writeJSON(w, http.StatusOK, map[string]any{"publicUrl": value, "gitSyncMode": "on_demand", "updatedAt": stamp})
}

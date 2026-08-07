package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/projectboard/projectboard/internal/providers"
)

type storedGitProviderSettings struct {
	PublicURL        string `json:"publicUrl"`
	GitHubAppID      string `json:"githubAppId,omitempty"`
	GitHubAppSlug    string `json:"githubAppSlug,omitempty"`
	GitHubPrivateKey string `json:"githubPrivateKey,omitempty"`
}

func (s *Server) gitClient() *providers.GitConnector {
	s.gitMu.RLock()
	defer s.gitMu.RUnlock()
	return s.git
}

func (s *Server) reloadGitConnector() error {
	config := s.config.GitConnect
	rows, err := s.store.DB.Query("SELECT provider,encrypted_config FROM git_provider_settings")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var provider, encrypted string
		if err = rows.Scan(&provider, &encrypted); err != nil {
			return err
		}
		plain, openErr := s.vault.Open(encrypted)
		if openErr != nil {
			return openErr
		}
		var stored storedGitProviderSettings
		if err = json.Unmarshal([]byte(plain), &stored); err != nil {
			return err
		}
		applyStoredGitSettings(&config, provider, stored)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	connector := providers.NewGitConnector(config)
	s.gitMu.Lock()
	s.git = connector
	s.gitMu.Unlock()
	return nil
}

func applyStoredGitSettings(config *providers.GitConnectConfig, provider string, stored storedGitProviderSettings) {
	if provider == "github" {
		config.PublicURL = stored.PublicURL
		config.GitHubAppID = stored.GitHubAppID
		config.GitHubAppSlug = stored.GitHubAppSlug
		config.GitHubPrivateKey = stored.GitHubPrivateKey
	}
}

func (s *Server) readStoredGitSettings(provider string) (storedGitProviderSettings, string, error) {
	var encrypted, updatedAt string
	err := s.store.DB.QueryRow("SELECT encrypted_config,updated_at FROM git_provider_settings WHERE provider=?", provider).Scan(&encrypted, &updatedAt)
	if err != nil {
		return storedGitProviderSettings{}, "", err
	}
	plain, err := s.vault.Open(encrypted)
	if err != nil {
		return storedGitProviderSettings{}, "", err
	}
	var stored storedGitProviderSettings
	if err = json.Unmarshal([]byte(plain), &stored); err != nil {
		return storedGitProviderSettings{}, "", err
	}
	return stored, updatedAt, nil
}

func gitSettingsSummary(provider string, stored storedGitProviderSettings, updatedAt string) map[string]any {
	configured := stored.PublicURL != "" && stored.GitHubAppID != "" && stored.GitHubAppSlug != "" && stored.GitHubPrivateKey != ""
	return map[string]any{
		"provider": provider, "configured": configured, "publicUrl": stored.PublicURL,
		"appId": stored.GitHubAppID, "appSlug": stored.GitHubAppSlug,
		"privateKeyConfigured": stored.GitHubPrivateKey != "", "syncMode": "on_demand", "updatedAt": updatedAt,
	}
}

func (s *Server) getGitProviderSettings(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	stored, updatedAt, err := s.readStoredGitSettings("github")
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, map[string]any{"github": gitSettingsSummary("github", storedGitProviderSettings{}, "")})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"github": gitSettingsSummary("github", stored, updatedAt)})
}

func (s *Server) updateGitProviderSettings(w http.ResponseWriter, r *http.Request) {
	a, ok := s.human(w, r)
	if !ok {
		return
	}
	if err := requireAdmin(a); err != nil {
		writeError(w, err)
		return
	}
	provider := r.PathValue("provider")
	if provider != "github" {
		writeError(w, &domainError{404, "PROVIDER_NOT_FOUND", "Unknown Git provider", nil})
		return
	}
	previous, _, err := s.readStoredGitSettings(provider)
	if err != nil && err != sql.ErrNoRows {
		writeError(w, err)
		return
	}
	var in struct {
		PublicURL  string `json:"publicUrl"`
		AppID      string `json:"appId"`
		AppSlug    string `json:"appSlug"`
		PrivateKey string `json:"privateKey"`
	}
	decode(r, &in)
	publicURL, err := s.systemPublicURL()
	if err == sql.ErrNoRows {
		writeError(w, &domainError{409, "GLOBAL_PUBLIC_URL_REQUIRED", "Configure the ProjectBoard address in System settings first", nil})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	stored := storedGitProviderSettings{PublicURL: publicURL}
	stored.GitHubAppID = strings.TrimSpace(in.AppID)
	stored.GitHubAppSlug = strings.TrimSpace(in.AppSlug)
	stored.GitHubPrivateKey = strings.TrimSpace(in.PrivateKey)
	if stored.GitHubPrivateKey == "" {
		stored.GitHubPrivateKey = previous.GitHubPrivateKey
	}
	if err = validateStoredGitProviderSettings(provider, stored); err != nil {
		writeError(w, &domainError{422, "INVALID_PROVIDER_CONFIGURATION", err.Error(), nil})
		return
	}
	stamp, err := s.saveGitProviderSettings(a, provider, stored)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gitSettingsSummary(provider, stored, stamp))
}

func validateStoredGitProviderSettings(provider string, stored storedGitProviderSettings) error {
	config := providers.GitConnectConfig{}
	applyStoredGitSettings(&config, provider, stored)
	return providers.NewGitConnector(config).ValidateConfiguration(provider)
}

func (s *Server) saveGitProviderSettings(a actor, provider string, stored storedGitProviderSettings) (string, error) {
	if err := validateStoredGitProviderSettings(provider, stored); err != nil {
		return "", err
	}
	plain, err := json.Marshal(stored)
	if err != nil {
		return "", err
	}
	encrypted, err := s.vault.Seal(string(plain))
	if err != nil {
		return "", err
	}
	stamp := now()
	_, err = s.store.DB.Exec(`INSERT INTO git_provider_settings(provider,encrypted_config,updated_by,updated_at) VALUES(?,?,?,?) ON CONFLICT(provider) DO UPDATE SET encrypted_config=excluded.encrypted_config,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, provider, encrypted, a.ID, stamp)
	if err != nil {
		return "", err
	}
	if err = s.reloadGitConnector(); err != nil {
		return "", fmt.Errorf("reload provider configuration: %w", err)
	}
	s.audit(a, "git.provider_settings_updated", "git_provider_settings", provider, "", map[string]any{"provider": provider})
	return stamp, nil
}

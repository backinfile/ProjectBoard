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
	PublicURL           string `json:"publicUrl"`
	GitHubAppID         string `json:"githubAppId,omitempty"`
	GitHubAppSlug       string `json:"githubAppSlug,omitempty"`
	GitHubPrivateKey    string `json:"githubPrivateKey,omitempty"`
	GitHubWebhookURL    string `json:"githubWebhookUrl,omitempty"`
	GitHubWebhookSecret string `json:"githubWebhookSecret,omitempty"`
	GitLabClientID      string `json:"gitlabClientId,omitempty"`
	GitLabClientSecret  string `json:"gitlabClientSecret,omitempty"`
	GitLabBaseURL       string `json:"gitlabBaseUrl,omitempty"`
	GitLabWebhookSecret string `json:"gitlabWebhookSecret,omitempty"`
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
		config.GitHubWebhookURL = stored.GitHubWebhookURL
		config.GitHubWebhookSecret = stored.GitHubWebhookSecret
	}
	if provider == "gitlab" {
		config.PublicURL = stored.PublicURL
		config.GitLabClientID = stored.GitLabClientID
		config.GitLabClientSecret = stored.GitLabClientSecret
		config.GitLabBaseURL = stored.GitLabBaseURL
		config.GitLabWebhookSecret = stored.GitLabWebhookSecret
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
	if provider == "github" {
		configured := stored.PublicURL != "" && stored.GitHubAppID != "" && stored.GitHubAppSlug != "" &&
			stored.GitHubPrivateKey != "" && stored.GitHubWebhookURL != "" && stored.GitHubWebhookSecret != ""
		return map[string]any{
			"provider": provider, "configured": configured, "publicUrl": stored.PublicURL,
			"appId": stored.GitHubAppID, "appSlug": stored.GitHubAppSlug, "webhookUrl": stored.GitHubWebhookURL,
			"privateKeyConfigured": stored.GitHubPrivateKey != "", "webhookSecretConfigured": stored.GitHubWebhookSecret != "", "updatedAt": updatedAt,
		}
	}
	configured := stored.PublicURL != "" && stored.GitLabClientID != "" && stored.GitLabClientSecret != "" &&
		stored.GitLabBaseURL != "" && stored.GitLabWebhookSecret != ""
	return map[string]any{
		"provider": provider, "configured": configured, "publicUrl": stored.PublicURL,
		"clientId": stored.GitLabClientID, "baseUrl": stored.GitLabBaseURL,
		"clientSecretConfigured": stored.GitLabClientSecret != "", "webhookSecretConfigured": stored.GitLabWebhookSecret != "", "updatedAt": updatedAt,
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
	result := map[string]any{}
	for _, provider := range []string{"github", "gitlab"} {
		stored, updatedAt, err := s.readStoredGitSettings(provider)
		if err == sql.ErrNoRows {
			result[provider] = gitSettingsSummary(provider, storedGitProviderSettings{}, "")
			continue
		}
		if err != nil {
			writeError(w, err)
			return
		}
		result[provider] = gitSettingsSummary(provider, stored, updatedAt)
	}
	writeJSON(w, http.StatusOK, result)
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
	if provider != "github" && provider != "gitlab" {
		writeError(w, &domainError{404, "PROVIDER_NOT_FOUND", "Unknown Git provider", nil})
		return
	}
	previous, _, err := s.readStoredGitSettings(provider)
	if err != nil && err != sql.ErrNoRows {
		writeError(w, err)
		return
	}
	var in struct {
		PublicURL     string `json:"publicUrl"`
		AppID         string `json:"appId"`
		AppSlug       string `json:"appSlug"`
		PrivateKey    string `json:"privateKey"`
		WebhookURL    string `json:"webhookUrl"`
		WebhookSecret string `json:"webhookSecret"`
		ClientID      string `json:"clientId"`
		ClientSecret  string `json:"clientSecret"`
		BaseURL       string `json:"baseUrl"`
	}
	decode(r, &in)
	stored := storedGitProviderSettings{PublicURL: strings.TrimRight(strings.TrimSpace(in.PublicURL), "/")}
	otherProvider := "gitlab"
	if provider == "gitlab" {
		otherProvider = "github"
	}
	other, _, otherErr := s.readStoredGitSettings(otherProvider)
	if otherErr != nil && otherErr != sql.ErrNoRows {
		writeError(w, otherErr)
		return
	}
	if otherErr == nil && other.PublicURL != "" && stored.PublicURL != other.PublicURL {
		writeError(w, &domainError{422, "PUBLIC_URL_MISMATCH", "GitHub and GitLab must use the same public ProjectBoard URL", nil})
		return
	}
	if provider == "github" {
		stored.GitHubAppID = strings.TrimSpace(in.AppID)
		stored.GitHubAppSlug = strings.TrimSpace(in.AppSlug)
		stored.GitHubPrivateKey = strings.TrimSpace(in.PrivateKey)
		stored.GitHubWebhookSecret = strings.TrimSpace(in.WebhookSecret)
		if stored.GitHubPrivateKey == "" {
			stored.GitHubPrivateKey = previous.GitHubPrivateKey
		}
		if stored.GitHubWebhookSecret == "" {
			stored.GitHubWebhookSecret = previous.GitHubWebhookSecret
		}
		stored.GitHubWebhookURL = strings.TrimSpace(in.WebhookURL)
		if stored.GitHubWebhookURL == "" && stored.PublicURL != "" {
			stored.GitHubWebhookURL = stored.PublicURL + "/api/git/webhooks/github"
		}
	} else {
		stored.GitLabClientID = strings.TrimSpace(in.ClientID)
		stored.GitLabClientSecret = strings.TrimSpace(in.ClientSecret)
		stored.GitLabWebhookSecret = strings.TrimSpace(in.WebhookSecret)
		if stored.GitLabClientSecret == "" {
			stored.GitLabClientSecret = previous.GitLabClientSecret
		}
		if stored.GitLabWebhookSecret == "" {
			stored.GitLabWebhookSecret = previous.GitLabWebhookSecret
		}
		stored.GitLabBaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		if stored.GitLabBaseURL == "" {
			stored.GitLabBaseURL = "https://gitlab.com"
		}
	}
	config := providers.GitConnectConfig{}
	applyStoredGitSettings(&config, provider, stored)
	connector := providers.NewGitConnector(config)
	if err = connector.ValidateConfiguration(provider); err != nil {
		writeError(w, &domainError{422, "INVALID_PROVIDER_CONFIGURATION", err.Error(), nil})
		return
	}
	plain, err := json.Marshal(stored)
	if err != nil {
		writeError(w, err)
		return
	}
	encrypted, err := s.vault.Seal(string(plain))
	if err != nil {
		writeError(w, err)
		return
	}
	stamp := now()
	_, err = s.store.DB.Exec(`INSERT INTO git_provider_settings(provider,encrypted_config,updated_by,updated_at) VALUES(?,?,?,?) ON CONFLICT(provider) DO UPDATE SET encrypted_config=excluded.encrypted_config,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, provider, encrypted, a.ID, stamp)
	if err != nil {
		writeError(w, err)
		return
	}
	if err = s.reloadGitConnector(); err != nil {
		writeError(w, fmt.Errorf("reload provider configuration: %w", err))
		return
	}
	s.audit(a, "git.provider_settings_updated", "git_provider_settings", provider, "", map[string]any{"provider": provider})
	writeJSON(w, http.StatusOK, gitSettingsSummary(provider, stored, stamp))
}

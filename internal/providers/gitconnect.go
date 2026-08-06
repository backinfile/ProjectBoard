package providers

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type GitConnectConfig struct {
	PublicURL          string
	GitHubAppID        string
	GitHubAppSlug      string
	GitHubPrivateKey   string
	GitHubAPIURL       string
	GitHubWebURL       string
	GitLabClientID     string
	GitLabClientSecret string
	GitLabBaseURL      string
	HTTPClient         *http.Client
}

type Repository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CloneURL      string `json:"cloneUrl"`
	DefaultBranch string `json:"defaultBranch"`
	WebURL        string `json:"webUrl"`
	CanWrite      bool   `json:"canWrite"`
}

type RecentCommit struct {
	SHA         string `json:"sha"`
	Message     string `json:"message"`
	Author      string `json:"author"`
	WebURL      string `json:"webUrl"`
	CommittedAt string `json:"committedAt"`
}

type ConnectionResult struct {
	Provider          string
	Name              string
	BaseURL           string
	AppID             string
	InstallationID    string
	Credential        string
	ExternalAccount   string
	Permissions       map[string]any
	WebhookStatus     string
	WebhookURL        string
	Repositories      []Repository
	GitLabAccessToken string
}

type GitHubManifestConversion struct {
	AppID         string
	AppSlug       string
	PrivateKey    string
	WebhookSecret string
}

type GitConnector struct {
	config GitConnectConfig
	client *http.Client
}

func NewGitConnector(config GitConnectConfig) *GitConnector {
	if config.GitHubAPIURL == "" {
		config.GitHubAPIURL = "https://api.github.com"
	}
	if config.GitHubWebURL == "" {
		config.GitHubWebURL = "https://github.com"
	}
	if config.GitLabBaseURL == "" {
		config.GitLabBaseURL = "https://gitlab.com"
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &GitConnector{config: config, client: config.HTTPClient}
}

func (c *GitConnector) Configured(provider string) bool {
	switch provider {
	case "github":
		return c.config.PublicURL != "" && c.config.GitHubAppID != "" && c.config.GitHubAppSlug != "" && c.config.GitHubPrivateKey != ""
	case "gitlab":
		return c.config.PublicURL != "" && c.config.GitLabClientID != "" && c.config.GitLabClientSecret != ""
	default:
		return false
	}
}

func (c *GitConnector) ValidateConfiguration(provider string) error {
	if !c.Configured(provider) {
		return fmt.Errorf("all required %s settings must be provided", provider)
	}
	for label, value := range map[string]string{"public URL": c.config.PublicURL} {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%s must be an absolute HTTP or HTTPS URL", label)
		}
	}
	if provider == "github" {
		if _, err := c.githubJWT(); err != nil {
			return err
		}
	}
	if provider == "gitlab" {
		parsed, err := url.Parse(c.config.GitLabBaseURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("GitLab base URL must be an absolute HTTP or HTTPS URL")
		}
	}
	return nil
}

func (c *GitConnector) AuthorizationURL(provider, state, challenge string) (string, error) {
	if !c.Configured(provider) {
		return "", fmt.Errorf("%s provider is not configured", provider)
	}
	switch provider {
	case "github":
		return strings.TrimRight(c.config.GitHubWebURL, "/") + "/apps/" + url.PathEscape(c.config.GitHubAppSlug) + "/installations/new?state=" + url.QueryEscape(state), nil
	case "gitlab":
		query := url.Values{
			"client_id":             {c.config.GitLabClientID},
			"redirect_uri":          {c.callbackURL("gitlab")},
			"response_type":         {"code"},
			"scope":                 {"api"},
			"state":                 {state},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
		}
		return strings.TrimRight(c.config.GitLabBaseURL, "/") + "/oauth/authorize?" + query.Encode(), nil
	}
	return "", fmt.Errorf("unsupported provider")
}

func (c *GitConnector) GitHubManifestRegistrationURL(organization, state string) string {
	base := strings.TrimRight(c.config.GitHubWebURL, "/")
	if organization == "" {
		return base + "/settings/apps/new?state=" + url.QueryEscape(state)
	}
	return base + "/organizations/" + url.PathEscape(organization) + "/settings/apps/new?state=" + url.QueryEscape(state)
}

func (c *GitConnector) ConvertGitHubManifest(ctx context.Context, code string) (*GitHubManifestConversion, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.config.GitHubAPIURL, "/")+"/app-manifests/"+url.PathEscape(code)+"/conversions", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	var result struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		PEM           string `json:"pem"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err = c.doJSON(request, &result); err != nil {
		return nil, fmt.Errorf("convert GitHub App manifest: %w", err)
	}
	if result.ID == 0 || result.Slug == "" || result.PEM == "" {
		return nil, fmt.Errorf("GitHub returned an incomplete App manifest conversion")
	}
	return &GitHubManifestConversion{
		AppID: strconv.FormatInt(result.ID, 10), AppSlug: result.Slug,
		PrivateKey: result.PEM, WebhookSecret: result.WebhookSecret,
	}, nil
}

func (c *GitConnector) CompleteGitHub(ctx context.Context, installationID string) (*ConnectionResult, error) {
	jwt, err := c.githubJWT()
	if err != nil {
		return nil, err
	}
	var installation struct {
		ID          int64          `json:"id"`
		Permissions map[string]any `json:"permissions"`
		Account     struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if err = c.githubJSON(ctx, http.MethodGet, "/app/installations/"+url.PathEscape(installationID), jwt, nil, &installation); err != nil {
		return nil, fmt.Errorf("verify installation: %w", err)
	}
	contents, _ := installation.Permissions["contents"].(string)
	if contents != "write" {
		return nil, fmt.Errorf("installation needs Contents: read and write permission")
	}
	var tokenResult struct {
		Token string `json:"token"`
	}
	body := map[string]any{"permissions": map[string]string{"contents": "write"}}
	if err = c.githubJSON(ctx, http.MethodPost, "/app/installations/"+url.PathEscape(installationID)+"/access_tokens", jwt, body, &tokenResult); err != nil {
		return nil, fmt.Errorf("create installation token: %w", err)
	}
	if tokenResult.Token == "" {
		return nil, fmt.Errorf("GitHub returned an empty installation token")
	}
	var repositories struct {
		Repositories []struct {
			ID            int64  `json:"id"`
			FullName      string `json:"full_name"`
			CloneURL      string `json:"clone_url"`
			HTMLURL       string `json:"html_url"`
			DefaultBranch string `json:"default_branch"`
		} `json:"repositories"`
	}
	if err = c.githubJSON(ctx, http.MethodGet, "/installation/repositories?per_page=100", tokenResult.Token, nil, &repositories); err != nil {
		return nil, fmt.Errorf("list installation repositories: %w", err)
	}
	repos := make([]Repository, 0, len(repositories.Repositories))
	for _, repository := range repositories.Repositories {
		repos = append(repos, Repository{ID: strconv.FormatInt(repository.ID, 10), Name: repository.FullName, CloneURL: repository.CloneURL, DefaultBranch: repository.DefaultBranch, WebURL: repository.HTMLURL, CanWrite: true})
	}
	return &ConnectionResult{Provider: "github", Name: "GitHub / " + installation.Account.Login, BaseURL: c.config.GitHubAPIURL, AppID: c.config.GitHubAppID, InstallationID: strconv.FormatInt(installation.ID, 10), Credential: "deployment-managed", ExternalAccount: installation.Account.Login, Permissions: installation.Permissions, WebhookStatus: "on_demand", Repositories: repos}, nil
}

func (c *GitConnector) CompleteGitLab(ctx context.Context, code, verifier string) (*ConnectionResult, error) {
	form := url.Values{
		"client_id":     {c.config.GitLabClientID},
		"client_secret": {c.config.GitLabClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {c.callbackURL("gitlab")},
		"code_verifier": {verifier},
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.config.GitLabBaseURL, "/")+"/oauth/token", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := c.doJSON(request, &token); err != nil {
		return nil, fmt.Errorf("exchange OAuth code: %w", err)
	}
	if token.AccessToken == "" || !strings.Contains(" "+token.Scope+" ", " api ") {
		return nil, fmt.Errorf("GitLab OAuth grant is missing the api scope")
	}
	var user struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	}
	if err := c.gitlabJSON(ctx, http.MethodGet, "/api/v4/user", token.AccessToken, nil, &user); err != nil {
		return nil, fmt.Errorf("verify GitLab identity: %w", err)
	}
	var projects []struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURLToRepo     string `json:"http_url_to_repo"`
		WebURL            string `json:"web_url"`
		DefaultBranch     string `json:"default_branch"`
		Permissions       struct {
			ProjectAccess *struct {
				AccessLevel int `json:"access_level"`
			} `json:"project_access"`
			GroupAccess *struct {
				AccessLevel int `json:"access_level"`
			} `json:"group_access"`
		} `json:"permissions"`
	}
	if err := c.gitlabJSON(ctx, http.MethodGet, "/api/v4/projects?membership=true&per_page=100", token.AccessToken, nil, &projects); err != nil {
		return nil, fmt.Errorf("list GitLab projects: %w", err)
	}
	repos := make([]Repository, 0, len(projects))
	for _, project := range projects {
		level := 0
		if project.Permissions.ProjectAccess != nil {
			level = project.Permissions.ProjectAccess.AccessLevel
		}
		if project.Permissions.GroupAccess != nil && project.Permissions.GroupAccess.AccessLevel > level {
			level = project.Permissions.GroupAccess.AccessLevel
		}
		repos = append(repos, Repository{ID: strconv.FormatInt(project.ID, 10), Name: project.PathWithNamespace, CloneURL: project.HTTPURLToRepo, DefaultBranch: project.DefaultBranch, WebURL: project.WebURL, CanWrite: level >= 30})
	}
	credential, _ := json.Marshal(map[string]any{"accessToken": token.AccessToken, "refreshToken": token.RefreshToken, "tokenType": token.TokenType, "scope": token.Scope, "expiresAt": time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339Nano)})
	return &ConnectionResult{Provider: "gitlab", Name: "GitLab / " + user.Username, BaseURL: c.config.GitLabBaseURL, Credential: string(credential), ExternalAccount: user.Username, Permissions: map[string]any{"scope": token.Scope}, WebhookStatus: "on_demand", Repositories: repos, GitLabAccessToken: token.AccessToken}, nil
}

func (c *GitConnector) RecentGitHubCommits(ctx context.Context, installationID, repositoryID, branch, since, until string) ([]RecentCommit, error) {
	jwt, err := c.githubJWT()
	if err != nil {
		return nil, err
	}
	var tokenResult struct {
		Token string `json:"token"`
	}
	repositoryNumber, err := strconv.ParseInt(repositoryID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid GitHub repository ID")
	}
	body := map[string]any{"repository_ids": []int64{repositoryNumber}, "permissions": map[string]string{"contents": "read"}}
	if err = c.githubJSON(ctx, http.MethodPost, "/app/installations/"+url.PathEscape(installationID)+"/access_tokens", jwt, body, &tokenResult); err != nil {
		return nil, fmt.Errorf("create read-only installation token: %w", err)
	}
	query := url.Values{"per_page": {"100"}}
	if branch != "" {
		query.Set("sha", branch)
	}
	if since != "" {
		query.Set("since", since)
	}
	if until != "" {
		query.Set("until", until)
	}
	var response []struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err = c.githubJSON(ctx, http.MethodGet, "/repositories/"+url.PathEscape(repositoryID)+"/commits?"+query.Encode(), tokenResult.Token, nil, &response); err != nil {
		if isGitHubEmptyRepositoryError(err) {
			return []RecentCommit{}, nil
		}
		return nil, fmt.Errorf("query recent GitHub commits: %w", err)
	}
	commits := make([]RecentCommit, 0, len(response))
	for _, item := range response {
		commits = append(commits, RecentCommit{SHA: item.SHA, Message: item.Commit.Message, Author: item.Commit.Author.Name, WebURL: item.HTMLURL, CommittedAt: item.Commit.Author.Date})
	}
	return commits, nil
}

func (c *GitConnector) RecentGitLabCommits(ctx context.Context, accessToken, repositoryID, branch, since, until string) ([]RecentCommit, error) {
	query := url.Values{"per_page": {"100"}}
	if branch != "" {
		query.Set("ref_name", branch)
	}
	if since != "" {
		query.Set("since", since)
	}
	if until != "" {
		query.Set("until", until)
	}
	var response []struct {
		ID            string `json:"id"`
		Message       string `json:"message"`
		AuthorName    string `json:"author_name"`
		WebURL        string `json:"web_url"`
		CommittedDate string `json:"committed_date"`
	}
	if err := c.gitlabJSON(ctx, http.MethodGet, "/api/v4/projects/"+url.PathEscape(repositoryID)+"/repository/commits?"+query.Encode(), accessToken, nil, &response); err != nil {
		return nil, fmt.Errorf("query recent GitLab commits: %w", err)
	}
	commits := make([]RecentCommit, 0, len(response))
	for _, item := range response {
		commits = append(commits, RecentCommit{SHA: item.ID, Message: item.Message, Author: item.AuthorName, WebURL: item.WebURL, CommittedAt: item.CommittedDate})
	}
	return commits, nil
}

func (c *GitConnector) callbackURL(provider string) string {
	return strings.TrimRight(c.config.PublicURL, "/") + "/api/git/connections/" + provider + "/callback"
}

func (c *GitConnector) githubJWT() (string, error) {
	block, _ := pem.Decode([]byte(c.config.GitHubPrivateKey))
	if block == nil {
		return "", fmt.Errorf("invalid GitHub App private key")
	}
	var key *rsa.PrivateKey
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	} else if key, err = x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		return "", fmt.Errorf("parse GitHub App private key: %w", err)
	}
	if key == nil {
		return "", fmt.Errorf("GitHub App key is not RSA")
	}
	now := time.Now().Unix()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iat": now - 30, "exp": now + 540, "iss": c.config.GitHubAppID})
	input := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (c *GitConnector) githubJSON(ctx context.Context, method, apiPath, token string, body any, out any) error {
	request, err := jsonRequest(ctx, method, strings.TrimRight(c.config.GitHubAPIURL, "/")+apiPath, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return c.doJSON(request, out)
}

func (c *GitConnector) gitlabJSON(ctx context.Context, method, apiPath, token string, body any, out any) error {
	request, err := jsonRequest(ctx, method, strings.TrimRight(c.config.GitLabBaseURL, "/")+apiPath, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return c.doJSON(request, out)
}

func (c *GitConnector) doJSON(request *http.Request, out any) error {
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &providerHTTPError{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

type providerHTTPError struct {
	StatusCode int
	Body       string
}

func (e *providerHTTPError) Error() string {
	return fmt.Sprintf("provider returned %d: %s", e.StatusCode, e.Body)
}

func isGitHubEmptyRepositoryError(err error) bool {
	var providerError *providerHTTPError
	if !errors.As(err, &providerError) || providerError.StatusCode != http.StatusConflict {
		return false
	}
	var payload struct {
		Message string `json:"message"`
	}
	return json.Unmarshal([]byte(providerError.Body), &payload) == nil && strings.EqualFold(strings.TrimSpace(payload.Message), "Git Repository is empty.")
}

func jsonRequest(ctx context.Context, method, target string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err == nil && body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, err
}

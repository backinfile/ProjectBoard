package server_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectboard/projectboard/internal/providers"
	"github.com/projectboard/projectboard/internal/server"
)

func TestServerPublishesHealthAndStaticWorkspace(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()

	for _, check := range []struct {
		path string
		want string
	}{
		{path: "/health", want: `"status":"ok"`},
		{path: "/", want: "ProjectBoard"},
	} {
		response, err := http.Get(host.URL + check.path)
		if err != nil {
			t.Fatalf("GET %s: %v", check.path, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || !strings.Contains(string(body), check.want) {
			t.Fatalf("GET %s = %d %q, want 200 containing %q", check.path, response.StatusCode, body, check.want)
		}
	}
}

func TestRunnerDownloadsAreDiscoveredAndServedToSignedInUsers(t *testing.T) {
	downloadDir := t.TempDir()
	runnerPath := filepath.Join(downloadDir, "projectboard-runner-linux-amd64")
	if err := os.WriteFile(runnerPath, []byte("runner-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	handler, err := server.New(server.Config{DataDir: t.TempDir(), RunnerDownloadDir: downloadDir, BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123"})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	listed := requestJSON(t, client, http.MethodGet, host.URL+"/api/runner/downloads", "", nil)
	downloads, ok := listed["downloads"].([]any)
	if !ok || len(downloads) != 1 {
		t.Fatalf("downloads=%#v, want one", listed["downloads"])
	}
	artifact := downloads[0].(map[string]any)
	if artifact["platform"] != "Linux" || artifact["arch"] != "x64" {
		t.Fatalf("artifact=%#v", artifact)
	}
	response, err := client.Get(host.URL + artifact["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || string(body) != "runner-binary" {
		t.Fatalf("download=%d %q", response.StatusCode, body)
	}
}

func TestHumanCanCompleteTheRequiredWorkItemFlow(t *testing.T) {
	handler, err := server.New(server.Config{
		DataDir:           t.TempDir(),
		BootstrapUsername: "admin",
		BootstrapPassword: "StrongPassword123",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{
		"username": "admin", "password": "StrongPassword123",
	})
	csrf := login["csrfToken"].(string)
	me := login["user"].(map[string]any)

	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{
		"key": "core", "name": "Core", "repositoryUrl": "https://github.com/acme/core.git",
	})
	item := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{
		"requestId": "create-work-001", "projectId": project["id"], "title": "Ship Go rewrite",
		"descriptionMarkdown": "Implement the agreed scope.", "acceptanceCriteriaMarkdown": "All checks pass.",
		"assigneeKind": "human", "assigneeId": me["id"],
	})
	if item["stage"] != "discussion" {
		t.Fatalf("created stage = %v, want discussion", item["stage"])
	}

	item = requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items/CORE-1/discussion-conclusions", csrf, map[string]any{
		"requestId": "freeze-work-001", "expectedVersion": item["version"], "goalMarkdown": "Ship",
		"scopeMarkdown": "Go server", "outOfScopeMarkdown": "None", "implementationPlanMarkdown": "Build vertically",
		"acceptanceCriteriaMarkdown": "All checks pass", "risksMarkdown": "Migration",
	})
	if item["stage"] != "execution" {
		t.Fatalf("frozen stage = %v, want execution", item["stage"])
	}

	item = requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items/CORE-1/submit-execution", csrf, map[string]any{
		"requestId": "execute-work-001", "expectedVersion": item["version"], "summaryMarkdown": "Implemented",
		"pushed": true, "worktreeClean": true, "forbiddenPathsClean": true, "commits": []string{"abc123"},
		"changedFiles": []string{"main.go"}, "validations": []map[string]any{{"name": "test", "required": true, "exitCode": 0, "durationMs": 10, "logSummary": "ok"}},
		"remainingRisksMarkdown": "",
	})
	if item["stage"] != "acceptance" {
		t.Fatalf("execution stage = %v, want acceptance", item["stage"])
	}

	item = requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items/CORE-1/acceptance-attempts", csrf, map[string]any{
		"requestId": "accept-work-001", "expectedVersion": item["version"], "outcome": "pass", "noteMarkdown": "Accepted",
	})
	if item["stage"] != "completed" {
		t.Fatalf("accepted stage = %v, want completed", item["stage"])
	}
	if conversation, ok := item["conversation"].([]any); !ok || len(conversation) < 4 {
		t.Fatalf("conversation = %#v, want recorded lifecycle", item["conversation"])
	}
}

func TestRunnerPairsPollsAndClaimsAnAssignedWorkItem(t *testing.T) {
	handler, err := server.New(server.Config{DataDir: t.TempDir(), BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123"})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "run", "name": "Runner", "repositoryUrl": "https://github.com/acme/run.git"})
	agent := requestJSON(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "runner-one", "purpose": "Go execution"})
	requestJSON(t, client, http.MethodPut, host.URL+"/api/projects/"+project["id"].(string)+"/agents/"+agent["id"].(string), csrf, map[string]any{})
	pairing := requestJSON(t, client, http.MethodPost, host.URL+"/api/agents/"+agent["id"].(string)+"/pairing-codes", csrf, map[string]any{})
	paired := requestJSON(t, http.DefaultClient, http.MethodPost, host.URL+"/api/agent/pair", "", map[string]any{"code": pairing["code"], "deviceName": "test-runner", "os": "windows", "version": "1.0.0", "publicKeyDigest": "digest"})
	status, code := requestAgentError(t, http.MethodPost, host.URL+"/api/agent/projects/"+project["id"].(string)+"/sync-commits", paired["agentToken"].(string))
	if status != http.StatusForbidden || code != "ACTIVE_PROJECT_RUN_REQUIRED" {
		t.Fatalf("sync without run = %d %s", status, code)
	}
	item := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{"requestId": "runner-item-001", "projectId": project["id"], "title": "Run me", "descriptionMarkdown": "", "acceptanceCriteriaMarkdown": "done", "assigneeKind": "agent", "assigneeId": agent["id"]})
	poll := requestAgentJSON(t, http.DefaultClient, http.MethodPost, host.URL+"/api/agent/poll", paired["agentToken"].(string), map[string]any{})
	assignments := poll["assignments"].([]any)
	if len(assignments) != 1 {
		t.Fatalf("assignments=%#v, want one", assignments)
	}
	mcp := requestAgentJSON(t, http.DefaultClient, http.MethodPost, host.URL+"/mcp", paired["agentToken"].(string), map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "poll_assignments", "arguments": map[string]any{}}})
	if mcp["result"] == nil {
		t.Fatalf("http MCP result=%#v", mcp)
	}
	claimed := requestAgentJSON(t, http.DefaultClient, http.MethodPost, host.URL+"/api/agent/assignments/"+item["id"].(string)+"/accept", paired["agentToken"].(string), map[string]any{"requestId": "claim-item-001", "expectedVersion": item["version"]})
	if claimed["leaseId"] == "" || claimed["runId"] == "" {
		t.Fatalf("claim=%#v, want lease and run", claimed)
	}
	status, code = requestAgentError(t, http.MethodPost, host.URL+"/api/agent/projects/"+project["id"].(string)+"/sync-commits", paired["agentToken"].(string))
	if status != http.StatusConflict || code != "REPOSITORY_GRANT_REQUIRED" {
		t.Fatalf("sync with active run = %d %s", status, code)
	}
}

func TestProjectsReuseAuthorizationThroughIndependentGrants(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	authorization := requestJSON(t, client, http.MethodPost, host.URL+"/api/provider-authorizations", csrf, map[string]any{"provider": "gitlab", "name": "Shared GitLab token", "secret": "access-token"})
	first := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "one", "name": "One", "repositoryUrl": "https://github.com/acme/shared.git"})
	second := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "two", "name": "Two", "repositoryUrl": "https://github.com/acme/shared.git"})
	for _, project := range []map[string]any{first, second} {
		grant := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects/"+project["id"].(string)+"/repository-grant", csrf, map[string]any{"authorizationId": authorization["id"], "repositoryId": "acme/shared", "repositoryName": "acme/shared", "cloneUrl": "https://github.com/acme/shared.git", "defaultBranch": "main", "accessLevel": "write"})
		if grant["authorizationId"] != authorization["id"] {
			t.Fatalf("grant=%#v", grant)
		}
	}
	requestJSON(t, client, http.MethodDelete, host.URL+"/api/projects/"+first["id"].(string)+"/repository-grant", csrf, map[string]any{})
	remaining := requestJSON(t, client, http.MethodGet, host.URL+"/api/projects/"+second["id"].(string)+"/repository-grant", csrf, nil)
	if remaining["authorizationId"] != authorization["id"] {
		t.Fatalf("second grant was not independent: %#v", remaining)
	}
}

func TestAutomaticGitHubConnectionVerifiesAndBindsRepository(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	providerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations/77":
			_, _ = io.WriteString(w, `{"id":77,"account":{"login":"acme"},"permissions":{"contents":"write","metadata":"read"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/77/access_tokens":
			_, _ = io.WriteString(w, `{"token":"installation-token"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/installation/repositories":
			_, _ = io.WriteString(w, `{"repositories":[{"id":101,"full_name":"acme/core","clone_url":"https://github.com/acme/core.git","html_url":"https://github.com/acme/core","default_branch":"main"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/101/commits":
			_, _ = io.WriteString(w, `[{"sha":"abc123","html_url":"https://github.com/acme/core/commit/abc123","commit":{"message":"CORE-1 implement sync","author":{"name":"Agent"}}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer providerAPI.Close()

	handler, err := server.New(server.Config{DataDir: t.TempDir(), BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123", GitConnect: providers.GitConnectConfig{PublicURL: "https://projectboard.example", GitHubAppID: "42", GitHubAppSlug: "projectboard-test", GitHubPrivateKey: string(privatePEM), GitHubAPIURL: providerAPI.URL, GitHubWebURL: providerAPI.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "core", "name": "Core", "repositoryUrl": "https://github.com/acme/core.git"})
	requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{"requestId": "sync-item-001", "projectId": project["id"], "title": "Sync recent commits", "descriptionMarkdown": "", "acceptanceCriteriaMarkdown": "commit is attached"})
	flow := requestJSON(t, client, http.MethodPost, host.URL+"/api/git/connections/github/start", csrf, map[string]any{"projectId": project["id"]})
	authorizationURL, _ := url.Parse(flow["authorizationUrl"].(string))
	state := authorizationURL.Query().Get("state")
	if state == "" {
		t.Fatalf("authorization URL has no state: %s", authorizationURL)
	}
	tampered, err := client.Get(host.URL + "/api/git/connections/github/callback?installation_id=77&state=" + url.QueryEscape(state+"-tampered"))
	if err != nil {
		t.Fatal(err)
	}
	tampered.Body.Close()
	stillWaiting := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/connection-flows/"+flow["id"].(string), csrf, nil)
	if stillWaiting["status"] != "waiting_for_provider" {
		t.Fatalf("tampered callback changed flow: %#v", stillWaiting)
	}
	callback, err := client.Get(host.URL + "/api/git/connections/github/callback?installation_id=77&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	callback.Body.Close()
	if callback.StatusCode != http.StatusOK {
		t.Fatalf("callback status=%d", callback.StatusCode)
	}
	status := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/connection-flows/"+flow["id"].(string), csrf, nil)
	if status["status"] != "ready" || status["matchedRepositoryId"] != "101" || status["webhookStatus"] != "on_demand" {
		t.Fatalf("flow status=%#v", status)
	}
	tamperedGrantBody, _ := json.Marshal(map[string]any{"authorizationId": status["authorizationId"], "repositoryId": "999", "repositoryName": "attacker/other", "cloneUrl": "https://github.com/attacker/other.git", "defaultBranch": "main", "accessLevel": "write"})
	tamperedGrantRequest, _ := http.NewRequest(http.MethodPost, host.URL+"/api/projects/"+project["id"].(string)+"/repository-grant", bytes.NewReader(tamperedGrantBody))
	tamperedGrantRequest.Header.Set("Content-Type", "application/json")
	tamperedGrantRequest.Header.Set("X-CSRF-Token", csrf)
	tamperedGrantResponse, err := client.Do(tamperedGrantRequest)
	if err != nil {
		t.Fatal(err)
	}
	tamperedGrantResponse.Body.Close()
	if tamperedGrantResponse.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("tampered repository grant status=%d", tamperedGrantResponse.StatusCode)
	}
	grant := requestJSON(t, client, http.MethodPost, host.URL+"/api/git/connection-flows/"+flow["id"].(string)+"/complete", csrf, map[string]any{"repositoryId": "101", "accessLevel": "write"})
	if grant["repositoryName"] != "acme/core" || grant["accessLevel"] != "write" {
		t.Fatalf("grant=%#v", grant)
	}
	synced := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects/"+project["id"].(string)+"/sync-commits", csrf, map[string]any{})
	if synced["fetched"] != float64(1) || synced["inserted"] != float64(1) || synced["matchedWorkItems"] != float64(1) {
		t.Fatalf("sync=%#v", synced)
	}
	repeated := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects/"+project["id"].(string)+"/sync-commits", csrf, map[string]any{})
	if repeated["inserted"] != float64(0) {
		t.Fatalf("duplicate sync inserted evidence: %#v", repeated)
	}

	secondFlow := requestJSON(t, client, http.MethodPost, host.URL+"/api/git/connections/github/start", csrf, map[string]any{"projectId": project["id"]})
	requestJSON(t, client, http.MethodPost, host.URL+"/api/git/connection-flows/"+secondFlow["id"].(string)+"/cancel", csrf, map[string]any{})
	canceled := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/connection-flows/"+secondFlow["id"].(string), csrf, nil)
	if canceled["status"] != "canceled" || canceled["errorCode"] != "AUTHORIZATION_CANCELED" {
		t.Fatalf("canceled flow=%#v", canceled)
	}
}

func TestGitProviderSettingsAreConfiguredFromWebAndPersistEncrypted(t *testing.T) {
	dataDir := t.TempDir()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))

	start := func() (*server.Application, *httptest.Server, *http.Client, string) {
		t.Helper()
		handler, startErr := server.New(server.Config{DataDir: dataDir, BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123"})
		if startErr != nil {
			t.Fatal(startErr)
		}
		host := httptest.NewServer(handler)
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
		return handler, host, client, login["csrfToken"].(string)
	}

	handler, host, client, csrf := start()
	requestJSON(t, client, http.MethodPut, host.URL+"/api/system/settings", csrf, map[string]any{"publicUrl": "http://127.0.0.1:3333"})
	saved := requestJSON(t, client, http.MethodPut, host.URL+"/api/git/provider-settings/github", csrf, map[string]any{
		"appId": "42", "appSlug": "projectboard-app", "privateKey": privatePEM,
	})
	if saved["configured"] != true || saved["privateKeyConfigured"] != true || saved["syncMode"] != "on_demand" {
		t.Fatalf("saved settings=%#v", saved)
	}
	savedJSON, _ := json.Marshal(saved)
	if bytes.Contains(savedJSON, []byte("PRIVATE KEY")) {
		t.Fatalf("settings response exposed a secret: %s", savedJSON)
	}
	configuration := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/connections", csrf, nil)
	if configuration["github"].(map[string]any)["configured"] != true {
		t.Fatalf("connection configuration=%#v", configuration)
	}

	host.Close()
	if err = handler.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(dataDir, "projectboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	var encrypted string
	if err = database.QueryRow("SELECT encrypted_config FROM git_provider_settings WHERE provider='github'").Scan(&encrypted); err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()
	if strings.Contains(encrypted, "PRIVATE KEY") {
		t.Fatal("provider settings were stored without encryption")
	}

	handler, host, client, csrf = start()
	defer handler.Close()
	defer host.Close()
	afterRestart := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/connections", csrf, nil)
	if afterRestart["github"].(map[string]any)["configured"] != true {
		t.Fatalf("settings did not survive restart: %#v", afterRestart)
	}
	summary := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/provider-settings", csrf, nil)
	summaryJSON, _ := json.Marshal(summary)
	if bytes.Contains(summaryJSON, []byte("PRIVATE KEY")) {
		t.Fatalf("settings summary exposed a secret: %s", summaryJSON)
	}
}

func TestGitHubManifestFlowAutomaticallyCreatesProviderSettings(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))
	providerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app-manifests/manifest-code/conversions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 42, "slug": "projectboard-auto", "pem": privatePEM,
			"webhook_secret": "manifest-webhook-secret",
		})
	}))
	defer providerAPI.Close()

	handler, err := server.New(server.Config{
		DataDir: t.TempDir(), BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123",
		GitConnect: providers.GitConnectConfig{GitHubAPIURL: providerAPI.URL, GitHubWebURL: providerAPI.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	flow := requestJSON(t, client, http.MethodPost, host.URL+"/api/git/provider-settings/github/manifest/start", csrf, map[string]any{"detectedPublicUrl": "http://127.0.0.1:3333"})
	registrationURL, err := url.Parse(flow["registrationUrl"].(string))
	if err != nil || registrationURL.Query().Get("state") == "" {
		t.Fatalf("registration URL=%v, err=%v", registrationURL, err)
	}
	var manifest map[string]any
	if err = json.Unmarshal([]byte(flow["manifest"].(string)), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["url"] != "http://127.0.0.1:3333" || manifest["setup_url"] != "http://127.0.0.1:3333/api/git/connections/github/callback" {
		t.Fatalf("manifest=%#v", manifest)
	}
	if manifest["hook_attributes"] != nil || manifest["default_events"] != nil {
		t.Fatalf("on-demand manifest unexpectedly configured webhooks: %#v", manifest)
	}
	systemSettings := requestJSON(t, client, http.MethodGet, host.URL+"/api/system/settings", csrf, nil)
	if systemSettings["publicUrl"] != "http://127.0.0.1:3333" || systemSettings["gitSyncMode"] != "on_demand" {
		t.Fatalf("auto-initialized system settings=%#v", systemSettings)
	}
	permissions := manifest["default_permissions"].(map[string]any)
	if permissions["contents"] != "write" {
		t.Fatalf("manifest silently changed permission boundary: %#v", permissions)
	}

	state := registrationURL.Query().Get("state")
	tampered, err := client.Get(host.URL + "/api/git/provider-settings/github/manifest/callback?code=manifest-code&state=" + url.QueryEscape(state+"-tampered"))
	if err != nil {
		t.Fatal(err)
	}
	tampered.Body.Close()
	pending := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/provider-settings/github/manifest-flows/"+flow["id"].(string), csrf, nil)
	if pending["status"] != "waiting_for_github" {
		t.Fatalf("tampered callback changed flow: %#v", pending)
	}

	// The public tunnel hostname used by a local deployment does not share the localhost session cookie.
	// The callback therefore authenticates the one-time state itself rather than depending on a browser session.
	callback, err := http.Get(host.URL + "/api/git/provider-settings/github/manifest/callback?code=manifest-code&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	callback.Body.Close()
	if callback.StatusCode != http.StatusOK {
		t.Fatalf("callback status=%d", callback.StatusCode)
	}
	completed := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/provider-settings/github/manifest-flows/"+flow["id"].(string), csrf, nil)
	if completed["status"] != "completed" {
		t.Fatalf("flow=%#v", completed)
	}
	settings := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/provider-settings", csrf, nil)
	github := settings["github"].(map[string]any)
	if github["configured"] != true || github["appId"] != "42" || github["appSlug"] != "projectboard-auto" {
		t.Fatalf("GitHub settings=%#v", github)
	}
	encoded, _ := json.Marshal(settings)
	if bytes.Contains(encoded, []byte(privatePEM)) || bytes.Contains(encoded, []byte("manifest-webhook-secret")) {
		t.Fatalf("settings exposed manifest secrets: %s", encoded)
	}

	canceledFlow := requestJSON(t, client, http.MethodPost, host.URL+"/api/git/provider-settings/github/manifest/start", csrf, map[string]any{"organization": "acme"})
	organizationURL, _ := url.Parse(canceledFlow["registrationUrl"].(string))
	if organizationURL.Path != "/organizations/acme/settings/apps/new" {
		t.Fatalf("organization registration URL=%s", organizationURL)
	}
	requestJSON(t, client, http.MethodPost, host.URL+"/api/git/provider-settings/github/manifest-flows/"+canceledFlow["id"].(string)+"/cancel", csrf, map[string]any{})
	canceled := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/provider-settings/github/manifest-flows/"+canceledFlow["id"].(string), csrf, nil)
	if canceled["status"] != "failed" || canceled["errorCode"] != "GITHUB_SETUP_CANCELED" {
		t.Fatalf("canceled manifest flow=%#v", canceled)
	}
}

func TestAutomaticGitLabOAuthUsesPKCEAndOnDemandSync(t *testing.T) {
	providerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("code_verifier") == "" || r.Form.Get("code") != "oauth-code" {
				http.Error(w, "missing PKCE verifier", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"gitlab-token","refresh_token":"refresh-token","token_type":"Bearer","scope":"api","expires_in":7200}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/user":
			_, _ = io.WriteString(w, `{"username":"operator","name":"Operator"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects":
			_, _ = io.WriteString(w, `[{"id":202,"path_with_namespace":"acme/service","http_url_to_repo":"https://gitlab.example/acme/service.git","web_url":"https://gitlab.example/acme/service","default_branch":"main","permissions":{"project_access":{"access_level":40}}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer providerAPI.Close()
	handler, err := server.New(server.Config{DataDir: t.TempDir(), BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123", GitConnect: providers.GitConnectConfig{PublicURL: "https://projectboard.example", GitLabClientID: "client-id", GitLabClientSecret: "client-secret", GitLabBaseURL: providerAPI.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "service", "name": "Service", "repositoryUrl": "https://gitlab.example/acme/service.git"})
	flow := requestJSON(t, client, http.MethodPost, host.URL+"/api/git/connections/gitlab/start", csrf, map[string]any{"projectId": project["id"]})
	authorizationURL, _ := url.Parse(flow["authorizationUrl"].(string))
	if authorizationURL.Query().Get("code_challenge") == "" || authorizationURL.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL missing PKCE: %s", authorizationURL)
	}
	callback, err := client.Get(host.URL + "/api/git/connections/gitlab/callback?code=oauth-code&state=" + url.QueryEscape(authorizationURL.Query().Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	callback.Body.Close()
	status := requestJSON(t, client, http.MethodGet, host.URL+"/api/git/connection-flows/"+flow["id"].(string), csrf, nil)
	if status["status"] != "ready" || status["matchedRepositoryId"] != "202" {
		t.Fatalf("flow status=%#v", status)
	}
	grant := requestJSON(t, client, http.MethodPost, host.URL+"/api/git/connection-flows/"+flow["id"].(string)+"/complete", csrf, map[string]any{"repositoryId": "202", "accessLevel": "write"})
	if grant["provider"] != "gitlab" || grant["repositoryName"] != "acme/service" {
		t.Fatalf("grant=%#v", grant)
	}
}

func requestJSON(t *testing.T, client *http.Client, method, endpoint, csrf string, body any) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(body)
	request, _ := http.NewRequest(method, endpoint, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var value map[string]any
	_ = json.NewDecoder(response.Body).Decode(&value)
	if response.StatusCode >= 300 {
		t.Fatalf("%s %s = %d %#v", method, endpoint, response.StatusCode, value)
	}
	return value
}

func requestAgentJSON(t *testing.T, client *http.Client, method, endpoint, token string, body any) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(body)
	request, _ := http.NewRequest(method, endpoint, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var value map[string]any
	_ = json.NewDecoder(response.Body).Decode(&value)
	if response.StatusCode >= 300 {
		t.Fatalf("%s %s = %d %#v", method, endpoint, response.StatusCode, value)
	}
	return value
}

func requestAgentError(t *testing.T, method, endpoint, token string) (int, string) {
	t.Helper()
	request, _ := http.NewRequest(method, endpoint, bytes.NewReader([]byte("{}")))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var value map[string]any
	_ = json.NewDecoder(response.Body).Decode(&value)
	errorValue, _ := value["error"].(map[string]any)
	code, _ := errorValue["code"].(string)
	return response.StatusCode, code
}

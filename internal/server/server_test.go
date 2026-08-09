package server_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

func TestDeletingUserRetainsRecordButHidesItFromLists(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	created := requestJSON(t, client, http.MethodPost, host.URL+"/api/users", csrf, map[string]any{"username": "soft-delete-user", "displayName": "Soft Delete User", "systemRole": "user"})
	userID := created["id"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "soft-delete", "name": "Soft Delete", "repositoryUrl": "https://example.com/soft-delete.git"})
	projectID := project["id"].(string)
	requestJSON(t, client, http.MethodPost, host.URL+"/api/projects/"+projectID+"/members", csrf, map[string]any{"userId": userID, "role": "viewer"})

	requestJSON(t, client, http.MethodDelete, host.URL+"/api/users/"+userID, csrf, nil)
	requestErrorCode(t, client, http.MethodPost, host.URL+"/api/users/"+userID+"/enable", csrf, nil, http.StatusNotFound, "NOT_FOUND")
	users := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/users", csrf, nil)
	for _, user := range users {
		if user["id"] == userID {
			t.Fatalf("deleted user remained in default list: %#v", user)
		}
	}
	retained := requestJSON(t, client, http.MethodGet, host.URL+"/api/users/"+userID, csrf, nil)
	if retained["status"] != "deleted" {
		t.Fatalf("retained user status=%#v, want deleted", retained["status"])
	}
	members := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/projects/"+projectID+"/members", csrf, nil)
	for _, member := range members {
		if member["id"] == userID {
			t.Fatalf("deleted user remained in project member list: %#v", member)
		}
	}
}

func TestDeletingAgentHidesItFromOrganizationList(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	agent := requestJSON(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "soft-delete-agent", "purpose": "retained history"})
	agentID := agent["id"].(string)
	requestJSON(t, client, http.MethodPost, host.URL+"/api/agents/"+agentID+"/disable", csrf, nil)
	foundDisabled := false
	for _, listedAgent := range requestJSONArray(t, client, http.MethodGet, host.URL+"/api/agents", csrf, nil) {
		if listedAgent["id"] == agentID {
			foundDisabled = listedAgent["status"] == "disabled" && listedAgent["enabled"] == false
		}
	}
	if !foundDisabled {
		t.Fatal("disabled Agent status was not exposed by the organization list")
	}
	requestJSON(t, client, http.MethodPost, host.URL+"/api/agents/"+agentID+"/enable", csrf, nil)

	requestJSON(t, client, http.MethodDelete, host.URL+"/api/agents/"+agentID, csrf, nil)
	for _, listedAgent := range requestJSONArray(t, client, http.MethodGet, host.URL+"/api/agents", csrf, nil) {
		if listedAgent["id"] == agentID {
			t.Fatalf("deleted agent remained in organization list: %#v", listedAgent)
		}
	}
}

func TestActivitySupportsPagination(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	for index := 1; index <= 5; index++ {
		requestJSON(t, client, http.MethodPost, host.URL+"/api/users", csrf, map[string]any{"username": fmt.Sprintf("audit-user-%d", index), "displayName": fmt.Sprintf("Audit User %d", index), "systemRole": "user"})
	}
	first := requestJSON(t, client, http.MethodGet, host.URL+"/api/activity?page=1&pageSize=2", csrf, nil)
	second := requestJSON(t, client, http.MethodGet, host.URL+"/api/activity?page=2&pageSize=2", csrf, nil)
	firstItems := first["items"].([]any)
	secondItems := second["items"].([]any)
	if first["total"].(float64) < 5 || len(firstItems) != 2 || len(secondItems) != 2 {
		t.Fatalf("unexpected activity pages: first=%#v second=%#v", first, second)
	}
	if firstItems[0].(map[string]any)["id"] == secondItems[0].(map[string]any)["id"] {
		t.Fatal("activity pagination returned the same leading event on both pages")
	}
}

func TestLocalCodexAgentConfigurationIsExposed(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	agent := requestJSON(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "local-codex", "runtimeType": "local_codex_cli", "maxConcurrentTasks": 3, "turnTimeoutMinutes": 45})
	if agent["runtimeType"] != "local_codex_cli" || agent["maxConcurrentTasks"] != float64(3) || agent["turnTimeoutMinutes"] != float64(45) || agent["purpose"] != "" {
		t.Fatalf("unexpected Agent: %#v", agent)
	}
	agents := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/agents", csrf, nil)
	if len(agents) != 1 || agents[0]["currentLoad"] != float64(0) {
		t.Fatalf("unexpected Agents: %#v", agents)
	}
	requestErrorCode(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "invalid", "maxConcurrentTasks": -1, "turnTimeoutMinutes": 120}, http.StatusUnprocessableEntity, "INVALID_AGENT_CONFIGURATION")
}

func TestCreatingAgentFailsWhenCodexIsMissingFromPath(t *testing.T) {
	handler, err := server.New(server.Config{
		DataDir:           t.TempDir(),
		BootstrapUsername: "admin",
		BootstrapPassword: "StrongPassword123",
		LookPath:          func(string) (string, error) { return "", errors.New("missing") },
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
	requestErrorCode(t, client, http.MethodPost, host.URL+"/api/agents", login["csrfToken"].(string), map[string]any{"name": "missing-codex", "purpose": "test PATH validation"}, http.StatusUnprocessableEntity, "CODEX_CLI_NOT_FOUND")
}

func TestTaskMessageAttachmentsCanBeUploadedBoundAndPreviewed(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "files", "name": "Files", "repositoryUrl": "https://example.com/files.git"})
	item := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{"requestId": "attachment-task", "projectId": project["id"], "title": "Attachment task", "descriptionMarkdown": "Check attachments", "acceptanceCriteriaMarkdown": "Markdown previews", "priority": "medium"})

	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("file", "review.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("# Review\n\n- safe preview"))
	_ = writer.Close()
	request, _ := http.NewRequest(http.MethodPost, host.URL+"/api/work-items/FILES-1/attachments", &upload)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-CSRF-Token", csrf)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var attachment map[string]any
	_ = json.NewDecoder(response.Body).Decode(&attachment)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("upload=%d %#v", response.StatusCode, attachment)
	}

	item = requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items/FILES-1/messages", csrf, map[string]any{"markdown": "Please review", "expectedVersion": item["version"], "attachmentIds": []string{attachment["id"].(string)}})
	attachments := item["attachments"].([]any)
	if len(attachments) != 1 || attachments[0].(map[string]any)["entry_id"] == nil {
		t.Fatalf("attachments=%#v", attachments)
	}
	preview, err := client.Get(host.URL + "/api/attachments/" + attachment["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Body.Close()
	content, _ := io.ReadAll(preview.Body)
	if preview.StatusCode != http.StatusOK || !strings.Contains(preview.Header.Get("Content-Disposition"), "inline") || string(content) != "# Review\n\n- safe preview" {
		t.Fatalf("preview=%d disposition=%q body=%q", preview.StatusCode, preview.Header.Get("Content-Disposition"), content)
	}

	var jsonUpload bytes.Buffer
	jsonWriter := multipart.NewWriter(&jsonUpload)
	jsonPart, err := jsonWriter.CreateFormFile("file", "context.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = jsonPart.Write([]byte(`{"owner":"Codex","ready":true}`))
	_ = jsonWriter.Close()
	jsonRequest, _ := http.NewRequest(http.MethodPost, host.URL+"/api/work-items/FILES-1/attachments", &jsonUpload)
	jsonRequest.Header.Set("Content-Type", jsonWriter.FormDataContentType())
	jsonRequest.Header.Set("X-CSRF-Token", csrf)
	jsonResponse, err := client.Do(jsonRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer jsonResponse.Body.Close()
	var jsonAttachment map[string]any
	_ = json.NewDecoder(jsonResponse.Body).Decode(&jsonAttachment)
	if jsonResponse.StatusCode != http.StatusCreated || jsonAttachment["entry_id"] != nil {
		t.Fatalf("description attachment=%d %#v", jsonResponse.StatusCode, jsonAttachment)
	}
	jsonPreview, err := client.Get(host.URL + "/api/attachments/" + jsonAttachment["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer jsonPreview.Body.Close()
	jsonContent, _ := io.ReadAll(jsonPreview.Body)
	if jsonPreview.StatusCode != http.StatusOK || !strings.Contains(jsonPreview.Header.Get("Content-Disposition"), "inline") || string(jsonContent) != `{"owner":"Codex","ready":true}` {
		t.Fatalf("json preview=%d disposition=%q body=%q", jsonPreview.StatusCode, jsonPreview.Header.Get("Content-Disposition"), jsonContent)
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
	if item["stage"] != "created" {
		t.Fatalf("created stage = %v, want created", item["stage"])
	}
	for _, target := range []string{"in_progress", "completed", "closed"} {
		item = requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items/CORE-1/stage", csrf, map[string]any{"expectedVersion": item["version"], "targetStage": target, "noteMarkdown": "advance"})
		if item["stage"] != target {
			t.Fatalf("stage = %v, want %s", item["stage"], target)
		}
	}
	if conversation, ok := item["conversation"].([]any); !ok || len(conversation) < 4 {
		t.Fatalf("conversation = %#v, want recorded lifecycle", item["conversation"])
	}
}

func TestWorkItemFollowersCriteriaAndNotifications(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	adminJar, _ := cookiejar.New(nil)
	adminClient := &http.Client{Jar: adminJar}
	login := requestJSON(t, adminClient, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	admin := login["user"].(map[string]any)
	member := requestJSON(t, adminClient, http.MethodPost, host.URL+"/api/users", csrf, map[string]any{"username": "follower", "displayName": "Follower", "systemRole": "user"})
	project := requestJSON(t, adminClient, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "people", "name": "People", "repositoryUrl": "https://github.com/acme/people.git"})
	requestJSON(t, adminClient, http.MethodPost, host.URL+"/api/projects/"+project["id"].(string)+"/members", csrf, map[string]any{"userId": member["id"], "role": "developer"})
	item := requestJSON(t, adminClient, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{
		"projectId": project["id"], "title": "Keep everyone informed", "acceptanceCriteriaMarkdown": "Initial criterion",
		"assigneeKind": "human", "assigneeId": member["id"], "followerIds": []string{member["id"].(string)},
	})
	if item["created_by_user_id"] != admin["id"] {
		t.Fatalf("creator=%v, want %v", item["created_by_user_id"], admin["id"])
	}
	followers := item["follower_ids"].([]any)
	if len(followers) != 1 || followers[0] != member["id"] {
		t.Fatalf("followers=%#v", followers)
	}
	item = requestJSON(t, adminClient, http.MethodPost, host.URL+"/api/work-items/PEOPLE-1/stage", csrf, map[string]any{"expectedVersion": item["version"], "targetStage": "in_progress"})
	if item["stage"] != "in_progress" {
		t.Fatalf("stage=%v, want in_progress", item["stage"])
	}
	item = requestJSON(t, adminClient, http.MethodPatch, host.URL+"/api/work-items/PEOPLE-1", csrf, map[string]any{"expectedVersion": item["version"], "acceptanceCriteriaMarkdown": "Initial criterion\nAdded during execution"})
	if item["stage"] != "in_progress" || !strings.Contains(item["acceptance_criteria_markdown"].(string), "Added during execution") {
		t.Fatalf("criteria update during progress failed: %#v", item)
	}
	memberJar, _ := cookiejar.New(nil)
	memberClient := &http.Client{Jar: memberJar}
	requestJSON(t, memberClient, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "follower", "password": member["temporaryPassword"]})
	notifications := requestJSONArray(t, memberClient, http.MethodGet, host.URL+"/api/notifications", "", nil)
	if len(notifications) < 2 {
		t.Fatalf("notifications=%#v, want assignment and update", notifications)
	}
	paged := requestJSON(t, memberClient, http.MethodGet, host.URL+"/api/notifications?page=1&pageSize=1&projectId="+project["id"].(string), "", nil)
	items, ok := paged["items"].([]any)
	if !ok || len(items) != 1 || paged["total"].(float64) < 2 || paged["unreadCount"].(float64) < 2 {
		t.Fatalf("paged notifications=%#v", paged)
	}
	if items[0].(map[string]any)["project_name"] != "People" {
		t.Fatalf("notification project=%#v, want People", items[0])
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

func requestJSONArray(t *testing.T, client *http.Client, method, endpoint, csrf string, body any) []map[string]any {
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
	var value []map[string]any
	_ = json.NewDecoder(response.Body).Decode(&value)
	if response.StatusCode >= 300 {
		t.Fatalf("%s %s = %d", method, endpoint, response.StatusCode)
	}
	return value
}

func requestErrorCode(t *testing.T, client *http.Client, method, endpoint, csrf string, body any, wantStatus int, wantCode string) {
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
	var value struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(response.Body).Decode(&value)
	if response.StatusCode != wantStatus || value.Error.Code != wantCode {
		t.Fatalf("%s %s = %d %q, want %d %q", method, endpoint, response.StatusCode, value.Error.Code, wantStatus, wantCode)
	}
}

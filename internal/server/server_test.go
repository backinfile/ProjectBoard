package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

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
	authorization := requestJSON(t, client, http.MethodPost, host.URL+"/api/provider-authorizations", csrf, map[string]any{"provider": "github", "name": "Shared GitHub App", "appId": "42", "installationId": "84", "secret": "private-key"})
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

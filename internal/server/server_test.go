package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/agentexec"
	"github.com/projectboard/projectboard/internal/server"
)

type cancelAwareRunner struct {
	started  chan string
	canceled chan string
}

func TestBlockingRunningAgentTaskCancelsOnlyThatExecution(t *testing.T) {
	dataDir := t.TempDir()
	runner := &cancelAwareRunner{started: make(chan string, 2), canceled: make(chan string, 2)}
	handler, err := server.New(server.Config{
		DataDir:           dataDir,
		BootstrapUsername: "admin",
		BootstrapPassword: "StrongPassword123",
		LookPath:          func(string) (string, error) { return "codex", nil },
		AgentRunner:       runner,
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
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "cancel", "name": "Cancel", "projectPath": filepath.Join(dataDir, "cancel-project")})
	agent := requestJSON(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "cancel-aware", "maxConcurrentTasks": 2, "turnTimeoutMinutes": 120})

	for number := 1; number <= 2; number++ {
		requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{
			"projectId": project["id"], "title": fmt.Sprintf("Task %d", number), "descriptionMarkdown": "Keep running", "acceptanceCriteriaMarkdown": "Canceled selectively", "isAgentTask": true,
		})
	}

	started := map[string]bool{}
	deadline := time.After(3 * time.Second)
	for len(started) < 2 {
		select {
		case item := <-runner.started:
			started[item] = true
		case <-deadline:
			t.Fatalf("started=%v, want both executions", started)
		}
	}
	live := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/agents/"+agent["id"].(string)+"/executions/current", csrf, nil)
	if len(live) != 2 || !strings.Contains(live[0]["output"].(string), "command_execution") {
		t.Fatalf("live executions=%#v, want streamed Codex output", live)
	}
	first := requestJSON(t, client, http.MethodGet, host.URL+"/api/work-items/CANCEL-1", csrf, nil)
	blocked := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items/CANCEL-1/block", csrf, map[string]any{"expectedVersion": first["version"], "reason": "manual diagnosis"})
	if blocked["blocked_at"] == nil || blocked["agent_state"] != "queued" {
		t.Fatalf("blocked item=%#v, want blocked and queued", blocked)
	}
	select {
	case canceled := <-runner.canceled:
		if canceled != "cancel-1" {
			t.Fatalf("canceled=%q, want cancel-1", canceled)
		}
	case <-time.After(time.Second):
		t.Fatal("blocking a running task did not cancel its Agent execution")
	}
	second := requestJSON(t, client, http.MethodGet, host.URL+"/api/work-items/CANCEL-2", csrf, nil)
	if second["agent_state"] != "running" {
		t.Fatalf("second task state=%v, want running", second["agent_state"])
	}
	executions := blocked["executions"].([]any)
	if len(executions) != 2 || executions[0].(map[string]any)["state"] != "failed" || executions[1].(map[string]any)["state"] != "queued" {
		t.Fatalf("blocked executions=%#v, want failed immutable request plus queued retry", executions)
	}
}

func TestInactiveAgentExecutionIsAutomaticallyDiagnosedAndStopped(t *testing.T) {
	dataDir := t.TempDir()
	runner := &cancelAwareRunner{started: make(chan string, 1), canceled: make(chan string, 1)}
	handler, err := server.New(server.Config{
		DataDir: dataDir, BootstrapUsername: "admin", BootstrapPassword: "StrongPassword123",
		LookPath: func(string) (string, error) { return "codex", nil }, AgentRunner: runner,
		AgentPollInterval: 10 * time.Millisecond, AgentStallTimeout: 40 * time.Millisecond,
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
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "stall", "name": "Stall", "projectPath": filepath.Join(dataDir, "stall-project")})
	requestJSON(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "stall-agent"})
	requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{"projectId": project["id"], "title": "Stall", "descriptionMarkdown": "No more output", "acceptanceCriteriaMarkdown": "Auto-stop", "isAgentTask": true})
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("Agent execution did not start")
	}
	select {
	case canceled := <-runner.canceled:
		if canceled != "stall-1" {
			t.Fatalf("canceled=%q, want stall-1", canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inactive Agent execution was not stopped")
	}
	deadline := time.Now().Add(time.Second)
	for {
		item := requestJSON(t, client, http.MethodGet, host.URL+"/api/work-items/STALL-1", csrf, nil)
		if item["agent_state"] == "paused_failure" {
			executions := item["executions"].([]any)
			if executions[len(executions)-1].(map[string]any)["state"] != "failed" {
				t.Fatalf("executions=%#v, want failed request after stall", executions)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("item=%#v, want paused_failure", item)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *cancelAwareRunner) Run(ctx context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	item := filepath.Base(invocation.WorkDir)
	if invocation.OnOutput != nil {
		invocation.OnOutput([]byte(`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}` + "\n"))
	}
	r.started <- item
	<-ctx.Done()
	r.canceled <- item
	return agentexec.Result{ThreadID: "thread-" + item}, ctx.Err()
}

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

func TestUnversionedStaticAssetsRevalidateAfterDeployment(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()

	response, err := http.Get(host.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if got := response.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache so a redeployed unversioned asset is revalidated", got)
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
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "soft-delete", "name": "Soft Delete", "projectPath": filepath.Join(t.TempDir(), "soft-delete")})
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
	agent := requestJSON(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "local-codex", "runtimeType": "local_codex_cli", "model": "gpt-5.6-terra", "reasoningEffort": "high", "maxConcurrentTasks": 3, "turnTimeoutMinutes": 45, "acceptTags": []string{"frontend"}, "rejectTags": []string{"blocked"}})
	if agent["runtimeType"] != "local_codex_cli" || agent["model"] != "gpt-5.6-terra" || agent["reasoningEffort"] != "high" || agent["maxConcurrentTasks"] != float64(3) || agent["turnTimeoutMinutes"] != float64(45) || agent["purpose"] != "" {
		t.Fatalf("unexpected Agent: %#v", agent)
	}
	agent = requestJSON(t, client, http.MethodPatch, host.URL+"/api/agents/"+agent["id"].(string), csrf, map[string]any{"purpose": "UI work", "model": "gpt-5.6-sol", "reasoningEffort": "xhigh", "acceptTags": []string{"frontend", "ux"}, "rejectTags": []string{"blocked"}})
	if fmt.Sprint(agent["acceptTags"]) != "[frontend ux]" || fmt.Sprint(agent["rejectTags"]) != "[blocked]" || agent["purpose"] != "UI work" || agent["model"] != "gpt-5.6-sol" || agent["reasoningEffort"] != "xhigh" {
		t.Fatalf("updated Agent tags were not exposed: %#v", agent)
	}
	agents := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/agents", csrf, nil)
	if len(agents) != 1 || agents[0]["currentLoad"] != float64(0) || agents[0]["totalTokens"] != float64(0) {
		t.Fatalf("unexpected Agents: %#v", agents)
	}
	requestErrorCode(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "invalid", "maxConcurrentTasks": -1, "turnTimeoutMinutes": 120}, http.StatusUnprocessableEntity, "INVALID_AGENT_CONFIGURATION")
	requestErrorCode(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "invalid-effort", "reasoningEffort": "extreme"}, http.StatusUnprocessableEntity, "INVALID_REASONING_EFFORT")
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
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "files", "name": "Files", "projectPath": filepath.Join(t.TempDir(), "files")})
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
		"key": "core", "name": "Core", "projectPath": filepath.Join(t.TempDir(), "core"),
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
	requests := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/agent-requests?projectId="+project["id"].(string), csrf, nil)
	if len(requests) != 1 || requests[0]["kind"] != "task_knowledge" || requests[0]["sourceWorkItemId"] != "CORE-1" || requests[0]["status"] != "queued" {
		t.Fatalf("agent requests = %#v, want one queued task knowledge request", requests)
	}
}

func TestAgentTaskCreatesOneShotRequestAndRetryCreatesANewRequest(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "agent", "name": "Agent", "projectPath": filepath.Join(t.TempDir(), "agent")})
	item := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{
		"projectId": project["id"], "title": "Plan this", "isAgentTask": true, "pauseAfterPlan": true,
	})
	if item["agent_state"] != "idle" {
		t.Fatalf("task retained legacy execution state: %#v", item)
	}
	requests := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/agent-requests?projectId="+project["id"].(string), csrf, nil)
	if len(requests) != 1 || requests[0]["kind"] != "task_plan" || requests[0]["sourceWorkItemId"] != "AGENT-1" {
		t.Fatalf("initial requests = %#v", requests)
	}
	originalID := requests[0]["id"].(string)
	cancelled := requestJSON(t, client, http.MethodPost, host.URL+"/api/agent-requests/"+originalID+"/cancel", csrf, map[string]any{})
	if cancelled["status"] != "cancelled" {
		t.Fatalf("cancelled request = %#v", cancelled)
	}
	retried := requestJSON(t, client, http.MethodPost, host.URL+"/api/agent-requests/"+originalID+"/retry", csrf, map[string]any{})
	if retried["id"] == originalID || retried["retryOfId"] != originalID || retried["status"] != "queued" {
		t.Fatalf("retried request = %#v", retried)
	}
}

func TestProjectKnowledgeTreeSupportsSearchLocksAndHistory(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "kb", "name": "Knowledge", "projectPath": filepath.Join(t.TempDir(), "knowledge")})
	root := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects/"+project["id"].(string)+"/knowledge", csrf, map[string]any{
		"title": "Architecture", "markdown": "System overview", "summary": "Service topology and module map", "triggerDescription": "Use when changing module responsibilities",
	})
	requestJSON(t, client, http.MethodPost, host.URL+"/api/projects/"+project["id"].(string)+"/knowledge", csrf, map[string]any{"parentId": root["id"], "title": "API", "markdown": "HTTP contract"})
	found := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/projects/"+project["id"].(string)+"/knowledge?query=topology", csrf, nil)
	if len(found) != 1 || found[0]["id"] != root["id"] || root["triggerDescription"] != "Use when changing module responsibilities" {
		t.Fatalf("knowledge search = %#v", found)
	}
	locked := requestJSON(t, client, http.MethodPost, host.URL+"/api/knowledge/nodes/"+root["id"].(string)+"/lock", csrf, map[string]any{"expectedVersion": root["version"], "locked": true})
	if locked["lockedForAgents"] != true {
		t.Fatalf("locked node = %#v", locked)
	}
	updated := requestJSON(t, client, http.MethodPatch, host.URL+"/api/knowledge/nodes/"+root["id"].(string), csrf, map[string]any{"expectedVersion": locked["version"], "title": "Architecture", "markdown": "Human edits remain allowed"})
	revisions := requestJSONArray(t, client, http.MethodGet, host.URL+"/api/knowledge/nodes/"+root["id"].(string)+"/revisions", csrf, nil)
	if updated["version"] != float64(3) || len(revisions) != 3 {
		t.Fatalf("updated=%#v revisions=%#v", updated, revisions)
	}
	viewer := requestJSON(t, client, http.MethodPost, host.URL+"/api/users", csrf, map[string]any{"username": "knowledge-viewer", "displayName": "Knowledge Viewer", "systemRole": "user"})
	requestJSON(t, client, http.MethodPost, host.URL+"/api/projects/"+project["id"].(string)+"/members", csrf, map[string]any{"userId": viewer["id"], "role": "viewer"})
	viewerJar, _ := cookiejar.New(nil)
	viewerClient := &http.Client{Jar: viewerJar}
	viewerLogin := requestJSON(t, viewerClient, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "knowledge-viewer", "password": viewer["temporaryPassword"]})
	viewerCSRF := viewerLogin["csrfToken"].(string)
	visible := requestJSONArray(t, viewerClient, http.MethodGet, host.URL+"/api/projects/"+project["id"].(string)+"/knowledge", "", nil)
	if len(visible) != 2 {
		t.Fatalf("viewer knowledge = %#v", visible)
	}
	requestErrorCode(t, viewerClient, http.MethodPatch, host.URL+"/api/knowledge/nodes/"+root["id"].(string), viewerCSRF, map[string]any{"expectedVersion": updated["version"], "title": "Denied", "markdown": "Denied"}, http.StatusForbidden, "DEVELOPER_REQUIRED")
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
	project := requestJSON(t, adminClient, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "people", "name": "People", "projectPath": filepath.Join(t.TempDir(), "people")})
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

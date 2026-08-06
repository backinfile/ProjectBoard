package server_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/projectboard/projectboard/internal/server"
)

func TestProjectWorkflowSettingsPersistAndStagesCanBeSelectedDirectly(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)

	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{
		"key": "workflow", "name": "Workflow", "repositoryUrl": "https://example.com/workflow.git",
	})
	if project["allowAgentExecution"] != true || project["allowSubtasks"] != true {
		t.Fatalf("new project defaults = %#v", project)
	}

	project = requestJSON(t, client, http.MethodPatch, host.URL+"/api/projects/"+project["id"].(string), csrf, map[string]any{
		"allowAgentExecution":         false,
		"allowAgentAutoClose":         true,
		"allowSubtasks":               false,
		"allowAgentAutoCloseSubtasks": true,
		"agentPrompts":                []string{"Prefer small commits.", "Run focused tests first."},
	})
	if project["allowAgentExecution"] != false || project["allowAgentAutoClose"] != true || project["allowSubtasks"] != false || project["allowAgentAutoCloseSubtasks"] != true {
		t.Fatalf("saved workflow policy = %#v", project)
	}
	if prompts, ok := project["agentPrompts"].([]any); !ok || len(prompts) != 2 {
		t.Fatalf("agent prompts = %#v", project["agentPrompts"])
	}

	item := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{
		"projectId": project["id"], "title": "Selectable stage", "acceptanceCriteriaMarkdown": "Stage changes are recorded",
	})
	item = requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items/WORKFLOW-1/stage", csrf, map[string]any{
		"targetStage": "order_closed", "noteMarkdown": "Close after review", "expectedVersion": item["version"],
	})
	if item["stage"] != "order_closed" || item["completed_at"] == nil {
		t.Fatalf("closed item = %#v", item)
	}
	item = requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items/WORKFLOW-1/stage", csrf, map[string]any{
		"targetStage": "discussion", "noteMarkdown": "Reopen for clarification", "expectedVersion": item["version"],
	})
	if item["stage"] != "discussion" || item["completed_at"] != nil {
		t.Fatalf("reopened item = %#v", item)
	}
}

func TestAgentCanCreateSubtasksOnlyFromAnActiveAssignedParent(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "children", "name": "Children", "repositoryUrl": "https://example.com/children.git"})
	agent := requestJSON(t, client, http.MethodPost, host.URL+"/api/agents", csrf, map[string]any{"name": "planner", "purpose": "Split large tasks"})
	requestJSON(t, client, http.MethodPut, host.URL+"/api/projects/"+project["id"].(string)+"/agents/"+agent["id"].(string), csrf, map[string]any{})
	pairing := requestJSON(t, client, http.MethodPost, host.URL+"/api/agents/"+agent["id"].(string)+"/pairing-codes", csrf, map[string]any{})
	paired := requestJSON(t, http.DefaultClient, http.MethodPost, host.URL+"/api/agent/pair", "", map[string]any{"code": pairing["code"], "deviceName": "planner-runner", "os": "windows", "version": "1.0.0", "publicKeyDigest": "digest"})
	token := paired["agentToken"].(string)
	parent := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{"projectId": project["id"], "title": "Large task", "assigneeKind": "agent", "assigneeId": agent["id"]})
	claimed := requestAgentJSON(t, http.DefaultClient, http.MethodPost, host.URL+"/api/agent/assignments/"+parent["id"].(string)+"/accept", token, map[string]any{"expectedVersion": parent["version"]})

	child := requestAgentJSON(t, http.DefaultClient, http.MethodPost, host.URL+"/api/agent/work-items/"+parent["id"].(string)+"/subtasks", token, map[string]any{
		"title": "Parallel slice", "descriptionMarkdown": "Implement one vertical slice", "acceptanceCriteriaMarkdown": "Focused checks pass", "priority": "high",
	})
	if child["parent_id"] != parent["id"] || child["assignee_id"] != agent["id"] || child["stage"] != "todo" {
		t.Fatalf("agent-created subtask = %#v", child)
	}
	requestJSON(t, client, http.MethodPatch, host.URL+"/api/projects/"+project["id"].(string), csrf, map[string]any{"allowAgentAutoClose": true})
	claimedItem := claimed["workItem"].(map[string]any)
	closed := requestAgentJSON(t, http.DefaultClient, http.MethodPost, host.URL+"/api/agent/work-items/"+parent["id"].(string)+"/stage", token, map[string]any{
		"expectedVersion": claimedItem["version"], "targetStage": "completed", "noteMarkdown": "All parent work accepted",
	})
	if closed["stage"] != "order_closed" {
		t.Fatalf("agent auto-close stage = %v, want order_closed", closed["stage"])
	}
}

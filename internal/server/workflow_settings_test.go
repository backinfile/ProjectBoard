package server_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/projectboard/projectboard/internal/server"
)

func TestTaskAgentPauseSettingsAreTaskScoped(t *testing.T) {
	handler := server.NewTestHandler(t.TempDir())
	defer handler.Close()
	host := httptest.NewServer(handler)
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login := requestJSON(t, client, http.MethodPost, host.URL+"/api/auth/login", "", map[string]any{"username": "admin", "password": "StrongPassword123"})
	csrf := login["csrfToken"].(string)
	project := requestJSON(t, client, http.MethodPost, host.URL+"/api/projects", csrf, map[string]any{"key": "workflow", "name": "Workflow", "repositoryUrl": "https://example.com/workflow.git"})
	human := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{"projectId": project["id"], "title": "Human", "acceptanceCriteriaMarkdown": "Done", "pauseAfterPlan": true, "pauseAfterCompletion": true})
	if human["is_agent_task"] != false || human["pause_after_plan"] != false || human["pause_after_completion"] != false {
		t.Fatalf("human task leaked Agent flags: %#v", human)
	}
	agent := requestJSON(t, client, http.MethodPost, host.URL+"/api/work-items", csrf, map[string]any{"projectId": project["id"], "title": "Agent", "acceptanceCriteriaMarkdown": "Done", "isAgentTask": true, "pauseAfterPlan": true, "pauseAfterCompletion": true})
	if agent["is_agent_task"] != true || agent["pause_after_plan"] != true || agent["pause_after_completion"] != true || agent["stage"] != "created" {
		t.Fatalf("Agent task settings: %#v", agent)
	}
}

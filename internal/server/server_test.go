package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/domain"
	"github.com/projectboard/projectboard/internal/events"
	"github.com/projectboard/projectboard/internal/server"
	"github.com/projectboard/projectboard/internal/store"
)

func TestUserCanCreateProjectAndTask(t *testing.T) {
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := httptest.NewServer(server.New(st, events.New()).Handler())
	defer srv.Close()
	project := post(t, srv.URL+"/api/v1/projects", map[string]any{"name": "示例项目", "path": "D:\\code\\example"})
	projectID := project["id"].(string)
	task := post(t, srv.URL+"/api/v1/projects/"+projectID+"/tasks", map[string]any{"title": "实现任务对话", "description": "支持 @Agent"})
	if task["status"] != "inbox" {
		t.Fatalf("expected inbox, got %v", task["status"])
	}
	res, err := http.Get(srv.URL + "/api/v1/projects/" + projectID + "/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var tasks []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0]["title"] != "实现任务对话" {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
}

func TestMCPTokenScopesKnowledgeAndCanBeRevoked(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	project, err := st.CreateProject(ctx, domain.Project{Name: "MCP", Path: "D:\\code\\mcp"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(ctx, domain.Task{ProjectID: project.ID, Title: "Read context"})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := st.CreateKnowledge(ctx, domain.Knowledge{ProjectID: project.ID, Title: "Visible", Content: "allowed", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateKnowledge(ctx, domain.Knowledge{ProjectID: project.ID, Title: "Hidden", Content: "secret", Permission: "hidden"})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.IssueCapability(ctx, "token", project.ID, task.ID, "run-1", []string{"knowledge.read"}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(st, events.New()).Handler())
	defer srv.Close()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "knowledge.read", "arguments": map[string]any{"id": visible.ID}}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var response map[string]any
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["result"] == nil {
		t.Fatalf("expected MCP result: %#v", response)
	}
	if err = st.RevokeCapabilities(ctx, "run-1"); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected revoked token rejection, got %d", res.StatusCode)
	}
}

func TestProjectPathCannotBeAddedTwice(t *testing.T) {
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := httptest.NewServer(server.New(st, events.New()).Handler())
	defer srv.Close()
	post(t, srv.URL+"/api/v1/projects", map[string]any{"name": "One", "path": "D:\\Code\\Example"})
	body, _ := json.Marshal(map[string]any{"name": "Two", "path": "d:\\code\\example"})
	res, err := http.Post(srv.URL+"/api/v1/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", res.StatusCode)
	}
}

func post(t *testing.T, url string, body any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		t.Fatalf("POST %s: %s", url, res.Status)
	}
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

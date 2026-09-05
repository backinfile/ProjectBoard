package main

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"os"
	"os/exec"
	"projectboard/internal/domain"
	"projectboard/internal/server"
	"projectboard/internal/store"
	"projectboard/web"
	"testing"
	"time"
)

func TestStdioHelper(t *testing.T) {
	if os.Getenv("PB_STDIO_HELPER") != "1" {
		return
	}
	if e := stdio([]string{"--project", "p1", "--url", os.Getenv("PB_STDIO_URL")}); e != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestStdioRoundTrip(t *testing.T) {
	st, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	state := domain.Empty()
	state.Projects = []domain.Project{{ID: "p1", Name: "本地项目", Code: "PB", Statuses: domain.DefaultStatuses(), InitialStatusID: "created"}}
	state, e = st.Save(state)
	if e != nil {
		t.Fatal(e)
	}
	token := server.Secret()
	_, e = st.DB.Exec(`INSERT INTO tokens VALUES(?,?,?,?,?)`, "token", "p1", "stdio", store.Hash([]byte(token)), domain.Now())
	if e != nil {
		t.Fatal(e)
	}
	httpServer := httptest.NewServer(server.New(st, web.Assets()))
	defer httpServer.Close()
	testBinary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(testBinary, "-test.run=^TestStdioHelper$")
	cmd.Env = append(os.Environ(), "PB_STDIO_HELPER=1", "PB_STDIO_URL="+httpServer.URL+"/api/mcp", "PROJECTBOARD_TOKEN="+token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, nil)
	if e != nil || len(tools.Tools) < 30 {
		t.Fatal("tool parity", e)
	}
	r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_task", Arguments: map[string]any{"project_id": "p1", "expected_revision": state.Revision, "title": "通过 stdio 创建"}})
	if e != nil || r.IsError {
		t.Fatal(r, e)
	}
	state, e = st.Read()
	if e != nil || len(state.Tasks) != 1 || state.Tasks[0].Title != "通过 stdio 创建" {
		t.Fatal("stdio write not visible in Web store")
	}
	r, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_project", Arguments: map[string]any{"project_id": "different"}})
	if e == nil && !r.IsError {
		t.Fatal("stdio scope bypass")
	}
	b, _ := json.Marshal(tools)
	t.Logf("stdio tool list: %d bytes", len(b))
}

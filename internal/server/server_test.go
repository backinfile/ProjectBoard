package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"projectboard/internal/domain"
	"projectboard/internal/store"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type fixture struct {
	t      *testing.T
	s      *Server
	http   *httptest.Server
	client *http.Client
	csrf   string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	st, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s := New(st, fstest.MapFS{"index.html": {Data: []byte("ProjectBoard")}})
	srv := httptest.NewServer(s)
	jar, _ := cookiejar.New(nil)
	f := &fixture{t: t, s: s, http: srv, client: &http.Client{Jar: jar}}
	t.Cleanup(func() { srv.Close(); st.Close() })
	r, b := f.request("POST", "/api/setup", map[string]any{"token": s.SetupToken, "password": "correct-password"})
	if r.StatusCode != 200 {
		t.Fatalf("setup: %s", b)
	}
	var v map[string]string
	json.Unmarshal(b, &v)
	f.csrf = v["csrf"]
	return f
}
func (f *fixture) request(method, path string, body any) (*http.Response, []byte) {
	f.t.Helper()
	var reader io.Reader
	if b, ok := body.([]byte); ok {
		reader = bytes.NewReader(b)
	} else if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, f.http.URL+path, reader)
	req.Header.Set("X-Requested-With", "ProjectBoard")
	req.Header.Set("X-CSRF-Token", f.csrf)
	res, e := f.client.Do(req)
	if e != nil {
		f.t.Fatal(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		f.t.Fatal(e)
	}
	return res, b
}
func sample() domain.State {
	st := domain.Empty()
	st.Projects = []domain.Project{{ID: "p1", Name: "项目一", Code: "PB", Statuses: domain.DefaultStatuses(), InitialStatusID: "created"}, {ID: "p2", Name: "项目二", Code: "TWO", Statuses: domain.DefaultStatuses(), InitialStatusID: "created"}}
	st.Tasks = []domain.Task{domain.NewTask("p1", "t1", "完成安装", 1), domain.NewTask("p2", "t2", "另一个项目", 1)}
	root := "n1"
	st.Nodes = []domain.Node{{ID: "n1", Project: "p1", Name: "技术", Tags: []string{"技术"}, Links: []string{}, History: []domain.Revision{}}, {ID: "n2", Project: "p1", Name: "启动", Parent: &root, Content: "启动命令", Tags: []string{}, Links: []string{}, History: []domain.Revision{}}}
	st.Tasks[0].Knowledge = []string{"n2"}
	return st
}
func (f *fixture) seed() domain.State {
	f.t.Helper()
	st, e := f.s.Store.Save(sample())
	if e != nil {
		f.t.Fatal(e)
	}
	return st
}
func (f *fixture) call(name string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	args["project_id"] = "p1"
	if isWrite(name) {
		st, _ := f.s.Store.Read()
		args["expected_revision"] = float64(st.Revision)
	}
	v, e := f.s.Call("p1", name, args)
	if e != nil {
		return nil, e
	}
	return v.(map[string]any), nil
}
func TestAuthenticationPersistenceAndConflict(t *testing.T) {
	f := setup(t)
	st := f.seed()
	r, b := f.request("GET", "/api/state", nil)
	if r.StatusCode != 200 || !bytes.Contains(b, []byte("完成安装")) {
		t.Fatalf("read: %s", b)
	}
	st.Tasks[0].Title = "保存后重启"
	r, b = f.request("PUT", "/api/state", st)
	if r.StatusCode != 200 {
		t.Fatalf("save: %s", b)
	}
	r, _ = f.request("PUT", "/api/state", st)
	if r.StatusCode != 409 {
		t.Fatal("stale save should conflict")
	}
	next, _ := f.s.Store.Read()
	if next.Tasks[0].Title != "保存后重启" {
		t.Fatal("conflict overwrote data")
	}
	csrf := f.csrf
	f.csrf = "wrong"
	r, _ = f.request("PUT", "/api/state", next)
	if r.StatusCode != 403 {
		t.Fatal("CSRF bypass")
	}
	f.csrf = csrf
	r, _ = f.request("POST", "/api/password", map[string]string{"old": "correct-password", "password": "updated-password"})
	if r.StatusCode != 200 {
		t.Fatal("password update")
	}
	r, _ = f.request("POST", "/api/logout", nil)
	if r.StatusCode != 403 {
		t.Fatal("old CSRF must expire")
	}
	r, _ = http.Get(f.http.URL + "/api/state")
	if r.StatusCode != 401 {
		t.Fatal("anonymous access")
	}
	r.Body.Close()
	req, _ := http.NewRequest("GET", f.http.URL+"/api/session", nil)
	req.Host = "evil.example"
	r, e := f.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("Host protection")
	}
}
func TestKnowledgeAndLabelsAreTransactional(t *testing.T) {
	f := setup(t)
	f.seed()
	_, e := f.call("update_knowledge_node", map[string]any{"id": "n1", "fields": map[string]any{"name": "架构"}})
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.call("get_knowledge_node", map[string]any{"address": "架构.启动"})
	if e != nil || v["data"].(map[string]any)["id"] != "n2" {
		t.Fatalf("address: %v %v", v, e)
	}
	st, _ := f.s.Store.Read()
	if st.Tasks[0].Knowledge[0] != "n2" {
		t.Fatal("stable reference lost")
	}
	_, e = f.call("move_knowledge_node", map[string]any{"id": "n1", "parent": "n2"})
	if e == nil {
		t.Fatal("cycle allowed")
	}
	_, e = f.call("attach_labels", map[string]any{"target_type": "task", "target_ids": []string{"t1", "t2"}, "tags": []string{"重点"}})
	if e == nil {
		t.Fatal("cross project batch allowed")
	}
	st, _ = f.s.Store.Read()
	if len(st.Tasks[0].Tags) != 0 {
		t.Fatal("partial batch committed")
	}
	v, e = f.call("preview_delete", map[string]any{"node_ids": []string{"n1"}})
	if e != nil {
		t.Fatal(e)
	}
	plan := v["data"].(map[string]any)
	if plan["count"] != 2 {
		t.Fatal("subtree incomplete")
	}
	_, e = f.call("commit_delete", map[string]any{"plan_id": plan["plan_id"]})
	if e != nil {
		t.Fatal(e)
	}
	st, _ = f.s.Store.Read()
	if st.Node("n2").Deleted == "" {
		t.Fatal("child retained")
	}
	_, e = f.call("restore_knowledge_node", map[string]any{"id": "n1"})
	if e != nil {
		t.Fatal(e)
	}
	st, _ = f.s.Store.Read()
	if st.Node("n2").Deleted != "" {
		t.Fatal("restore child")
	}
}
func TestAttachmentsBackupRestoreAndReminder(t *testing.T) {
	f := setup(t)
	st := f.seed()
	a, e := f.s.Store.PutAttachment("p1", "示例.txt", "text/plain", strings.NewReader("文件内容"))
	if e != nil {
		t.Fatal(e)
	}
	st.Tasks[0].DetailAttachments = []domain.Attachment{a}
	st.Tasks[0].Notes = []domain.Comment{{ID: "comment1", Text: "", At: domain.Now(), Attachments: []domain.Attachment{a}}}
	st.Tasks[0].Reminder = time.Now().Add(-time.Minute).Format("2006-01-02T15:04")
	st.Tasks[0].Repeat = "monthly"
	st.Tasks[0].Due = "2026-01-31"
	st, e = f.s.Store.Save(st)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.Tick(); e != nil {
		t.Fatal(e)
	}
	f.s.Store.Tick()
	r, b := f.request("GET", "/api/notifications", nil)
	if r.StatusCode != 200 || bytes.Count(b, []byte(`"task"`)) != 1 {
		t.Fatalf("reminder: %s", b)
	}
	backup, e := f.s.Store.Backup()
	if e != nil {
		t.Fatal(e)
	}
	st.Tasks[0].Status = "done"
	st, e = f.s.Store.Save(st)
	if e != nil {
		t.Fatal(e)
	}
	if len(st.Tasks) != 3 || st.Tasks[2].Due != "2026-02-28" || len(st.Tasks[2].Notes) != 0 {
		t.Fatal("repeat task semantics")
	}
	restored, e := f.s.Store.Restore(backup, st.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if len(restored.Tasks) != 2 {
		t.Fatal("restore task set")
	}
	meta, raw, e := f.s.Store.Attachment("p1", restored.Tasks[0].DetailAttachments[0].ID)
	if e != nil || string(raw) != "文件内容" || meta.SHA256 != a.SHA256 {
		t.Fatal("attachment restore integrity")
	}
	if !f.s.checkPassword("correct-password") {
		t.Fatal("restore changed admin")
	}
	var malicious bytes.Buffer
	z := zip.NewWriter(&malicious)
	w, _ := z.Create("../escape")
	w.Write([]byte("oops"))
	z.Close()
	if _, e = f.s.Store.Restore(malicious.Bytes(), restored.Revision); e == nil {
		t.Fatal("zip traversal accepted")
	}
}

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}
func TestMCPProtocolTokenIsolationAndRevocation(t *testing.T) {
	f := setup(t)
	f.seed()
	r, b := f.request("POST", "/api/tokens?project=p1", map[string]string{"name": "test"})
	if r.StatusCode != 201 {
		t.Fatal(string(b))
	}
	var token map[string]string
	json.Unmarshal(b, &token)
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.http.URL + "/api/mcp", HTTPClient: &http.Client{Transport: authTransport{token["token"]}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	defs, e := session.ListTools(ctx, nil)
	if e != nil || len(defs.Tools) != len(toolNames) {
		t.Fatalf("tools: %v", e)
	}
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_project", Arguments: map[string]any{"project_id": "p1"}})
	if e != nil || result.IsError {
		t.Fatalf("call: %v %v", result, e)
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_task", Arguments: map[string]any{"project_id": "p2", "id": "t2"}})
	if e != nil || !result.IsError {
		t.Fatal("project escape")
	}
	r, _ = f.request("DELETE", "/api/tokens?project=p1&id="+token["id"], nil)
	if r.StatusCode != 200 {
		t.Fatal("revoke")
	}
	_, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_project", Arguments: map[string]any{"project_id": "p1"}})
	if e == nil {
		t.Fatal("revoked token accepted")
	}
}

func TestHTTPSCookiesAndOrigin(t *testing.T) {
	f := setup(t)
	srv := httptest.NewTLSServer(f.s)
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(`{"password":"correct-password"}`))
	req.Header.Set("X-Requested-With", "ProjectBoard")
	req.Header.Set("Origin", srv.URL)
	r, e := srv.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 200 || len(r.Cookies()) != 1 || !r.Cookies()[0].Secure || !r.Cookies()[0].HttpOnly {
		t.Fatal("HTTPS session flags")
	}
	req, _ = http.NewRequest("POST", srv.URL+"/api/login", strings.NewReader(`{"password":"correct-password"}`))
	req.Header.Set("X-Requested-With", "ProjectBoard")
	req.Header.Set("Origin", "https://different.example")
	r, e = srv.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("cross origin accepted")
	}
}

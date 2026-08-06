package runnercli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestLocalAgentDefaultsToCodexAndCanBeChanged(t *testing.T) {
	dir := t.TempDir()
	stateData, _ := json.Marshal(state{Server: "https://projectboard.example.com", Token: "secret", DeviceID: "device-1"})
	if err := os.WriteFile(filepath.Join(dir, "runner-state.json"), stateData, 0o600); err != nil {
		t.Fatal(err)
	}

	app := New(Options{ConfigDir: dir, LookPath: func(name string) (string, error) {
		return filepath.Join("tools", name), nil
	}})
	info, err := app.Info()
	if err != nil || info.LocalAgent != "codex" {
		t.Fatalf("Info() = %+v, %v", info, err)
	}
	if err := app.SetLocalAgent("opencode"); err != nil {
		t.Fatal(err)
	}
	info, err = app.Info()
	if err != nil || info.LocalAgent != "opencode" {
		t.Fatalf("Info() after selection = %+v, %v", info, err)
	}
}

func TestSetLocalAgentRejectsUnsupportedOrMissingCLI(t *testing.T) {
	app := New(Options{ConfigDir: t.TempDir(), LookPath: func(name string) (string, error) {
		return "", os.ErrNotExist
	}})
	if err := app.SetLocalAgent("unknown"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("SetLocalAgent(unknown) error = %v", err)
	}
	if err := app.SetLocalAgent("codex"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("SetLocalAgent(codex) error = %v", err)
	}
}

func TestLocalAgentCommandUsesSelectedAdapter(t *testing.T) {
	app := New(Options{ConfigDir: t.TempDir(), LookPath: func(name string) (string, error) {
		return `C:\\tools\\` + name + `.exe`, nil
	}})

	codex, err := app.LocalAgentCommand("codex", `D:\\repo`, "Implement PB-12")
	if err != nil {
		t.Fatal(err)
	}
	wantCodex := []string{"--ask-for-approval", "never", "exec", "--sandbox", "workspace-write", "-C", `D:\\repo`, "--", "Implement PB-12"}
	if codex.Path != `C:\\tools\\codex.exe` || !reflect.DeepEqual(codex.Args, wantCodex) {
		t.Fatalf("codex command = %+v", codex)
	}

	openCode, err := app.LocalAgentCommand("opencode", `D:\\repo`, "Implement PB-12")
	if err != nil {
		t.Fatal(err)
	}
	if openCode.Path != `C:\\tools\\opencode.exe` || openCode.Dir != `D:\\repo` || !reflect.DeepEqual(openCode.Args, []string{"run", "--auto", "--dir", `D:\\repo`, "--", "Implement PB-12"}) {
		t.Fatalf("opencode command = %+v", openCode)
	}
}

func TestAppConnectPauseResumeAndPoll(t *testing.T) {
	var pollCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/pair":
			_ = json.NewEncoder(w).Encode(map[string]string{"agentToken": "secret", "deviceId": "device-1"})
		case "/api/agent/poll":
			if got := r.Header.Get("Authorization"); got != "Bearer secret" {
				t.Errorf("Authorization = %q", got)
			}
			pollCount++
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "idle"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("PROJECTBOARD_URL", server.URL)

	var output bytes.Buffer
	app := New(Options{ConfigDir: t.TempDir(), Out: &output, Err: &output})
	if err := app.Run(context.Background(), []string{"connect", "PB-TEST"}); err != nil {
		t.Fatal(err)
	}
	info, err := app.Info()
	if err != nil || !info.Paired || info.ServerURL != server.URL {
		t.Fatalf("Info() = %+v, %v", info, err)
	}

	if err := app.Run(context.Background(), []string{"pause"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"poll"}); err != nil {
		t.Fatal(err)
	}
	if pollCount != 0 {
		t.Fatalf("paused runner polled %d times", pollCount)
	}
	if err := app.Run(context.Background(), []string{"resume"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background(), []string{"poll"}); err != nil {
		t.Fatal(err)
	}
	if pollCount != 1 {
		t.Fatalf("resumed runner polled %d times", pollCount)
	}
}

func TestConnectUsesExplicitServerAndAgentKey(t *testing.T) {
	var receivedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/pair" {
			http.NotFound(w, r)
			return
		}
		var input struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		receivedKey = input.Code
		_ = json.NewEncoder(w).Encode(map[string]string{"agentToken": "agent-token", "deviceId": "device-2"})
	}))
	defer server.Close()

	app := New(Options{ConfigDir: t.TempDir()})
	if err := app.Connect(context.Background(), server.URL+"/", "  PB-AGENT-KEY  "); err != nil {
		t.Fatal(err)
	}
	if receivedKey != "PB-AGENT-KEY" {
		t.Fatalf("Agent Key = %q", receivedKey)
	}
	info, err := app.Info()
	if err != nil || !info.Paired || info.ServerURL != server.URL {
		t.Fatalf("Info() = %+v, %v", info, err)
	}
}

func TestConnectRejectsInvalidInputWithoutCallingServer(t *testing.T) {
	app := New(Options{ConfigDir: t.TempDir()})
	for _, test := range []struct {
		name   string
		server string
		key    string
	}{
		{name: "missing address", server: "", key: "PB-KEY"},
		{name: "unsupported scheme", server: "file:///tmp/projectboard", key: "PB-KEY"},
		{name: "credentials in address", server: "https://user:password@example.com", key: "PB-KEY"},
		{name: "missing key", server: "https://projectboard.example.com", key: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := app.Connect(context.Background(), test.server, test.key); err == nil {
				t.Fatal("Connect() succeeded, want error")
			}
		})
	}
}

func TestConnectReturnsProjectBoardErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "PAIRING_CODE_INVALID", "message": "Agent Key is invalid or expired"}})
	}))
	defer server.Close()

	err := New(Options{ConfigDir: t.TempDir()}).Connect(context.Background(), server.URL, "PB-INVALID")
	if err == nil || err.Error() != "Agent Key is invalid or expired" {
		t.Fatalf("Connect() error = %v", err)
	}
}

func TestWatchStopsPromptlyWhenContextIsCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "idle"})
	}))
	defer server.Close()

	dir := t.TempDir()
	stateData, _ := json.Marshal(state{Server: server.URL, Token: "secret", DeviceID: "device-1"})
	if err := os.WriteFile(filepath.Join(dir, "runner-state.json"), stateData, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var statuses []PollStatus
	app := New(Options{ConfigDir: dir, OnPoll: func(event PollEvent) {
		mu.Lock()
		statuses = append(statuses, event.Status)
		mu.Unlock()
		if event.Status == PollReady {
			cancel()
		}
	}})
	if err := app.Run(ctx, []string{"poll", "--watch"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []PollStatus{PollStarting, PollReady, PollStopped}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %v, want %v", statuses, want)
	}
}

func TestRunReturnsUsageError(t *testing.T) {
	err := New(Options{ConfigDir: t.TempDir()}).Run(context.Background(), nil)
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("Run() error = %T %v", err, err)
	}
}

package agentexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
)

type fakeRunner struct {
	results chan Result
	calls   chan Invocation
}

func TestCloneArgsUseRemoteDefaultBranch(t *testing.T) {
	args := cloneArgs("https://github.com/acme/repo.git", `C:\work\PB-1`)
	want := []string{"clone", "https://github.com/acme/repo.git", `C:\work\PB-1`}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("clone args = %#v, want %#v", args, want)
	}
}

func TestExecArgsAllowTaskAgentToCommit(t *testing.T) {
	fresh := execArgs(Invocation{WorkDir: `C:\work\PB-1`}, `C:\schema.json`)
	resume := execArgs(Invocation{WorkDir: `C:\work\PB-1`, ThreadID: "thread-1"}, `C:\schema.json`)
	for name, args := range map[string][]string{"fresh": fresh, "resume": resume} {
		joined := strings.Join(args, "\x00")
		if strings.Contains(joined, "workspace-write") {
			t.Fatalf("%s invocation still blocks .git metadata writes: %#v", name, args)
		}
		if !strings.Contains(joined, "danger-full-access") {
			t.Fatalf("%s invocation does not allow task commits: %#v", name, args)
		}
	}
}

func (f *fakeRunner) Run(_ context.Context, in Invocation) (Result, error) {
	f.calls <- in
	return <-f.results, nil
}

func TestLocalAgentPausesAfterInitialPlan(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES('p','PB','Project','',?,?)", stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec("INSERT INTO agents(id,name,purpose,created_at) VALUES('a','Codex','Local',?)", stamp)
	if err != nil {
		t.Fatal(err)
	}
	queue := workqueue.New(db)
	item, err := queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, workqueue.CreateInput{ProjectID: "p", Title: "Plan me", IsAgentTask: true, PauseAfterPlan: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(dir, "workspaces", "pb-1", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: make(chan Result, 2), calls: make(chan Invocation, 2)}
	runner.results <- Result{ThreadID: "thread-1", Status: "planned", Message: "Implementation plan"}
	runner.results <- Result{ThreadID: "thread-1", Status: "paused", Message: "Waiting for input"}
	module := New(db, queue, Options{DataDir: dir, Runner: runner, PollInterval: 10 * time.Millisecond})
	if err = module.Start(); err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	select {
	case call := <-runner.calls:
		if !call.PlanOnly {
			t.Fatal("first turn was not plan-only")
		}
		if !strings.Contains(call.Prompt, "Development branch: main") {
			t.Fatalf("prompt does not identify the development branch: %s", call.Prompt)
		}
		if !strings.Contains(call.Prompt, "merge the work branch back into the development branch") {
			t.Fatalf("prompt does not explain the default branch workflow: %s", call.Prompt)
		}
		if !strings.Contains(call.Prompt, "explicit instruction instead") {
			t.Fatalf("prompt does not give explicit task instructions precedence: %s", call.Prompt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Agent did not receive task")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		item, err = queue.Get(t.Context(), item.ID)
		if err == nil && item.AgentState == "paused_plan" {
			if item.CodexThreadID == nil || *item.CodexThreadID != "thread-1" {
				t.Fatalf("thread = %#v", item.CodexThreadID)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if item.AgentState != "paused_plan" {
		t.Fatalf("agent state = %s", item.AgentState)
	}
	item, err = queue.AddMessageWithResume(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, item.ID, "Continue", item.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	module.Wake()
	select {
	case call := <-runner.calls:
		if call.PlanOnly || call.ThreadID != "thread-1" {
			t.Fatalf("resume invocation = %+v", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Agent session was not resumed")
	}
}

func TestWorkspacePreparationFailureReleasesAgentCapacity(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	missingRepository := "file:///" + filepath.ToSlash(filepath.Join(dir, "missing.git"))
	if _, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES('p','PB','Project',?,?,?)", missingRepository, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO agents(id,name,purpose,created_at) VALUES('a','Codex','Local',?)", stamp); err != nil {
		t.Fatal(err)
	}
	queue := workqueue.New(db)
	item, err := queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, workqueue.CreateInput{ProjectID: "p", Title: "Cannot clone", IsAgentTask: true})
	if err != nil {
		t.Fatal(err)
	}
	module := New(db, queue, Options{DataDir: dir, Runner: &fakeRunner{results: make(chan Result), calls: make(chan Invocation)}, PollInterval: 10 * time.Millisecond})
	if err = module.Start(); err != nil {
		t.Fatal(err)
	}
	defer module.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		item, err = queue.Get(t.Context(), item.ID)
		if err == nil && item.AgentState == "paused_failure" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if item.AgentState != "paused_failure" {
		t.Fatalf("agent state = %s", item.AgentState)
	}
	var state string
	var endedAt, errorMessage *string
	if err = db.DB.QueryRow("SELECT state,ended_at,error_message FROM agent_executions WHERE work_item_id=?", item.ID).Scan(&state, &endedAt, &errorMessage); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || endedAt == nil || errorMessage == nil {
		t.Fatalf("failed execution was not finalized: state=%s endedAt=%v error=%v", state, endedAt, errorMessage)
	}
}

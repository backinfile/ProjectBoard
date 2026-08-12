package agentexec

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/projectrepo"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
)

type fakeRunner struct {
	results chan Result
	calls   chan Invocation
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

func TestAgentTagRulesRejectBeforeAcceptAndReevaluateEveryClaim(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	projectPath := filepath.Join(dir, "source")
	if _, err = projectrepo.Prepare(t.Context(), projectPath); err != nil {
		t.Fatal(err)
	}
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project',?,?,?)", projectPath, stamp, stamp)
	_, _ = db.DB.Exec("INSERT INTO agents(id,name,purpose,accept_tags_json,reject_tags_json,created_at) VALUES('a','Frontend','UI','[\"frontend\"]','[\"blocked\"]',?)", stamp)
	queue := workqueue.New(db)
	item, err := queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, workqueue.CreateInput{ProjectID: "p", Title: "Tagged", IsAgentTask: true, Tags: []string{"frontend", "blocked"}})
	if err != nil {
		t.Fatal(err)
	}
	module := New(db, queue, Options{DataDir: dir})
	claim, err := module.claim()
	if err != nil || claim != nil {
		t.Fatalf("rejected task was claimed: claim=%+v err=%v", claim, err)
	}
	item, err = queue.Update(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, item.ID, workqueue.UpdateInput{ExpectedVersion: item.Version, Configure: true, IsAgentTask: true, Tags: []string{"frontend"}})
	if err != nil {
		t.Fatal(err)
	}
	claim, err = module.claim()
	if err != nil || claim == nil || claim.AgentID != "a" {
		t.Fatalf("accepted task was not claimed: claim=%+v err=%v", claim, err)
	}
}

func TestCompletedStandardTaskCanBeClaimedForMerge(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	projectPath := filepath.Join(dir, "source")
	if _, err = projectrepo.Prepare(t.Context(), projectPath); err != nil {
		t.Fatal(err)
	}
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project',?,?,?)", projectPath, stamp, stamp)
	_, _ = db.DB.Exec("INSERT INTO agents(id,name,purpose,created_at) VALUES('a','Codex','Local',?)", stamp)
	queue := workqueue.New(db)
	item, err := queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, workqueue.CreateInput{ProjectID: "p", Title: "Merge me", IsAgentTask: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("UPDATE work_items SET stage='completed',agent_phase='merge',agent_state='queued',completed_at=? WHERE id=?", stamp, item.ID); err != nil {
		t.Fatal(err)
	}
	claim, err := New(db, queue, Options{DataDir: dir}).claim()
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || claim.ItemID != item.ID || claim.Phase != "merge" || claim.Workspace != projectPath {
		t.Fatalf("merge claim = %+v", claim)
	}
}

func TestStandardCompletionEntersCompletedStageAndQueuesSeparateMerge(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	projectPath := filepath.Join(dir, "source")
	if _, err = projectrepo.Prepare(t.Context(), projectPath); err != nil {
		t.Fatal(err)
	}
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project',?,?,?)", projectPath, stamp, stamp)
	_, _ = db.DB.Exec("INSERT INTO agents(id,name,purpose,created_at) VALUES('a','Codex','Local',?)", stamp)
	queue := workqueue.New(db)
	item, err := queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, workqueue.CreateInput{ProjectID: "p", Title: "Complete then merge", IsAgentTask: true})
	if err != nil {
		t.Fatal(err)
	}
	module := New(db, queue, Options{DataDir: dir})
	workClaim, err := module.claim()
	if err != nil || workClaim == nil {
		t.Fatalf("work claim = %+v, err = %v", workClaim, err)
	}
	if err = module.prepare(t.Context(), workClaim); err != nil {
		t.Fatal(err)
	}
	result := Result{ThreadID: "thread-1", Status: "completed", Message: "Ready"}
	module.recordResult(workClaim, result, result.Status, nil)
	module.finish(workClaim, result)
	item, err = queue.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Stage != "completed" || item.AgentPhase != "merge" || item.AgentState != "queued" || item.CompletedAt == nil {
		t.Fatalf("completed item = %+v", item)
	}
	mergeClaim, err := module.claim()
	if err != nil || mergeClaim == nil {
		t.Fatalf("merge claim = %+v, err = %v", mergeClaim, err)
	}
	if mergeClaim.Phase != "merge" || mergeClaim.ThreadID != "thread-1" || mergeClaim.Workspace != projectPath {
		t.Fatalf("separate merge claim = %+v", mergeClaim)
	}
}

func TestLocalAgentPausesAfterInitialPlan(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	projectPath := filepath.Join(dir, "source")
	if _, err = projectrepo.Prepare(t.Context(), projectPath); err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project',?,?,?)", projectPath, stamp, stamp)
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
		if !strings.Contains(call.Prompt, "Target branch: main") || !strings.Contains(call.Prompt, "Workflow: standard") {
			t.Fatalf("prompt does not identify the local workflow: %s", call.Prompt)
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
	missingRepository := filepath.Join(dir, "missing")
	if _, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project',?,?,?)", missingRepository, stamp, stamp); err != nil {
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

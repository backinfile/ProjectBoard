package agentexec

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/agentrequest"
	"github.com/projectboard/projectboard/internal/knowledge"
	"github.com/projectboard/projectboard/internal/projectrepo"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
)

type fakeRunner struct {
	results chan Result
	calls   chan Invocation
}

func TestExecArgsAlwaysStartAFreshWritableSession(t *testing.T) {
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
		if strings.Contains(joined, "resume") {
			t.Fatalf("%s invocation reused an old session: %#v", name, args)
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
	projectPath := filepath.Join(dir, "project")
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
	item, err := queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, workqueue.CreateInput{ProjectID: "p", Title: "Plan me"})
	if err != nil {
		t.Fatal(err)
	}
	requests := agentrequest.New(db)
	request, err := requests.Create(t.Context(), agentrequest.CreateInput{ProjectID: "p", Kind: "task_plan", SourceWorkItemID: item.ID, Title: "Plan task", CreatedByType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: make(chan Result, 2), calls: make(chan Invocation, 2)}
	runner.results <- Result{ThreadID: "thread-1", Status: "planned", Message: "Implementation plan"}
	runner.results <- Result{ThreadID: "thread-1", Status: "paused", Message: "Waiting for input"}
	module := New(db, queue, requests, knowledge.New(db), Options{DataDir: dir, Runner: runner, PollInterval: 10 * time.Millisecond})
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
		request, err = requests.Get(t.Context(), request.ID)
		if err == nil && request.Status == "succeeded" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if request.Status != "succeeded" {
		t.Fatalf("request status = %s", request.Status)
	}
	plan := ""
	if request.FinalMessage != nil {
		plan = *request.FinalMessage
	}
	_, err = requests.Create(t.Context(), agentrequest.CreateInput{ProjectID: "p", Kind: "task_execution", SourceWorkItemID: item.ID, Title: "Execute task", PromptMarkdown: plan, CreatedByType: "human", CreatedByID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	module.Wake()
	select {
	case call := <-runner.calls:
		if call.PlanOnly || call.ThreadID != "" {
			t.Fatalf("fresh execution invocation = %+v", call)
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
	item, err := queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "u"}, workqueue.CreateInput{ProjectID: "p", Title: "Cannot clone"})
	if err != nil {
		t.Fatal(err)
	}
	requests := agentrequest.New(db)
	request, err := requests.Create(t.Context(), agentrequest.CreateInput{ProjectID: "p", Kind: "task_execution", SourceWorkItemID: item.ID, Title: "Cannot clone", CreatedByType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	module := New(db, queue, requests, knowledge.New(db), Options{DataDir: dir, Runner: &fakeRunner{results: make(chan Result), calls: make(chan Invocation)}, PollInterval: 10 * time.Millisecond})
	if err = module.Start(); err != nil {
		t.Fatal(err)
	}
	defer module.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		request, err = requests.Get(t.Context(), request.ID)
		if err == nil && request.Status == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if request.Status != "failed" {
		t.Fatalf("request status = %s", request.Status)
	}
	if request.EndedAt == nil || request.ErrorMessage == nil {
		t.Fatalf("failed request was not finalized: %#v", request)
	}
}

func TestKnowledgeRequestAppliesStructuredOperations(t *testing.T) {
	dir := t.TempDir()
	database, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = database.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,knowledge_compaction_days,knowledge_compaction_request_count,created_at,updated_at) VALUES('p','KB','Knowledge',?,0,0,?,?)", filepath.Join(dir, "knowledge-project"), stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec("INSERT INTO agents(id,name,purpose,created_at) VALUES('a','Codex','Local',?)", stamp)
	if err != nil {
		t.Fatal(err)
	}
	knowledgeModule := knowledge.New(database)
	node, err := knowledgeModule.Create(t.Context(), knowledge.Actor{Type: "human", ID: "u"}, knowledge.CreateInput{ProjectID: "p", Title: "API", Markdown: "old"})
	if err != nil {
		t.Fatal(err)
	}
	requests := agentrequest.New(database)
	request, err := requests.Create(t.Context(), agentrequest.CreateInput{ProjectID: "p", Kind: "task_knowledge", Title: "Update knowledge", CreatedByType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: make(chan Result, 1), calls: make(chan Invocation, 1)}
	runner.results <- Result{ThreadID: "fresh-thread", Status: "completed", Message: "Updated knowledge", KnowledgeOperations: []knowledge.Operation{{Type: "update", NodeID: node.ID, ExpectedVersion: node.Version, Title: node.Title, Markdown: "new"}}}
	module := New(database, workqueue.New(database), requests, knowledgeModule, Options{DataDir: dir, Runner: runner, PollInterval: 10 * time.Millisecond})
	if err = module.Start(); err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	select {
	case invocation := <-runner.calls:
		if invocation.ThreadID != "" || !strings.Contains(invocation.Prompt, "locked_for_agents") {
			t.Fatalf("knowledge invocation = %#v", invocation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("knowledge request was not executed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		request, _ = requests.Get(t.Context(), request.ID)
		if request.Status == "succeeded" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	node, _ = knowledgeModule.Get(t.Context(), node.ID)
	if request.Status != "succeeded" || node.Markdown != "new" || node.Version != 2 {
		t.Fatalf("request=%#v node=%#v", request, node)
	}
}

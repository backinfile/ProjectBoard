package workqueue

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/store"
)

func TestTaskTagsAreNormalizedAndUpdated(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	q := New(db)
	item, err := q.Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Tagged task", Tags: []string{" frontend ", "Urgent", "FRONTEND", ""}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(item.Tags, []string{"frontend", "Urgent"}) {
		t.Fatalf("created tags = %#v", item.Tags)
	}
	item, err = q.Update(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, UpdateInput{ExpectedVersion: item.Version, Configure: true, Tags: []string{"backend"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(item.Tags, []string{"backend"}) {
		t.Fatalf("updated tags = %#v", item.Tags)
	}
}

func TestAgentPauseFlagsAreClearedForHumanTask(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	item, err := New(db).Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Human", PauseAfterPlan: true, PauseBeforeCompletion: true})
	if err != nil {
		t.Fatal(err)
	}
	if item.IsAgentTask || item.PauseAfterPlan || item.PauseBeforeCompletion {
		t.Fatalf("unexpected agent settings: %+v", item)
	}
}

func TestTaskCreationTimelineIsAttributedToSystem(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp); err != nil {
		t.Fatal(err)
	}
	item, err := New(db).Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Task"})
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Conversation) != 1 || item.Conversation[0]["kind"] != "stage_transition" || item.Conversation[0]["author_type"] != "system" || item.Conversation[0]["author_id"] != nil {
		t.Fatalf("creation timeline attribution = %#v, want a system stage transition", item.Conversation)
	}
}

func TestTaskDevelopmentBranchIsNotRestrictedByProjectAllowlist(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	q := New(db)
	item, err := q.Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Feature", TargetBranch: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if item.TargetBranch != "dev" {
		t.Fatalf("created target branch = %q", item.TargetBranch)
	}
	item, err = q.Update(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, UpdateInput{ExpectedVersion: item.Version, TargetBranch: "release/v2"})
	if err != nil {
		t.Fatal(err)
	}
	if item.TargetBranch != "release/v2" {
		t.Fatalf("updated target branch = %q", item.TargetBranch)
	}
}

func TestClosedTaskRejectsMessages(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp)
	q := New(db)
	item, _ := q.Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Task"})
	item, _ = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "in_progress", "")
	item, _ = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "completed", "")
	item, _ = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "closed", "")
	if _, err = q.AddMessage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, "late", item.Version); err == nil {
		t.Fatal("closed task accepted a message")
	}
}

func TestSimpleConversationWorkflowSkipsCompletedStage(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp)
	q := New(db)
	item, err := q.Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Conversation", WorkflowType: "simple_conversation"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "in_progress", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "completed", ""); err == nil {
		t.Fatal("simple conversation entered completed stage")
	}
	item, err = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "closed", "")
	if err != nil || item.Stage != "closed" {
		t.Fatalf("simple conversation did not close directly: item=%+v err=%v", item, err)
	}
}

func TestContinueFromMergeReviewReturnsTaskToProgress(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp)
	q := New(db)
	item, err := q.Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Review", IsAgentTask: true, PauseBeforeCompletion: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("UPDATE work_items SET stage='completed',completed_at=?,agent_phase='merge_review',agent_state='paused_completion' WHERE id=?", stamp, item.ID); err != nil {
		t.Fatal(err)
	}
	item, err = q.Get(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, err = q.AgentAction(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "continue_work", "Please revise")
	if err != nil {
		t.Fatal(err)
	}
	if item.Stage != "in_progress" || item.AgentPhase != "work" || item.AgentState != "queued" || item.CompletedAt != nil {
		t.Fatalf("continued item = %+v", item)
	}
}

func TestStandardAgentTaskCannotBypassMergeByClosingCompletedStage(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp)
	q := New(db)
	item, err := q.Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Must merge", IsAgentTask: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("UPDATE work_items SET stage='completed',agent_phase='merge_review',agent_state='paused_completion' WHERE id=?", item.ID); err != nil {
		t.Fatal(err)
	}
	item, _ = q.Get(t.Context(), item.ID)
	if _, err = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "closed", "skip merge"); err == nil {
		t.Fatal("standard Agent task closed without completing its merge")
	}
}

func TestPausedAgentRequiresExplicitResumeMessage(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PB','Project','C:\\repos\\p',?,?)", stamp, stamp)
	q := New(db)
	item, err := q.Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Agent task", IsAgentTask: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("UPDATE work_items SET stage='in_progress',agent_state='paused_plan' WHERE id=?", item.ID); err != nil {
		t.Fatal(err)
	}
	item, err = q.AddMessage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, "Additional context", item.Version)
	if err != nil {
		t.Fatal(err)
	}
	if item.AgentState != "paused_plan" || item.ResumeRequested {
		t.Fatalf("ordinary message resumed the Agent: %+v", item)
	}
	item, err = q.AddMessageWithResume(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, "Continue execution", item.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if item.AgentState != "queued" || !item.ResumeRequested || item.Stage != "in_progress" {
		t.Fatalf("explicit resume did not queue the Agent: %+v", item)
	}
}

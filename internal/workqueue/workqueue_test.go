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
	_, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES('p','PB','Project','',?,?)", stamp, stamp)
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
	_, err = db.DB.Exec("INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES('p','PB','Project','',?,?)", stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	item, err := New(db).Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Human", PauseAfterPlan: true, PauseAfterCompletion: true})
	if err != nil {
		t.Fatal(err)
	}
	if item.IsAgentTask || item.PauseAfterPlan || item.PauseAfterCompletion {
		t.Fatalf("unexpected agent settings: %+v", item)
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
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES('p','PB','Project','',?,?)", stamp, stamp)
	q := New(db)
	item, _ := q.Create(t.Context(), Actor{Type: "human", ID: "u"}, CreateInput{ProjectID: "p", Title: "Task"})
	item, _ = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "in_progress", "")
	item, _ = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "completed", "")
	item, _ = q.MoveStage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, item.Version, "closed", "")
	if _, err = q.AddMessage(t.Context(), Actor{Type: "human", ID: "u"}, item.ID, "late", item.Version); err == nil {
		t.Fatal("closed task accepted a message")
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
	_, _ = db.DB.Exec("INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES('p','PB','Project','',?,?)", stamp, stamp)
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

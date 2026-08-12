package knowledge

import (
	"path/filepath"
	"testing"

	"github.com/projectboard/projectboard/internal/store"
)

func TestAgentLocksVersioningAndRevisionRestore(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.DB.Exec(`INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('project-1','know','Knowledge','C:/knowledge','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	module := New(database)
	human := Actor{Type: "human", ID: "user-1"}
	agent := Actor{Type: "agent", ID: "agent-1"}
	parent, err := module.Create(t.Context(), human, CreateInput{ProjectID: "project-1", Title: "Architecture", Markdown: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := module.Create(t.Context(), human, CreateInput{ProjectID: "project-1", ParentID: parent.ID, Title: "API", Markdown: "child"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err = module.SetLock(t.Context(), human, parent.ID, parent.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = module.Update(t.Context(), agent, UpdateInput{ID: parent.ID, ExpectedVersion: parent.Version, Title: parent.Title, Markdown: "agent overwrite"}); !IsCode(err, "NODE_LOCKED") {
		t.Fatalf("agent update locked node error = %v", err)
	}
	if _, err = module.Create(t.Context(), agent, CreateInput{ProjectID: "project-1", ParentID: parent.ID, Title: "Allowed child"}); err != nil {
		t.Fatalf("agent could not create under locked node: %v", err)
	}
	child, err = module.Update(t.Context(), human, UpdateInput{ID: child.ID, ExpectedVersion: child.Version, Title: child.Title, Markdown: "child v2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = module.Update(t.Context(), human, UpdateInput{ID: child.ID, ExpectedVersion: 1, Title: child.Title, Markdown: "stale"}); !IsCode(err, "VERSION_CONFLICT") {
		t.Fatalf("stale update error = %v", err)
	}
	restored, err := module.Restore(t.Context(), human, child.ID, child.Version, 1)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Markdown != "child" || restored.Version != 3 {
		t.Fatalf("restored node = %#v", restored)
	}
	revisions, err := module.Revisions(t.Context(), child.ID)
	if err != nil || len(revisions) != 3 {
		t.Fatalf("revisions = %#v, err=%v", revisions, err)
	}
}

func TestAgentOperationsAreAtomicAndCannotDeleteLockedSubtrees(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.DB.Exec(`INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('project-1','atomic','Atomic','C:/atomic','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	module := New(database)
	human := Actor{Type: "human", ID: "user-1"}
	agent := Actor{Type: "agent", ID: "agent-1"}
	locked, _ := module.Create(t.Context(), human, CreateInput{ProjectID: "project-1", Title: "Protected", Markdown: "original"})
	child, _ := module.Create(t.Context(), human, CreateInput{ProjectID: "project-1", ParentID: locked.ID, Title: "Child", Markdown: "before"})
	locked, _ = module.SetLock(t.Context(), human, locked.ID, locked.Version, true)
	_, err = module.Apply(t.Context(), agent, "project-1", []Operation{
		{Type: "update", NodeID: child.ID, ExpectedVersion: child.Version, Title: child.Title, Markdown: "must roll back"},
		{Type: "update", NodeID: locked.ID, ExpectedVersion: locked.Version, Title: locked.Title, Markdown: "blocked"},
	})
	if !IsCode(err, "NODE_LOCKED") {
		t.Fatalf("batch error = %v", err)
	}
	child, _ = module.Get(t.Context(), child.ID)
	if child.Markdown != "before" || child.Version != 1 {
		t.Fatalf("partial Agent write escaped rollback: %#v", child)
	}
	if err = module.Delete(t.Context(), agent, locked.ID, locked.Version); !IsCode(err, "NODE_LOCKED") {
		t.Fatalf("delete locked subtree error = %v", err)
	}
}

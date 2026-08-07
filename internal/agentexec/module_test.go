package agentexec

import (
	"path/filepath"
	"testing"

	"github.com/projectboard/projectboard/internal/security"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workqueue"
)

func TestListTasksPrefersAssignedThenPriorityAndHonorsUnassignedGrant(t *testing.T) {
	database, module, queue, agentID, projectID := fixture(t, true)
	defer database.Close()

	unassignedLow := createTask(t, queue, projectID, "unassigned low", "low", "", "")
	unassignedUrgent := createTask(t, queue, projectID, "unassigned urgent", "urgent", "", "")
	assigned := createTask(t, queue, projectID, "assigned medium", "medium", "agent", agentID)

	tasks, err := module.ListTasks(t.Context(), agentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 || tasks[0].ID != assigned.ID || tasks[1].ID != unassignedUrgent.ID || tasks[2].ID != unassignedLow.ID {
		t.Fatalf("unexpected task order: %#v", tasks)
	}

	if _, err = database.DB.Exec("UPDATE agent_project_grants SET allow_unassigned_claim=0 WHERE agent_id=? AND project_id=?", agentID, projectID); err != nil {
		t.Fatal(err)
	}
	tasks, err = module.ListTasks(t.Context(), agentID)
	if err != nil || len(tasks) != 1 || tasks[0].ID != assigned.ID {
		t.Fatalf("unassigned grant was not enforced: %#v, %v", tasks, err)
	}
}

func TestClaimIsAtomicAndLimitsAgentToOneLease(t *testing.T) {
	database, module, queue, agentID, projectID := fixture(t, true)
	defer database.Close()
	first := createTask(t, queue, projectID, "first", "high", "", "")
	second := createTask(t, queue, projectID, "second", "medium", "", "")

	execution, err := module.Claim(t.Context(), agentID, first.ID, first.Version)
	if err != nil {
		t.Fatal(err)
	}
	if execution.TaskBranch != "projectboard/test/1" || execution.RepositoryName != "acme/repo" {
		t.Fatalf("unexpected execution: %#v", execution)
	}
	if _, err = module.Claim(t.Context(), agentID, second.ID, second.Version); err == nil {
		t.Fatal("second active task was claimed")
	}
	if err = module.Release(t.Context(), agentID, execution.ID, execution.LeaseID, "done", false); err != nil {
		t.Fatal(err)
	}
	if _, err = module.Claim(t.Context(), agentID, second.ID, second.Version); err != nil {
		t.Fatalf("task was not claimable after release: %v", err)
	}
}

func fixture(t *testing.T, allowUnassigned bool) (*store.Store, *Module, *workqueue.Module, string, string) {
	t.Helper()
	database, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	queue := workqueue.New(database)
	stamp, agentID, projectID := now(), security.Token(18), security.Token(18)
	_, err = database.DB.Exec("INSERT INTO agents(id,name,purpose,status,created_at) VALUES(?,?,'test','active',?)", agentID, "agent-"+agentID[:6], stamp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec("INSERT INTO projects(id,project_key,name,repository_url,created_at,updated_at) VALUES(?,'test','Test','https://github.com/acme/repo.git',?,?)", projectID, stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	allow := 0
	if allowUnassigned {
		allow = 1
	}
	_, err = database.DB.Exec("INSERT INTO agent_project_grants(agent_id,project_id,allow_unassigned_claim,created_at) VALUES(?,?,?,?)", agentID, projectID, allow, stamp)
	if err != nil {
		t.Fatal(err)
	}
	authorizationID := security.Token(18)
	_, err = database.DB.Exec("INSERT INTO provider_authorizations(id,provider,name,base_url,installation_id,encrypted_secret,status,created_at,updated_at) VALUES(?,'github','GitHub','https://github.com','99','sealed','active',?,?)", authorizationID, stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec("INSERT INTO project_repository_grants(id,project_id,authorization_id,repository_id,repository_name,clone_url,default_branch,access_level,approved_by,approved_at) VALUES(?,?,?,'42','acme/repo','https://github.com/acme/repo.git','main','write','admin',?)", security.Token(18), projectID, authorizationID, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return database, New(database, queue), queue, agentID, projectID
}

func createTask(t *testing.T, queue *workqueue.Module, projectID, title, priority, kind, assigneeID string) *workqueue.WorkItem {
	t.Helper()
	item, err := queue.Create(t.Context(), workqueue.Actor{Type: "human", ID: "admin"}, workqueue.CreateInput{ProjectID: projectID, Title: title, AcceptanceCriteriaMarkdown: "done", Priority: priority, AssigneeKind: kind, AssigneeID: assigneeID})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

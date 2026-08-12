package agentrequest

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/store"
)

func TestCompactionSchedulesAtConfiguredCountWithoutDuplicates(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "requests.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.DB.Exec(`INSERT INTO projects(id,project_key,name,project_path,knowledge_compaction_days,knowledge_compaction_request_count,created_at,updated_at) VALUES('p','KB','Knowledge','C:/knowledge',0,2,'now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	module := New(database)
	module.now = func() time.Time { return time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC) }
	for index := 0; index < 2; index++ {
		request, createErr := module.Create(t.Context(), CreateInput{ProjectID: "p", Kind: "task_knowledge", Title: "Task knowledge", CreatedByType: "system"})
		if createErr != nil {
			t.Fatal(createErr)
		}
		_, _ = database.DB.Exec("UPDATE agent_requests SET status='succeeded',ended_at=? WHERE id=?", module.now().Add(time.Duration(index)*time.Minute).Format(time.RFC3339Nano), request.ID)
	}
	compaction, err := module.MaybeScheduleCompaction(t.Context(), "p")
	if err != nil || compaction == nil || compaction.Kind != "project_knowledge_compaction" {
		t.Fatalf("compaction=%#v err=%v", compaction, err)
	}
	duplicate, err := module.MaybeScheduleCompaction(t.Context(), "p")
	if err != nil || duplicate != nil {
		t.Fatalf("duplicate compaction=%#v err=%v", duplicate, err)
	}
}

func TestApprovingPlanCreatesFreshExecutionRequest(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "requests.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.DB.Exec(`INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','PLAN','Plan','C:/plan', 'now','now');
		INSERT INTO work_items(id,project_id,number,title,description_markdown,acceptance_criteria_markdown,priority,stage,target_branch,created_at,updated_at) VALUES('PLAN-1','p',1,'Task','','','medium','created','main','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	module := New(database)
	plan, err := module.Create(t.Context(), CreateInput{ProjectID: "p", Kind: "task_plan", SourceWorkItemID: "PLAN-1", Title: "处理任务：PLAN-1", CreatedByType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec("UPDATE agent_requests SET status='succeeded',thread_id='old-thread',final_message='Approved steps',ended_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := module.ApprovePlan(t.Context(), plan.ID, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if execution.ID == plan.ID || execution.Kind != "task_execution" || execution.Status != "queued" || execution.PromptMarkdown != "Approved steps" || execution.ThreadID != nil {
		t.Fatalf("execution request = %#v", execution)
	}
}

func TestTaskCannotHaveTwoActiveAgentRequests(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "requests.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.DB.Exec(`INSERT INTO projects(id,project_key,name,project_path,created_at,updated_at) VALUES('p','ONE','One','C:/one', 'now','now');
		INSERT INTO work_items(id,project_id,number,title,description_markdown,acceptance_criteria_markdown,priority,stage,target_branch,created_at,updated_at) VALUES('ONE-1','p',1,'Task','','','medium','created','main','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	module := New(database)
	_, err = module.Create(t.Context(), CreateInput{ProjectID: "p", Kind: "task_plan", SourceWorkItemID: "ONE-1", Title: "Plan", CreatedByType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = module.Create(t.Context(), CreateInput{ProjectID: "p", Kind: "task_execution", SourceWorkItemID: "ONE-1", Title: "Execute", CreatedByType: "system"}); !IsCode(err, "REQUEST_ACTIVE") {
		t.Fatalf("second active task request error = %v", err)
	}
}

func IsCode(err error, code string) bool {
	var requestError *Error
	return errors.As(err, &requestError) && requestError.Code == code
}

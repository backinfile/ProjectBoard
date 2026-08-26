package runmanager_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/projectboard/projectboard/internal/agent"
	"github.com/projectboard/projectboard/internal/domain"
	"github.com/projectboard/projectboard/internal/events"
	"github.com/projectboard/projectboard/internal/runmanager"
	"github.com/projectboard/projectboard/internal/store"
	"github.com/projectboard/projectboard/internal/workspace"
)

type fakeRunner struct{ started chan string }

func (f fakeRunner) Run(_ context.Context, r agent.Request, h agent.Handler) (agent.Result, error) {
	f.started <- r.CWD
	h.OnEvent(agent.Event{Method: "turn/started", Params: []byte(`{"turn":{"id":"turn-1"}}`)})
	h.OnEvent(agent.Event{Method: "item/agentMessage/delta", Params: []byte(`{"delta":"已完成实现。"}`)})
	return agent.Result{ThreadID: "thread-1", TurnID: "turn-1", Status: "completed"}, nil
}

func TestQueuedRunExecutesInTaskWorkspace(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.name", "Test")
	git(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	st, err := store.Open(ctx, filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	project, err := st.CreateProject(ctx, domain.Project{Name: "Project", Path: repo, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(ctx, domain.Task{ProjectID: project.ID, Title: "Implement"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := st.GetOrCreateConversation(ctx, task.ID, "agent_codex_default")
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, domain.Run{TaskID: task.ID, ConversationID: conversation.ID, AgentID: "agent_codex_default", Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 1)
	m := runmanager.New(st, events.New(), workspace.NewManager(filepath.Join(root, "managed")), fakeRunner{started: started}, 1)
	if err := m.Enqueue(run); err != nil {
		t.Fatal(err)
	}
	select {
	case cwd := <-started:
		if cwd == repo {
			t.Fatal("run was not isolated in a task worktree")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not start")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ := st.ListRuns(ctx, task.ID)
		if len(runs) == 1 && runs[0].Status == domain.RunSucceeded {
			messages, err := st.ListMessages(ctx, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 1 || messages[0].Role != "assistant" || messages[0].Content != "已完成实现。" {
				t.Fatalf("assistant response was not persisted: %#v", messages)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run did not succeed")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

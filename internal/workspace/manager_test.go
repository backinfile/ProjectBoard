package workspace_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectboard/projectboard/internal/workspace"
)

func TestParallelCandidateCanBeIntegratedAndAutomaticallyCleaned(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "init", "-b", "main")
	run(t, repo, "git", "config", "user.name", "Test")
	run(t, repo, "git", "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "base")
	m := workspace.NewManager(filepath.Join(root, "managed"))
	main, err := m.EnsureTask(ctx, repo, "project", "task", "PB-1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main.Path, "README.md"), []byte("main snapshot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, children, err := m.CreateChildren(ctx, main, "plan-1", []string{"review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 {
		t.Fatalf("expected one child")
	}
	content, err := os.ReadFile(filepath.Join(children[0].Path, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(content), "\r\n", "\n") != "main snapshot\n" {
		t.Fatalf("child did not inherit snapshot: %q", content)
	}
	if err := os.WriteFile(filepath.Join(children[0].Path, "review.txt"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := m.CompleteChild(ctx, children[0], "review")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.IntegrateAndCleanup(ctx, main, candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(children[0].Path); !os.IsNotExist(err) {
		t.Fatalf("child worktree was not removed")
	}
	if got, err := os.ReadFile(filepath.Join(main.Path, "review.txt")); err != nil || strings.ReplaceAll(string(got), "\r\n", "\n") != "candidate\n" {
		t.Fatalf("candidate not integrated: %q %v", got, err)
	}
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %s: %v", name, args, out, err)
	}
}

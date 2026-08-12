package projectrepo_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectboard/projectboard/internal/projectrepo"
)

func TestPrepareCreatesAUsableLocalRepository(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-project")
	repository, err := projectrepo.Prepare(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if repository.Path != path {
		t.Fatalf("path = %q, want %q", repository.Path, path)
	}
	if repository.DefaultBranch != "main" {
		t.Fatalf("default branch = %q, want main", repository.DefaultBranch)
	}
	if _, err = os.Stat(filepath.Join(path, ".git")); err != nil {
		t.Fatalf("repository was not initialized: %v", err)
	}
	command := exec.Command("git", "-C", path, "log", "-1", "--pretty=%s")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("read baseline commit: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "chore: initialize ProjectBoard project" {
		t.Fatalf("baseline commit = %q", output)
	}
}

func TestPrepareCommitsFilesWhenInitializingRepository(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing-files")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("project files\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := projectrepo.Prepare(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if content := runGit(t, path, "show", "HEAD:README.md"); content != "project files\n" {
		t.Fatalf("baseline content = %q", content)
	}
}

func TestPrepareRejectsDirtyExistingRepository(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirty")
	if _, err := projectrepo.Prepare(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := projectrepo.Prepare(context.Background(), path); err == nil {
		t.Fatal("dirty existing repository was accepted")
	}
}

func TestEnsureWorktreeCreatesTaskBranchBesideRepository(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "source")
	if _, err := projectrepo.Prepare(context.Background(), projectPath); err != nil {
		t.Fatal(err)
	}
	workspace, err := projectrepo.EnsureWorktree(context.Background(), projectPath, "PB", 7, "main")
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(filepath.Dir(projectPath), "worktrees", "pb-7")
	if workspace.Path != wantPath || workspace.Branch != "projectboard/pb/7" || workspace.BaseCommit == "" {
		t.Fatalf("workspace = %+v, want path %q and task branch", workspace, wantPath)
	}
	branch := strings.TrimSpace(runGit(t, workspace.Path, "branch", "--show-current"))
	if branch != workspace.Branch {
		t.Fatalf("checked out branch = %q, want %q", branch, workspace.Branch)
	}
}

func TestHasSecondParentDistinguishesMergeCommits(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "source")
	if _, err := projectrepo.Prepare(context.Background(), projectPath); err != nil {
		t.Fatal(err)
	}
	merged, err := projectrepo.HasSecondParent(context.Background(), projectPath, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if merged {
		t.Fatal("baseline commit was reported as a merge commit")
	}
	runGit(t, projectPath, "switch", "-c", "feature")
	if err = os.WriteFile(filepath.Join(projectPath, "feature.txt"), []byte("feature"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, projectPath, "add", "feature.txt")
	runGit(t, projectPath, "-c", "user.name=ProjectBoard", "-c", "user.email=projectboard@local", "commit", "-m", "feature")
	runGit(t, projectPath, "switch", "main")
	runGit(t, projectPath, "-c", "user.name=ProjectBoard", "-c", "user.email=projectboard@local", "merge", "--no-ff", "feature", "-m", "merge feature")
	merged, err = projectrepo.HasSecondParent(context.Background(), projectPath, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !merged {
		t.Fatal("merge commit did not report a second parent")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

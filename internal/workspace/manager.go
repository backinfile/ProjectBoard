package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrNotGit = errors.New("project directory is not a git repository")
var ErrDirtyTarget = errors.New("target worktree must be clean")

type Workspace struct {
	ID         string `json:"id"`
	TaskID     string `json:"taskId"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	BaseCommit string `json:"baseCommit"`
	Kind       string `json:"kind"`
	ParentID   string `json:"parentId,omitempty"`
}

type Candidate struct {
	Workspace Workspace `json:"workspace"`
	Commit    string    `json:"commit"`
	Patch     string    `json:"patch"`
}

type Manager struct{ root string }

func NewManager(root string) *Manager { return &Manager{root: filepath.Clean(root)} }

func (m *Manager) InitializeRepository(ctx context.Context, path, branch string) (string, error) {
	if branch == "" {
		branch = "main"
	}
	if _, err := git(ctx, path, "rev-parse", "--git-dir"); err == nil {
		return "", fmt.Errorf("directory is already a git repository")
	}
	if _, err := git(ctx, path, "init", "-b", branch); err != nil {
		return "", err
	}
	if _, err := git(ctx, path, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := git(ctx, path, "-c", "user.name=ProjectBoard", "-c", "user.email=projectboard@localhost", "commit", "--allow-empty", "-m", "ProjectBoard baseline"); err != nil {
		return "", err
	}
	head, err := git(ctx, path, "rev-parse", "HEAD")
	return strings.TrimSpace(head), err
}

func (m *Manager) EnsureTask(ctx context.Context, projectPath, projectID, taskID, taskKey, defaultBranch string) (Workspace, error) {
	if _, err := git(ctx, projectPath, "rev-parse", "--git-dir"); err != nil {
		return Workspace{}, ErrNotGit
	}
	base, err := git(ctx, projectPath, "rev-parse", defaultBranch)
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve default branch: %w", err)
	}
	path := filepath.Join(m.root, safe(projectID), safe(taskID), "main")
	branch := "projectboard/task-" + strings.ToLower(safe(taskKey))
	if current, err := git(ctx, projectPath, "rev-parse", "--verify", branch); err == nil && strings.TrimSpace(current) != "" {
		if _, statErr := os.Stat(path); statErr == nil {
			return Workspace{ID: "main-" + taskID, TaskID: taskID, Path: path, Branch: branch, BaseCommit: strings.TrimSpace(base), Kind: "main"}, nil
		}
		if _, err = git(ctx, projectPath, "worktree", "add", path, branch); err != nil {
			return Workspace{}, err
		}
	} else if _, err = git(ctx, projectPath, "worktree", "add", "-b", branch, path, defaultBranch); err != nil {
		return Workspace{}, err
	}
	return Workspace{ID: "main-" + taskID, TaskID: taskID, Path: path, Branch: branch, BaseCommit: strings.TrimSpace(base), Kind: "main"}, nil
}

func (m *Manager) CreateChildren(ctx context.Context, main Workspace, planID string, names []string) (string, []Workspace, error) {
	if main.Kind != "main" {
		return "", nil, fmt.Errorf("only a task main workspace can create children")
	}
	if len(names) == 0 {
		return "", nil, fmt.Errorf("at least one child is required")
	}
	if _, err := git(ctx, main.Path, "add", "-A"); err != nil {
		return "", nil, err
	}
	message := "projectboard: parallel snapshot " + safe(planID)
	if _, err := git(ctx, main.Path, "-c", "user.name=ProjectBoard", "-c", "user.email=projectboard@localhost", "commit", "--allow-empty", "-m", message); err != nil {
		return "", nil, err
	}
	head, err := git(ctx, main.Path, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, err
	}
	head = strings.TrimSpace(head)
	children := make([]Workspace, 0, len(names))
	for i, name := range names {
		id := fmt.Sprintf("%s-%02d", safe(planID), i+1)
		// Git refs are files: a main branch named projectboard/task-x prevents
		// creating projectboard/task-x/child. Keep child refs as siblings.
		branch := fmt.Sprintf("projectboard/child-%s-%s", strings.ToLower(safe(main.TaskID)), id)
		path := filepath.Join(filepath.Dir(main.Path), "children", id)
		if _, err := git(ctx, main.Path, "worktree", "add", "-b", branch, path, head); err != nil {
			return head, children, fmt.Errorf("create child %s: %w", name, err)
		}
		children = append(children, Workspace{ID: id, TaskID: main.TaskID, Path: path, Branch: branch, BaseCommit: head, Kind: "child", ParentID: main.ID})
	}
	return head, children, nil
}

func (m *Manager) CompleteChild(ctx context.Context, child Workspace, summary string) (Candidate, error) {
	if child.Kind != "child" {
		return Candidate{}, fmt.Errorf("workspace is not a child")
	}
	if _, err := git(ctx, child.Path, "add", "-A"); err != nil {
		return Candidate{}, err
	}
	if _, err := git(ctx, child.Path, "-c", "user.name=ProjectBoard", "-c", "user.email=projectboard@localhost", "commit", "--allow-empty", "-m", "projectboard: candidate "+summary); err != nil {
		return Candidate{}, err
	}
	commit, err := git(ctx, child.Path, "rev-parse", "HEAD")
	if err != nil {
		return Candidate{}, err
	}
	commit = strings.TrimSpace(commit)
	patch, err := git(ctx, child.Path, "diff", "--binary", child.BaseCommit, commit)
	if err != nil {
		return Candidate{}, err
	}
	return Candidate{Workspace: child, Commit: commit, Patch: patch}, nil
}

func (m *Manager) IntegrateAndCleanup(ctx context.Context, main Workspace, c Candidate) (err error) {
	if dirty, checkErr := git(ctx, main.Path, "status", "--porcelain"); checkErr != nil {
		return checkErr
	} else if strings.TrimSpace(dirty) != "" {
		return ErrDirtyTarget
	}
	if _, err = git(ctx, main.Path, "cherry-pick", c.Commit); err != nil {
		return fmt.Errorf("candidate conflict: %w", err)
	}
	return m.cleanup(ctx, main.Path, c.Workspace)
}

func (m *Manager) RejectAndCleanup(ctx context.Context, main Workspace, c Candidate) error {
	return m.cleanup(ctx, main.Path, c.Workspace)
}

func (m *Manager) cleanup(ctx context.Context, repositoryPath string, child Workspace) error {
	root, err := filepath.Abs(m.root)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(child.Path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove workspace outside managed root")
	}
	if _, err := git(ctx, repositoryPath, "worktree", "remove", "--force", target); err != nil {
		return err
	}
	if _, err := git(ctx, repositoryPath, "branch", "-D", child.Branch); err != nil {
		return err
	}
	return nil
}

func (m *Manager) Finalize(ctx context.Context, projectPath string, main Workspace, defaultBranch, message string) (string, error) {
	if dirty, err := git(ctx, projectPath, "status", "--porcelain"); err != nil {
		return "", err
	} else if strings.TrimSpace(dirty) != "" {
		return "", ErrDirtyTarget
	}
	branch, err := git(ctx, projectPath, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(branch) != defaultBranch {
		return "", fmt.Errorf("project directory must be on default branch %s", defaultBranch)
	}
	base, err := git(ctx, projectPath, "rev-parse", defaultBranch)
	if err != nil {
		return "", err
	}
	tree, err := git(ctx, main.Path, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	commit, err := git(ctx, main.Path, "commit-tree", strings.TrimSpace(tree), "-p", strings.TrimSpace(base), "-m", message)
	if err != nil {
		return "", err
	}
	commit = strings.TrimSpace(commit)
	if _, err := git(ctx, projectPath, "merge", "--ff-only", commit); err != nil {
		return "", fmt.Errorf("merge task commit: %w", err)
	}
	return commit, nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safe(v string) string {
	v = unsafeName.ReplaceAllString(strings.TrimSpace(v), "-")
	v = strings.Trim(v, "-.")
	if v == "" {
		return "workspace"
	}
	return v
}

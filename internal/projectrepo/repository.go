package projectrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repository struct {
	Path          string
	DefaultBranch string
}

type Workspace struct {
	Path       string
	Branch     string
	BaseCommit string
}

func Prepare(ctx context.Context, requestedPath string) (Repository, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" || !filepath.IsAbs(requestedPath) {
		return Repository{}, errors.New("project path must be absolute")
	}
	path := filepath.Clean(requestedPath)
	if filepath.Clean(filepath.VolumeName(path)+string(os.PathSeparator)) == path {
		return Repository{}, errors.New("project path cannot be a filesystem root")
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		if err = os.MkdirAll(path, 0o700); err != nil {
			return Repository{}, fmt.Errorf("create project directory: %w", err)
		}
		info, err = os.Stat(path)
	}
	if err != nil {
		return Repository{}, fmt.Errorf("inspect project directory: %w", err)
	}
	if !info.IsDir() {
		return Repository{}, errors.New("project path must be a directory")
	}
	if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
		path = filepath.Clean(resolved)
	}

	repositoryRoot, rootErr := gitOutput(ctx, path, "rev-parse", "--show-toplevel")
	if rootErr == nil {
		root, absErr := filepath.Abs(strings.TrimSpace(repositoryRoot))
		if absErr != nil || !samePath(root, path) {
			return Repository{}, errors.New("project path must be the Git repository root")
		}
		bare, bareErr := gitOutput(ctx, path, "rev-parse", "--is-bare-repository")
		if bareErr != nil || strings.TrimSpace(bare) != "false" {
			return Repository{}, errors.New("project path must be a non-bare Git repository")
		}
	} else {
		if err = git(ctx, "", "init", "-b", "main", path); err != nil {
			return Repository{}, err
		}
	}

	if _, err = gitOutput(ctx, path, "rev-parse", "--verify", "HEAD"); err != nil {
		if err = git(ctx, path, "add", "-A"); err != nil {
			return Repository{}, err
		}
		if err = git(ctx, path, "-c", "user.name=ProjectBoard", "-c", "user.email=projectboard@local", "commit", "--allow-empty", "-m", "chore: initialize ProjectBoard project"); err != nil {
			return Repository{}, err
		}
	} else {
		status, statusErr := gitOutput(ctx, path, "status", "--porcelain")
		if statusErr != nil {
			return Repository{}, statusErr
		}
		if strings.TrimSpace(status) != "" {
			return Repository{}, errors.New("project repository must have a clean working tree")
		}
	}
	branch, err := gitOutput(ctx, path, "branch", "--show-current")
	if err != nil {
		return Repository{}, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return Repository{}, errors.New("project repository cannot use a detached HEAD")
	}
	return Repository{Path: path, DefaultBranch: branch}, nil
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	return strings.EqualFold(left, right)
}

func BranchExists(ctx context.Context, repositoryPath, branch string) (bool, error) {
	command := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	command.Dir = repositoryPath
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check branch %q: %w", branch, err)
}

func EnsureWorktree(ctx context.Context, repositoryPath, projectKey string, number int64, targetBranch string) (Workspace, error) {
	exists, err := BranchExists(ctx, repositoryPath, targetBranch)
	if err != nil {
		return Workspace{}, err
	}
	if !exists {
		return Workspace{}, fmt.Errorf("target branch %q does not exist", targetBranch)
	}
	base, err := gitOutput(ctx, repositoryPath, "rev-parse", targetBranch)
	if err != nil {
		return Workspace{}, err
	}
	projectKey = strings.ToLower(strings.TrimSpace(projectKey))
	branch := fmt.Sprintf("projectboard/%s/%d", projectKey, number)
	workspacePath := WorktreePath(repositoryPath, projectKey, number)
	if _, statErr := os.Stat(workspacePath); statErr == nil {
		current, currentErr := CurrentBranch(ctx, workspacePath)
		if currentErr != nil || current != branch {
			return Workspace{}, errors.New("task worktree path already exists and belongs to another branch")
		}
		return Workspace{Path: workspacePath, Branch: branch, BaseCommit: strings.TrimSpace(base)}, nil
	} else if !os.IsNotExist(statErr) {
		return Workspace{}, statErr
	}
	if err = os.MkdirAll(filepath.Dir(workspacePath), 0o700); err != nil {
		return Workspace{}, fmt.Errorf("create worktree directory: %w", err)
	}
	taskBranchExists, err := BranchExists(ctx, repositoryPath, branch)
	if err != nil {
		return Workspace{}, err
	}
	if taskBranchExists {
		err = git(ctx, repositoryPath, "worktree", "add", workspacePath, branch)
	} else {
		err = git(ctx, repositoryPath, "worktree", "add", "-b", branch, workspacePath, targetBranch)
	}
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{Path: workspacePath, Branch: branch, BaseCommit: strings.TrimSpace(base)}, nil
}

func WorktreePath(repositoryPath, projectKey string, number int64) string {
	return filepath.Join(filepath.Dir(repositoryPath), "worktrees", fmt.Sprintf("%s-%d", strings.ToLower(strings.TrimSpace(projectKey)), number))
}

func CurrentBranch(ctx context.Context, path string) (string, error) {
	branch, err := gitOutput(ctx, path, "branch", "--show-current")
	return strings.TrimSpace(branch), err
}

func Head(ctx context.Context, path string) (string, error) {
	head, err := gitOutput(ctx, path, "rev-parse", "HEAD")
	return strings.TrimSpace(head), err
}

func ResolveRevision(ctx context.Context, repositoryPath, revision string) (string, error) {
	value, err := gitOutput(ctx, repositoryPath, "rev-parse", revision)
	return strings.TrimSpace(value), err
}

func Status(ctx context.Context, path string) (string, error) {
	status, err := gitOutput(ctx, path, "status", "--porcelain")
	return strings.TrimSpace(status), err
}

func Checkout(ctx context.Context, repositoryPath, branch string) error {
	return git(ctx, repositoryPath, "switch", branch)
}

func IsAncestor(ctx context.Context, repositoryPath, ancestor, descendant string) (bool, error) {
	command := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", ancestor, descendant)
	command.Dir = repositoryPath
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func HasSecondParent(ctx context.Context, repositoryPath, revision string) (bool, error) {
	command := exec.CommandContext(ctx, "git", "rev-parse", "--verify", revision+"^2")
	command.Dir = repositoryPath
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}

func RemoveWorktree(ctx context.Context, repositoryPath, workspacePath string) error {
	return git(ctx, repositoryPath, "worktree", "remove", workspacePath)
}

func git(ctx context.Context, dir string, args ...string) error {
	_, err := gitOutput(ctx, dir, args...)
	return err
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

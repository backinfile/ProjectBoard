package runnercli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const defaultLocalAgent = "codex"

// LocalAgentInfo describes a local coding CLI that the Runner knows how to use.
type LocalAgentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Selected  bool   `json:"selected"`
}

// AgentCommand is the resolved, non-interactive process invocation for a local Agent.
type AgentCommand struct {
	Path string
	Args []string
	Dir  string
}

type localAgentAdapter struct {
	id      string
	name    string
	command string
	build   func(path, workDir, prompt string) AgentCommand
}

var localAgentAdapters = []localAgentAdapter{
	{
		id: "codex", name: "Codex CLI", command: "codex",
		build: func(path, workDir, prompt string) AgentCommand {
			return AgentCommand{Path: path, Args: []string{"--ask-for-approval", "never", "exec", "--sandbox", "workspace-write", "-C", workDir, "--", prompt}}
		},
	},
	{
		id: "opencode", name: "OpenCode CLI", command: "opencode",
		build: func(path, workDir, prompt string) AgentCommand {
			return AgentCommand{Path: path, Args: []string{"run", "--auto", "--dir", workDir, "--", prompt}, Dir: workDir}
		},
	},
}

func findLocalAgent(id string) (localAgentAdapter, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, adapter := range localAgentAdapters {
		if adapter.id == id {
			return adapter, true
		}
	}
	return localAgentAdapter{}, false
}

// LocalAgents returns all supported adapters and whether their CLI is available.
func (a *App) LocalAgents() []LocalAgentInfo {
	selected := defaultLocalAgent
	if s, err := a.load(); err == nil && s.LocalAgent != "" {
		selected = s.LocalAgent
	}
	result := make([]LocalAgentInfo, 0, len(localAgentAdapters))
	for _, adapter := range localAgentAdapters {
		path, err := a.lookPath(adapter.command)
		result = append(result, LocalAgentInfo{
			ID: adapter.id, Name: adapter.name, Command: adapter.command,
			Installed: err == nil, Path: path, Selected: adapter.id == selected,
		})
	}
	return result
}

// SetLocalAgent validates the selected CLI and persists it in Runner state.
func (a *App) SetLocalAgent(id string) error {
	adapter, ok := findLocalAgent(id)
	if !ok {
		return fmt.Errorf("unsupported local Agent %q", id)
	}
	if _, err := a.lookPath(adapter.command); err != nil {
		return fmt.Errorf("%s is not installed or not available on PATH", adapter.name)
	}
	s, err := a.load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.LocalAgent = adapter.id
	if s.Mappings == nil {
		s.Mappings = map[string]string{}
	}
	return a.save(s)
}

func (a *App) localAgent(args []string) error {
	if len(args) == 1 && args[0] == "list" {
		return writeLocalAgents(a.out, a.LocalAgents())
	}
	if len(args) == 2 && args[0] == "use" {
		if err := a.SetLocalAgent(args[1]); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "local Agent set to %s\n", strings.ToLower(strings.TrimSpace(args[1])))
		return nil
	}
	return &UsageError{Message: "usage: projectboard-runner agent <list|use <codex|opencode>>"}
}

// LocalAgentCommand resolves a supported Agent to its non-interactive invocation.
func (a *App) LocalAgentCommand(id, workDir, prompt string) (AgentCommand, error) {
	adapter, ok := findLocalAgent(id)
	if !ok {
		return AgentCommand{}, fmt.Errorf("unsupported local Agent %q", id)
	}
	if strings.TrimSpace(workDir) == "" {
		return AgentCommand{}, errors.New("local Agent work directory is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return AgentCommand{}, errors.New("local Agent prompt is required")
	}
	path, err := a.lookPath(adapter.command)
	if err != nil {
		return AgentCommand{}, fmt.Errorf("%s is not installed or not available on PATH", adapter.name)
	}
	return adapter.build(path, workDir, prompt), nil
}

// ExecuteLocalAgent runs the selected local coding CLI and streams its output.
func (a *App) ExecuteLocalAgent(ctx context.Context, workDir, prompt string) error {
	id := defaultLocalAgent
	s, err := a.load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if s.LocalAgent != "" {
		id = s.LocalAgent
	}
	invocation, err := a.LocalAgentCommand(id, workDir, prompt)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, invocation.Path, invocation.Args...)
	cmd.Dir = invocation.Dir
	cmd.Stdout = a.out
	cmd.Stderr = a.err
	return cmd.Run()
}

func writeLocalAgents(w io.Writer, agents []LocalAgentInfo) error {
	return writeJSON(w, agents)
}

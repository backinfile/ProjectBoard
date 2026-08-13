//go:build windows

package agentexec

import (
	"os/exec"
	"testing"
)

func TestRunAgentCommandUsesWindowsJobObject(t *testing.T) {
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "exit 0")
	if err := runAgentCommand(cmd); err != nil {
		t.Fatalf("run command in Windows Job Object: %v", err)
	}
}

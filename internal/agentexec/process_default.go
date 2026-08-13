//go:build !windows

package agentexec

import "os/exec"

func runAgentCommand(cmd *exec.Cmd) error {
	return cmd.Run()
}

//go:build !windows

package server

import "os"

func protectSSHHostKey(path string) error {
	return os.Chmod(path, 0o600)
}

//go:build windows

package main

// Regenerate the architecture-specific resources after changing the manifest.
//go:generate go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -manifest projectboard-runner.exe.manifest -o rsrc_windows_amd64.syso
//go:generate go run github.com/akavel/rsrc@v0.10.2 -arch arm64 -manifest projectboard-runner.exe.manifest -o rsrc_windows_arm64.syso

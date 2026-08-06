//go:build windows

package main

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestAutoStartRegistryRoundTrip(t *testing.T) {
	location := autoStartLocation{
		root:      registry.CURRENT_USER,
		keyPath:   fmt.Sprintf(`Software\ProjectBoard\RunnerTests\%d`, os.Getpid()),
		valueName: "Runner",
	}
	t.Cleanup(func() { _ = registry.DeleteKey(location.root, location.keyPath) })

	enabled, err := location.enabled()
	if err != nil || enabled {
		t.Fatalf("initial enabled=%v err=%v", enabled, err)
	}

	command := autoStartCommand(`C:\Program Files\ProjectBoard\projectboard-runner.exe`)
	if err = location.set(true, command); err != nil {
		t.Fatalf("enable auto-start: %v", err)
	}
	if enabled, err = location.enabled(); err != nil || !enabled {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}

	if err = location.set(false, ""); err != nil {
		t.Fatalf("disable auto-start: %v", err)
	}
	if enabled, err = location.enabled(); err != nil || enabled {
		t.Fatalf("disabled state=%v err=%v", enabled, err)
	}
}

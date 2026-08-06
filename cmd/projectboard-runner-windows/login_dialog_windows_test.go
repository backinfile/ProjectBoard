//go:build windows

package main

import (
	"runtime"
	"testing"
)

func TestLoginDialogCanBeCreated(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	state, declaration := newLoginDialog("http://127.0.0.1:3333")
	if err := declaration.Create(nil); err != nil {
		t.Fatalf("create login dialog: %v", err)
	}
	state.dialog.Dispose()
}

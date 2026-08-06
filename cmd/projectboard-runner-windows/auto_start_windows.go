//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	autoStartRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartValueName    = "ProjectBoard Runner"
)

type autoStartLocation struct {
	root      registry.Key
	keyPath   string
	valueName string
}

var runnerAutoStart = autoStartLocation{
	root:      registry.CURRENT_USER,
	keyPath:   autoStartRegistryPath,
	valueName: autoStartValueName,
}

func autoStartCommand(executablePath string) string {
	return `"` + executablePath + `"`
}

func currentAutoStartCommand() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve Runner executable: %w", err)
	}
	return autoStartCommand(executablePath), nil
}

func (location autoStartLocation) enabled() (bool, error) {
	key, err := registry.OpenKey(location.root, location.keyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()

	value, _, err := key.GetStringValue(location.valueName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value != "", nil
}

func (location autoStartLocation) set(enabled bool, command string) error {
	if enabled {
		key, _, err := registry.CreateKey(location.root, location.keyPath, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		return key.SetStringValue(location.valueName, command)
	}

	key, err := registry.OpenKey(location.root, location.keyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	if err = key.DeleteValue(location.valueName); errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

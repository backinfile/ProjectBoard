//go:build windows

package store

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func lockDirectory(path string) (func(), error) {
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, fmt.Errorf("数据目录正在使用，请打开已运行的 ProjectBoard：%w", e)
	}
	return func() { windows.CloseHandle(h) }, nil
}

//go:build windows

package updater

import (
	"errors"

	"golang.org/x/sys/windows"
)

func processAlive(pid int) (bool, error) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	return result == uint32(windows.WAIT_TIMEOUT), nil
}

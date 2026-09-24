//go:build windows

package supervisor

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

func terminatePID(pid int) error {
	taskkill := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F")
	taskkill.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	if output, err := taskkill.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill pid %d: %w (%s)", pid, err, string(output))
	}
	return nil
}

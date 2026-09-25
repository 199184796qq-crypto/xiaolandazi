//go:build !windows

package supervisor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureSupervisorProcess() error {
	return nil
}

func configureProcess(cmd *exec.Cmd, _ string) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func availableMemoryMB() (uint64, bool) {
	return 0, false
}

func terminatePID(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		process, findErr := os.FindProcess(pid)
		if findErr != nil {
			return err
		}
		return process.Kill()
	}
	return nil
}

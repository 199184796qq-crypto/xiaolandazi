//go:build windows

package supervisor

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	createNoWindow           = 0x08000000
	normalPriorityClass      = 0x00000020
	highPriorityClass        = 0x00000080
	belowNormalPriorityClass = 0x00004000
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentProcess    = kernel32.NewProc("GetCurrentProcess")
	procSetPriorityClass     = kernel32.NewProc("SetPriorityClass")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func configureSupervisorProcess() error {
	handle, _, _ := procGetCurrentProcess.Call()
	result, _, callErr := procSetPriorityClass.Call(handle, highPriorityClass)
	if result == 0 {
		return fmt.Errorf("SetPriorityClass(HIGH): %v", callErr)
	}
	return nil
}

func configureProcess(cmd *exec.Cmd, priority string) {
	priorityFlag := uint32(normalPriorityClass)
	switch priority {
	case "below_normal":
		priorityFlag = belowNormalPriorityClass
	case "high":
		priorityFlag = highPriorityClass
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | priorityFlag,
	}
}

func availableMemoryMB() (uint64, bool) {
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	result, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return 0, false
	}
	return status.AvailPhys / (1024 * 1024), true
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

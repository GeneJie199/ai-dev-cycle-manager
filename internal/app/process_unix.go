//go:build !windows

package app

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func prepareManagedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 5 * time.Second
	command.Cancel = func() error { return stopManagedCommand(command) }
}

func stopManagedCommand(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	if err == nil {
		time.AfterFunc(3*time.Second, func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) })
	}
	return err
}

//go:build darwin || linux

package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

var buildCancelGracePeriod = 2 * time.Second

func configureBuildCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return signalBuildProcessGroup(command.Process.Pid, syscall.SIGTERM)
	}
}

func watchBuildProcess(ctx context.Context, pid int) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if err := signalBuildProcessGroup(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return
			}
			select {
			case <-done:
			case <-time.After(buildCancelGracePeriod):
				_ = signalBuildProcessGroup(pid, syscall.SIGKILL)
			}
		case <-done:
		}
	}()

	return func() {
		close(done)
	}
}

func signalBuildProcessGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(-pid, signal); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

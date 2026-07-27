//go:build !darwin && !linux

package agent

import (
	"context"
	"os/exec"
)

func configureBuildCommand(command *exec.Cmd) {}

func watchBuildProcess(ctx context.Context, pid int) func() {
	return func() {}
}

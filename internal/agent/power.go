package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
)

var caffeinatePath = "/usr/bin/caffeinate"

func startPowerAssertion(ctx context.Context, pid int, enabled bool, stream *buildStream) func() {
	if !enabled || runtime.GOOS != "darwin" || pid <= 0 {
		return func() {}
	}

	command := exec.CommandContext(ctx, caffeinatePath, "-i", "-w", strconv.Itoa(pid))
	if err := command.Start(); err != nil {
		stream.write(Event{Kind: "error", Message: "prevent sleep failed: " + err.Error()})
		return func() {}
	}

	stream.write(Event{Kind: "build", Message: "preventing sleep during build"})
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()

	return func() {
		select {
		case <-done:
		default:
			_ = command.Process.Kill()
			<-done
		}
	}
}

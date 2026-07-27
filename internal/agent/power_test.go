package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStartPowerAssertionRunsCaffeinateOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("caffeinate is macOS-specific")
	}

	fake, argsPath := fakeCaffeinate(t)
	original := caffeinatePath
	caffeinatePath = fake
	defer func() {
		caffeinatePath = original
	}()

	sleep := exec.Command("/bin/sleep", "1")
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = sleep.Process.Kill()
		_ = sleep.Wait()
	}()

	stream := newBuildStream("build-123", "sleep", Redactor{})
	stop := startPowerAssertion(context.Background(), sleep.Process.Pid, true, stream)
	defer stop()

	args := waitForPowerFile(t, argsPath)
	if !strings.Contains(args, "-i -w "+strconv.Itoa(sleep.Process.Pid)) {
		t.Fatalf("unexpected caffeinate args: %s", args)
	}
	if !eventsContain(stream.eventsAfter(0), "preventing sleep during build") {
		t.Fatalf("missing power assertion event: %+v", stream.eventsAfter(0))
	}
}

func TestStartPowerAssertionDisabledDoesNothing(t *testing.T) {
	fake, argsPath := fakeCaffeinate(t)
	original := caffeinatePath
	caffeinatePath = fake
	defer func() {
		caffeinatePath = original
	}()

	stop := startPowerAssertion(context.Background(), 123, false, newBuildStream("build-123", "job", Redactor{}))
	stop()

	if _, err := os.Stat(argsPath); err == nil {
		t.Fatal("expected fake caffeinate not to run")
	}
}

func fakeCaffeinate(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "caffeinate")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuote(argsPath) + "\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, argsPath
}

func waitForPowerFile(t *testing.T, path string) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", path)
	return ""
}

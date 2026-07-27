package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/charliewilco/transwarp/internal/dispatch"
)

func TestWriteGitHubOutputsWritesAvailableDispatchIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github-output")

	err := writeGitHubOutputs(path, dispatch.RunResult{
		RequestID: "run-123",
		BuildID:   "build-456",
		JobID:     "xcode-debug",
		MachineID: "machine-789",
		PublicURL: "https://runner.example.com",
	})
	if err != nil {
		t.Fatalf("writeGitHubOutputs returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	got := string(data)
	want := "request-id<<TRANSWARP_OUTPUT\nrun-123\nTRANSWARP_OUTPUT\nbuild-id<<TRANSWARP_OUTPUT\nbuild-456\nTRANSWARP_OUTPUT\njob-id<<TRANSWARP_OUTPUT\nxcode-debug\nTRANSWARP_OUTPUT\nmachine-id<<TRANSWARP_OUTPUT\nmachine-789\nTRANSWARP_OUTPUT\npublic-url<<TRANSWARP_OUTPUT\nhttps://runner.example.com\nTRANSWARP_OUTPUT\n"
	if got != want {
		t.Fatalf("unexpected output file:\nwant %q\ngot  %q", want, got)
	}
}

func TestWriteGitHubOutputsWritesRecordedResultStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github-output")

	err := writeGitHubOutputs(path, dispatch.RunResult{
		RequestID: "run-123",
		BuildID:   "build-456",
		Status:    "failed",
		ExitCode:  65,
		Error:     "xcodebuild exited 65",
	})
	if err != nil {
		t.Fatalf("writeGitHubOutputs returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	got := string(data)
	want := "request-id<<TRANSWARP_OUTPUT\nrun-123\nTRANSWARP_OUTPUT\nbuild-id<<TRANSWARP_OUTPUT\nbuild-456\nTRANSWARP_OUTPUT\nstatus<<TRANSWARP_OUTPUT\nfailed\nTRANSWARP_OUTPUT\nexit-code<<TRANSWARP_OUTPUT\n65\nTRANSWARP_OUTPUT\nerror<<TRANSWARP_OUTPUT\nxcodebuild exited 65\nTRANSWARP_OUTPUT\n"
	if got != want {
		t.Fatalf("unexpected output file:\nwant %q\ngot  %q", want, got)
	}
}

func TestWriteGitHubOutputsUsesFreshDelimiterForCollidingValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github-output")

	err := writeGitHubOutputs(path, dispatch.RunResult{
		RequestID: "line one\nTRANSWARP_OUTPUT\nline two",
	})
	if err != nil {
		t.Fatalf("writeGitHubOutputs returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	got := string(data)
	want := "request-id<<TRANSWARP_OUTPUT_END\nline one\nTRANSWARP_OUTPUT\nline two\nTRANSWARP_OUTPUT_END\n"
	if got != want {
		t.Fatalf("unexpected output file:\nwant %q\ngot  %q", want, got)
	}
}

func TestWriteGitHubOutputsSkipsMissingPath(t *testing.T) {
	if err := writeGitHubOutputs("", dispatch.RunResult{RequestID: "run-123"}); err != nil {
		t.Fatalf("writeGitHubOutputs with empty path returned error: %v", err)
	}
}

func TestWriteResultSummaryWritesAvailableDispatchIDs(t *testing.T) {
	var output bytes.Buffer

	writeResultSummary(&output, dispatch.RunResult{
		RequestID: "run-123",
		BuildID:   "build-456",
		JobID:     "xcode-debug",
		MachineID: "machine-789",
		PublicURL: "https://runner.example.com",
	})

	want := "[result] request_id run-123\n[result] build_id build-456\n[result] job_id xcode-debug\n[result] machine_id machine-789\n[result] public_url https://runner.example.com\n"
	if output.String() != want {
		t.Fatalf("unexpected result summary:\nwant %q\ngot  %q", want, output.String())
	}
}

func TestWriteResultSummaryWritesRecordedResultStatus(t *testing.T) {
	var output bytes.Buffer

	writeResultSummary(&output, dispatch.RunResult{
		RequestID: "run-123",
		BuildID:   "build-456",
		Status:    "failed",
		ExitCode:  65,
		Error:     "xcodebuild exited 65",
	})

	want := "[result] request_id run-123\n[result] build_id build-456\n[result] status failed\n[result] exit_code 65\n[result] error xcodebuild exited 65\n"
	if output.String() != want {
		t.Fatalf("unexpected result summary:\nwant %q\ngot  %q", want, output.String())
	}
}

func TestShouldWriteResultSummaryRequiresRunnerMetadata(t *testing.T) {
	quiet := dispatch.RunResult{
		RequestID: "run-123",
		JobID:     "xcode-debug",
	}
	if shouldWriteResultSummary(quiet) {
		t.Fatal("request and job IDs alone should not force a result summary")
	}

	accepted := dispatch.RunResult{
		RequestID: "run-123",
		BuildID:   "build-456",
		JobID:     "xcode-debug",
	}
	if !shouldWriteResultSummary(accepted) {
		t.Fatal("accepted build metadata should write a result summary")
	}

	terminal := dispatch.RunResult{
		RequestID: "run-123",
		Status:    "failed",
		ExitCode:  65,
		Error:     "xcodebuild exited 65",
	}
	if !shouldWriteResultSummary(terminal) {
		t.Fatal("terminal result metadata should write a result summary")
	}
}

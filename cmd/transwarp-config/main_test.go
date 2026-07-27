package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseJobSpecDecodesRepeatedArgumentsAndEnvironment(t *testing.T) {
	job, err := parseJobSpec("id=echo&label=Echo+Smoke&working_directory=%2Ftmp&command=%2Fbin%2Fecho&arg=hello+world&arg=again&env=FOO%3Dbar&redacted_environment_key=SECRET&timeout_seconds=10")
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "echo" {
		t.Fatalf("expected job id echo, got %q", job.ID)
	}
	if job.Label != "Echo Smoke" {
		t.Fatalf("expected decoded label, got %q", job.Label)
	}
	if got := strings.Join(job.Arguments, ","); got != "hello world,again" {
		t.Fatalf("expected repeated arguments, got %q", got)
	}
	if job.Environment["FOO"] != "bar" {
		t.Fatalf("expected environment value, got %#v", job.Environment)
	}
	if got := strings.Join(job.RedactedEnvironmentKeys, ","); got != "SECRET" {
		t.Fatalf("expected redacted key, got %q", got)
	}
	if job.TimeoutSeconds != 10 {
		t.Fatalf("expected timeout 10, got %d", job.TimeoutSeconds)
	}
}

func TestParseJobSpecRejectsUnknownFields(t *testing.T) {
	_, err := parseJobSpec("id=echo&script=%2Fbin%2Fecho")
	if err == nil || !strings.Contains(err.Error(), `unknown field "script"`) {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestParseJobSpecRejectsDuplicateScalarFields(t *testing.T) {
	_, err := parseJobSpec("id=echo&id=again")
	if err == nil || !strings.Contains(err.Error(), `field "id" may only be provided once`) {
		t.Fatalf("expected duplicate field error, got %v", err)
	}
}

func TestParseJobSpecRejectsMalformedEnvironment(t *testing.T) {
	_, err := parseJobSpec("id=echo&env=NOPE")
	if err == nil || !strings.Contains(err.Error(), "expected KEY=VALUE") {
		t.Fatalf("expected malformed env error, got %v", err)
	}
}

func TestRepeatedJobsCollectsMultipleJobs(t *testing.T) {
	var jobs repeatedJobs
	if err := jobs.Set("id=echo&label=Echo&working_directory=%2Ftmp&command=%2Fbin%2Fecho&arg=hi&timeout_seconds=10"); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Set("id=slow&label=Slow&working_directory=%2Ftmp&command=%2Fusr%2Fbin%2Ftrue&timeout_seconds=20"); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[1].ID != "slow" || jobs[1].TimeoutSeconds != 20 {
		t.Fatalf("unexpected second job: %#v", jobs[1])
	}
}

func TestEncodeJobSpecRoundTripsQuerySignificantCharacters(t *testing.T) {
	original, err := parseJobSpec("id=echo&label=Echo&working_directory=%2Ftmp%2Ftranswarp+review%2Ba%26b%25c&command=%2Ftmp%2Ftranswarp+review%2Ba%26b%25c%2Frun&arg=hello+%2B+%26+%25&timeout_seconds=10")
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodeJobSpec(original)
	decoded, err := parseJobSpec(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WorkingDirectory != original.WorkingDirectory {
		t.Fatalf("expected working directory %q, got %q", original.WorkingDirectory, decoded.WorkingDirectory)
	}
	if decoded.Command != original.Command {
		t.Fatalf("expected command %q, got %q", original.Command, decoded.Command)
	}
	if strings.Join(decoded.Arguments, "\n") != strings.Join(original.Arguments, "\n") {
		t.Fatalf("expected arguments %#v, got %#v", original.Arguments, decoded.Arguments)
	}
}

func TestEnvAnyReturnsFirstSetValue(t *testing.T) {
	t.Setenv("TRANSWARP_ENV_FIRST", "")
	t.Setenv("TRANSWARP_ENV_SECOND", "secret-from-env")
	t.Setenv("TRANSWARP_ENV_THIRD", "later-secret")

	if got := envAny("TRANSWARP_ENV_FIRST", "TRANSWARP_ENV_SECOND", "TRANSWARP_ENV_THIRD"); got != "secret-from-env" {
		t.Fatalf("expected second env value, got %q", got)
	}
	if got := os.Getenv("TRANSWARP_ENV_THIRD"); got != "later-secret" {
		t.Fatalf("test env was unexpectedly changed: %q", got)
	}
}

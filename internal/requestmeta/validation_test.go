package requestmeta

import (
	"strings"
	"testing"
)

func TestValidateAcceptsCommonBuildMetadata(t *testing.T) {
	if err := Validate(
		"run-123",
		"https://github.com/example/app.git",
		"refs/heads/main",
		"0123456789abcdef0123456789abcdef01234567",
	); err != nil {
		t.Fatalf("expected metadata to be accepted: %v", err)
	}
}

func TestValidateRejectsUnsafeBuildMetadata(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
		repoURL   string
		ref       string
		commit    string
		wantError string
	}{
		{
			name:      "request ID whitespace",
			requestID: "run 123",
			wantError: "request_id must contain only",
		},
		{
			name:      "request ID slash",
			requestID: "run/123",
			wantError: "request_id must contain only",
		},
		{
			name:      "repo URL newline",
			requestID: "run-123",
			repoURL:   "https://github.com/example/app.git\nInjected: true",
			wantError: "repo_url must not contain control characters",
		},
		{
			name:      "repo URL credentials",
			requestID: "run-123",
			repoURL:   "https://token:secret@github.com/example/app.git",
			wantError: "repo_url must not include credentials",
		},
		{
			name:      "repo URL query",
			requestID: "run-123",
			repoURL:   "https://github.com/example/app.git?token=secret",
			wantError: "repo_url must not include query or fragment",
		},
		{
			name:      "repo URL fragment",
			requestID: "run-123",
			repoURL:   "https://github.com/example/app.git#token",
			wantError: "repo_url must not include query or fragment",
		},
		{
			name:      "repo URL too long",
			requestID: "run-123",
			repoURL:   "https://github.com/example/" + strings.Repeat("é", 1024) + ".git",
			wantError: "repo_url is too long",
		},
		{
			name:      "ref option",
			requestID: "run-123",
			ref:       "--help",
			wantError: "ref",
		},
		{
			name:      "commit control character",
			requestID: "run-123",
			commit:    "abc123\nnext",
			wantError: "commit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(test.requestID, test.repoURL, test.ref, test.commit)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestValidateRequestIDRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
		wantError string
	}{
		{
			name:      "empty",
			requestID: "",
			wantError: "request_id is required",
		},
		{
			name:      "whitespace",
			requestID: "run 123",
			wantError: "request_id must contain only",
		},
		{
			name:      "slash",
			requestID: "run/123",
			wantError: "request_id must contain only",
		},
		{
			name:      "control",
			requestID: "run\n123",
			wantError: "request_id must contain only",
		},
		{
			name:      "too long",
			requestID: strings.Repeat("a", 257),
			wantError: "request_id is too long",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRequestID(test.requestID)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestValidateRequestIDAcceptsStableIdentifierShape(t *testing.T) {
	if err := ValidateRequestID("run-123_attempt.1_job_name"); err != nil {
		t.Fatalf("expected request ID to pass: %v", err)
	}
}

func TestValidateMachineIDRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name      string
		machineID string
		wantError string
	}{
		{
			name:      "empty",
			machineID: "",
			wantError: "machine_id is required",
		},
		{
			name:      "whitespace",
			machineID: "machine 123",
			wantError: "machine_id must contain only",
		},
		{
			name:      "slash",
			machineID: "machine/123",
			wantError: "machine_id must contain only",
		},
		{
			name:      "control",
			machineID: "machine\n123",
			wantError: "machine_id must contain only",
		},
		{
			name:      "too long",
			machineID: strings.Repeat("a", 129),
			wantError: "machine_id is too long",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateMachineID(test.machineID)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestValidateMachineIDAcceptsStableIdentifierShape(t *testing.T) {
	if err := ValidateMachineID("mac-studio_1.local"); err != nil {
		t.Fatalf("expected machine ID to pass: %v", err)
	}
}

func TestValidateJobIDRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name      string
		jobID     string
		wantError string
	}{
		{
			name:      "empty",
			jobID:     "",
			wantError: "job_id is required",
		},
		{
			name:      "whitespace",
			jobID:     "xcode debug",
			wantError: "job_id must contain only",
		},
		{
			name:      "slash",
			jobID:     "xcode/debug",
			wantError: "job_id must contain only",
		},
		{
			name:      "control",
			jobID:     "xcode\nbuild",
			wantError: "job_id must contain only",
		},
		{
			name:      "too long",
			jobID:     strings.Repeat("a", 129),
			wantError: "job_id is too long",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateJobID(test.jobID)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestValidateJobIDAcceptsStableIdentifierShape(t *testing.T) {
	if err := ValidateJobID("xcode-debug_1.release"); err != nil {
		t.Fatalf("expected job ID to pass: %v", err)
	}
}

func TestValidateBuildIDRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name      string
		buildID   string
		wantError string
	}{
		{
			name:      "empty",
			buildID:   "",
			wantError: "build_id is required",
		},
		{
			name:      "whitespace",
			buildID:   "build 123",
			wantError: "build_id must contain only",
		},
		{
			name:      "slash",
			buildID:   "build/123",
			wantError: "build_id must contain only",
		},
		{
			name:      "control",
			buildID:   "build\n123",
			wantError: "build_id must contain only",
		},
		{
			name:      "too long",
			buildID:   strings.Repeat("a", 129),
			wantError: "build_id is too long",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateBuildID(test.buildID)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestValidateBuildIDAcceptsStableIdentifierShape(t *testing.T) {
	if err := ValidateBuildID("build-123_abc.release"); err != nil {
		t.Fatalf("expected build ID to pass: %v", err)
	}
}

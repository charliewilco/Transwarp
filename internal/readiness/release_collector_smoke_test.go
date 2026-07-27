package readiness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReleaseEvidenceCollectorSmokeAcceptsLocalIncompleteCollectorOutput(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "transwarp-audit.json")
	auditStderr := filepath.Join(dir, "transwarp-audit.stderr")
	selfHostedPath := writeCollectorSmokeSelfHostedEvidence(t, dir)
	archivePath := filepath.Join(dir, "Transwarp-release.zip")
	writeCollectorSmokeAppLaunchEvidence(t, dir)
	if err := os.WriteFile(auditStderr, []byte("overall=missing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, []byte("release archive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auditPath, []byte(`{
		"generated_at": "2026-07-27T12:00:00Z",
		"app_path": ".build/Transwarp.app",
		"release_archive_path": ".build/Transwarp-release.zip",
		"strict": false,
		"overall": "missing",
		"checks": [
			{"id": "self_hosted_mac_readiness", "status": "pass", "summary": "Self-hosted Mac readiness evidence passed"},
			{"id": "app_launch_smoke", "status": "pass", "summary": "Packaged app launch smoke evidence passed"},
			{"id": "release_archive_present", "status": "pass", "summary": "Release archive exists"},
			{"id": "release_archive_readable", "status": "pass", "summary": "Release archive is readable"},
			{"id": "release_archive_contents", "status": "pass", "summary": "Release archive includes app and validation files"},
			{"id": "release_archive_checksums", "status": "pass", "summary": "Release archive checksums match"},
			{"id": "named_tunnel_smoke", "status": "missing", "summary": "Named Cloudflare Tunnel smoke evidence missing"},
			{"id": "ci_dispatch_smoke", "status": "missing", "summary": "CI dispatch evidence missing"},
			{"id": "clean_mac_validation", "status": "missing", "summary": "Clean-Mac validation evidence missing"}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ValidateReleaseEvidenceCollectorSmoke(ReleaseEvidenceCollectorSmokeOptions{
		AuditPath:              auditPath,
		AuditStderrPath:        auditStderr,
		SelfHostedEvidencePath: selfHostedPath,
		ReleaseArchivePath:     archivePath,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateReleaseEvidenceCollectorSmokeRejectsCompleteLookingLocalAudit(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "transwarp-audit.json")
	auditStderr := filepath.Join(dir, "transwarp-audit.stderr")
	selfHostedPath := writeCollectorSmokeSelfHostedEvidence(t, dir)
	archivePath := filepath.Join(dir, "Transwarp-release.zip")
	writeCollectorSmokeAppLaunchEvidence(t, dir)
	for _, path := range []string{auditStderr, archivePath} {
		if err := os.WriteFile(path, []byte("present\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(auditPath, []byte(`{
		"generated_at": "2026-07-27T12:00:00Z",
		"app_path": ".build/Transwarp.app",
		"release_archive_path": ".build/Transwarp-release.zip",
		"strict": false,
		"overall": "pass",
		"checks": [
			{"id": "self_hosted_mac_readiness", "status": "pass"},
			{"id": "app_launch_smoke", "status": "pass"},
			{"id": "release_archive_present", "status": "pass"},
			{"id": "release_archive_readable", "status": "pass"},
			{"id": "release_archive_contents", "status": "pass"},
			{"id": "release_archive_checksums", "status": "pass"},
			{"id": "named_tunnel_smoke", "status": "pass"},
			{"id": "ci_dispatch_smoke", "status": "missing"},
			{"id": "clean_mac_validation", "status": "missing"}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ValidateReleaseEvidenceCollectorSmoke(ReleaseEvidenceCollectorSmokeOptions{
		AuditPath:              auditPath,
		AuditStderrPath:        auditStderr,
		SelfHostedEvidencePath: selfHostedPath,
		ReleaseArchivePath:     archivePath,
	})
	if err == nil || !strings.Contains(err.Error(), "named_tunnel_smoke expected missing, got pass") {
		t.Fatalf("expected named tunnel status error, got %v", err)
	}
}

func writeCollectorSmokeSelfHostedEvidence(t *testing.T, dir string) string {
	t.Helper()
	sourceLog := filepath.Join(dir, "self-hosted-source.log")
	if err := os.WriteFile(sourceLog, []byte("self-hosted Mac readiness passed\narchitecture=arm64\nxcode=Xcode 26.6 Build version 17F113\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "self-hosted-mac.json")
	if err := WriteSelfHostedEvidence(SelfHostedEvidenceWriteOptions{
		OutputPath:                   outputPath,
		Architecture:                 "arm64",
		MacOS:                        "26.5.2",
		DeveloperDir:                 "/Applications/Xcode.app/Contents/Developer",
		Xcode:                        "Xcode 26.6 Build version 17F113",
		CodeSigningIdentitiesVisible: true,
		GitHubActions:                true,
		RunnerName:                   "Mac Studio",
		RunnerOS:                     "macOS",
		SourceLogPath:                sourceLog,
		Now:                          fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
	return outputPath
}

func writeCollectorSmokeAppLaunchEvidence(t *testing.T, dir string) {
	t.Helper()
	sourceDir := filepath.Join(dir, "app-launch-source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"build.ndjson": `{"kind":"build","message":"starting Xcode Version","build_id":"build-123","job_id":"xcode-version"}
{"kind":"log","message":"Xcode 26.6","build_id":"build-123","job_id":"xcode-version"}
{"kind":"build","message":"passed","build_id":"build-123","job_id":"xcode-version"}
`,
		"build-status.json": `{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed","result":{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","exit_code":0}}`,
		"status.json":       `{"machine_id":"machine-123","recent_builds":[{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed"}]}`,
		"app.log":           "app stdout\n",
		"app.err":           "app stderr\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteAppLaunchEvidence(AppLaunchEvidenceWriteOptions{
		OutputPath:          filepath.Join(dir, "app-launch-evidence.json"),
		TunnelMode:          "off",
		MachineID:           "machine-123",
		JobID:               "xcode-version",
		RequestID:           "request-123",
		BuildID:             "build-123",
		BuildLogPath:        filepath.Join(sourceDir, "build.ndjson"),
		StatusJSONPath:      filepath.Join(sourceDir, "status.json"),
		BuildStatusJSONPath: filepath.Join(sourceDir, "build-status.json"),
		AppLogPath:          filepath.Join(sourceDir, "app.log"),
		AppStderrPath:       filepath.Join(sourceDir, "app.err"),
		Now:                 fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
}

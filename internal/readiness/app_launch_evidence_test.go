package readiness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAppLaunchEvidenceWritesValidatedReceipt(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := writeAppLaunchSourceFiles(t, sourceDir, false)
	output := filepath.Join(dir, "evidence", "app-launch.json")

	err := WriteAppLaunchEvidence(AppLaunchEvidenceWriteOptions{
		OutputPath:          output,
		AppPath:             appPath,
		TunnelMode:          "off",
		MachineID:           "machine-123",
		JobID:               "xcode-version",
		RequestID:           "request-123",
		BuildID:             "build-123",
		BuildLogPath:        paths["build"],
		StatusJSONPath:      paths["status"],
		BuildStatusJSONPath: paths["build-status"],
		AppLogPath:          paths["app-log"],
		AppStderrPath:       paths["app-err"],
		Now:                 fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if check := appLaunchEvidenceCheck(output, appPath); check.Status != StatusPass {
		t.Fatalf("expected validated app launch evidence, got %+v", check)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"kind": "transwarp-app-launch-evidence"`) {
		t.Fatalf("receipt did not contain app launch kind: %s", data)
	}
	if !strings.Contains(string(data), `"exit_code": 0`) {
		t.Fatalf("receipt did not contain terminal exit code: %s", data)
	}
	for field, expected := range hashes {
		if !strings.Contains(string(data), `"`+field+`": "`+expected+`"`) {
			t.Fatalf("receipt did not contain %s=%s: %s", field, expected, data)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(output), "app-launch-build.ndjson")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteAppLaunchEvidenceWritesQuickTunnelProof(t *testing.T) {
	dir := t.TempDir()
	appPath, _ := writeCleanMacBundleFixture(t, dir)
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := writeAppLaunchSourceFiles(t, sourceDir, true)
	output := filepath.Join(dir, "evidence", "app-launch.json")

	err := WriteAppLaunchEvidence(AppLaunchEvidenceWriteOptions{
		OutputPath:                output,
		AppPath:                   appPath,
		TunnelMode:                "quick",
		PublicURL:                 "https://quick.trycloudflare.com",
		MachineID:                 "machine-123",
		JobID:                     "xcode-version",
		RequestID:                 "request-123",
		BuildID:                   "build-123",
		TunnelReady:               true,
		PublicStatusAuthenticated: true,
		BuildLogPath:              paths["build"],
		StatusJSONPath:            paths["status"],
		BuildStatusJSONPath:       paths["build-status"],
		PublicDiagnoseLogPath:     paths["diagnose"],
		PublicDispatchLogPath:     paths["dispatch"],
		AppLogPath:                paths["app-log"],
		AppStderrPath:             paths["app-err"],
		Now:                       fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if check := appLaunchEvidenceCheck(output, appPath); check.Status != StatusPass {
		t.Fatalf("expected validated quick app launch evidence, got %+v", check)
	}
}

func TestAppLaunchEvidenceRejectsDifferentAppBundle(t *testing.T) {
	dir := t.TempDir()
	appPath, _ := writeCleanMacBundleFixture(t, filepath.Join(dir, "launched"))
	otherAppPath, _ := writeCleanMacBundleFixture(t, filepath.Join(dir, "audited"))
	if err := os.WriteFile(filepath.Join(otherAppPath, "Contents", "MacOS", "Transwarp"), []byte("different app executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := writeAppLaunchSourceFiles(t, sourceDir, false)
	output := filepath.Join(dir, "evidence", "app-launch.json")
	if err := WriteAppLaunchEvidence(AppLaunchEvidenceWriteOptions{
		OutputPath:          output,
		AppPath:             appPath,
		TunnelMode:          "off",
		MachineID:           "machine-123",
		JobID:               "xcode-version",
		RequestID:           "request-123",
		BuildID:             "build-123",
		BuildLogPath:        paths["build"],
		StatusJSONPath:      paths["status"],
		BuildStatusJSONPath: paths["build-status"],
		AppLogPath:          paths["app-log"],
		AppStderrPath:       paths["app-err"],
		Now:                 fixedNow,
	}); err != nil {
		t.Fatal(err)
	}

	check := appLaunchEvidenceCheck(output, otherAppPath)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "app_executable_sha256") {
		t.Fatalf("expected app hash mismatch, got %+v", check)
	}
}

func TestWriteAppLaunchEvidenceRequiresQuickTunnelCompanions(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := writeAppLaunchSourceFiles(t, sourceDir, false)

	err := WriteAppLaunchEvidence(AppLaunchEvidenceWriteOptions{
		OutputPath:          filepath.Join(dir, "evidence", "app-launch.json"),
		TunnelMode:          "quick",
		PublicURL:           "https://quick.trycloudflare.com",
		MachineID:           "machine-123",
		JobID:               "xcode-version",
		RequestID:           "request-123",
		BuildID:             "build-123",
		TunnelReady:         true,
		BuildLogPath:        paths["build"],
		StatusJSONPath:      paths["status"],
		BuildStatusJSONPath: paths["build-status"],
		AppLogPath:          paths["app-log"],
		AppStderrPath:       paths["app-err"],
	})
	if err == nil || !strings.Contains(err.Error(), "public diagnose log") {
		t.Fatalf("expected missing public diagnose log error, got %v", err)
	}
}

func TestAppLaunchEvidenceRequiresTerminalExitCode(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := writeAppLaunchSourceFiles(t, sourceDir, false)
	output := filepath.Join(dir, "evidence", "app-launch.json")
	if err := WriteAppLaunchEvidence(AppLaunchEvidenceWriteOptions{
		OutputPath:          output,
		TunnelMode:          "off",
		MachineID:           "machine-123",
		JobID:               "xcode-version",
		RequestID:           "request-123",
		BuildID:             "build-123",
		BuildLogPath:        paths["build"],
		StatusJSONPath:      paths["status"],
		BuildStatusJSONPath: paths["build-status"],
		AppLogPath:          paths["app-log"],
		AppStderrPath:       paths["app-err"],
		Now:                 fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(data), "\n\t\"exit_code\": 0,", "", 1)
	if modified == string(data) {
		t.Fatal("fixture did not contain exit_code")
	}
	if err := os.WriteFile(output, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}

	check := appLaunchEvidenceCheck(output)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "exit_code") {
		t.Fatalf("expected exit_code missing detail, got %+v", check)
	}
}

func writeAppLaunchSourceFiles(t *testing.T, dir string, quick bool) map[string]string {
	t.Helper()
	status := `{"machine_id":"machine-123","recent_builds":[{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed"}]}`
	if quick {
		status = `{"machine_id":"machine-123","public_url":"https://quick.trycloudflare.com","tunnel":{"mode":"quick","ready":true},"recent_builds":[{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed"}]}`
	}
	files := map[string]string{
		"build":        `{"kind":"log","message":"Xcode 26.6","build_id":"build-123","job_id":"xcode-version"}` + "\n" + `{"kind":"build","message":"passed","build_id":"build-123","job_id":"xcode-version"}` + "\n",
		"build-status": `{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed","result":{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","exit_code":0}}`,
		"status":       status,
		"app-log":      "app stdout\n",
		"app-err":      "app stderr\n",
	}
	if quick {
		files["diagnose"] = "[ok] authenticated status reachable for Transwarp Launch Smoke (machine-123)\n[ok] tunnel mode=quick state=running connected=true ready=true\n[ok] runner public_url=https://quick.trycloudflare.com\n[ok] job \"xcode-version\" is advertised\ndiagnosis passed\n"
		files["dispatch"] = "public url: https://quick.trycloudflare.com\n[build] starting Xcode Version\nXcode 26.6\n[build] passed\n[result] request_id request-123\n[result] build_id build-123\n[result] job_id xcode-version\n"
	}
	paths := map[string]string{}
	for name, content := range files {
		path := filepath.Join(dir, name+".log")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	return paths
}

package readiness

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
}

func TestOverallStatusTreatsMissingAsIncomplete(t *testing.T) {
	checks := []Check{
		{ID: "local", Status: StatusPass},
		{ID: "named_tunnel", Status: StatusMissing},
	}
	if status := overallStatus(checks, false); status != StatusMissing {
		t.Fatalf("expected missing status, got %s", status)
	}
	if status := overallStatus(checks, true); status != StatusFail {
		t.Fatalf("expected strict missing to fail, got %s", status)
	}
}

func TestOverallStatusAllowsWarningsOutsideStrictMode(t *testing.T) {
	checks := []Check{
		{ID: "codesign", Status: StatusWarning},
	}
	if status := overallStatus(checks, false); status != StatusWarning {
		t.Fatalf("expected warning status, got %s", status)
	}
	if status := overallStatus(checks, true); status != StatusFail {
		t.Fatalf("expected strict warning to fail, got %s", status)
	}
}

func TestReportSummaryLinesIncludesCountsAndNonPassingChecks(t *testing.T) {
	report := Report{
		Overall: StatusMissing,
		Checks: []Check{
			{ID: "bundle", Status: StatusPass, Summary: "Bundle exists"},
			{ID: "codesign", Status: StatusWarning, Summary: "Distribution signature missing", Detail: "ad-hoc signed"},
			{ID: "named_tunnel", Status: StatusMissing, Summary: "Named tunnel evidence missing", Detail: "provide receipt\nrun named tunnel smoke"},
		},
	}

	lines := report.SummaryLines()
	if lines[0] != "overall=missing checks=1 pass, 1 warning, 1 missing, 0 fail" {
		t.Fatalf("unexpected summary header: %q", lines[0])
	}
	if len(lines) != 3 {
		t.Fatalf("expected only non-passing checks after header, got %#v", lines)
	}
	if !strings.Contains(lines[1], "warning: codesign") {
		t.Fatalf("expected warning line, got %#v", lines)
	}
	if !strings.Contains(lines[2], "missing: named_tunnel") {
		t.Fatalf("expected missing line, got %#v", lines)
	}
	if strings.Contains(lines[2], "\n") {
		t.Fatalf("expected one-line details, got %q", lines[2])
	}
}

func TestExitCodeAllowingIncompleteStillFailsHardFailures(t *testing.T) {
	report := Report{
		Overall: StatusFail,
		Checks: []Check{
			{ID: "bundle", Status: StatusPass},
			{ID: "manifest", Status: StatusFail},
			{ID: "named_tunnel", Status: StatusMissing},
		},
	}

	if code := report.ExitCodeAllowingIncomplete(); code == 0 {
		t.Fatal("expected hard failure to remain nonzero")
	}
}

func TestExitCodeAllowingIncompleteKeepsStrictModeStrict(t *testing.T) {
	report := Report{
		Strict:  true,
		Overall: StatusFail,
		Checks: []Check{
			{ID: "bundle", Status: StatusPass},
			{ID: "named_tunnel", Status: StatusMissing},
		},
	}

	if code := report.ExitCodeAllowingIncomplete(); code == 0 {
		t.Fatal("expected strict missing evidence to remain nonzero")
	}
}

func TestExitCodeAllowingIncompleteAllowsWarningsAndMissingEvidence(t *testing.T) {
	report := Report{
		Overall: StatusMissing,
		Checks: []Check{
			{ID: "bundle", Status: StatusPass},
			{ID: "codesign", Status: StatusWarning},
			{ID: "named_tunnel", Status: StatusMissing},
		},
	}

	if code := report.ExitCodeAllowingIncomplete(); code != 0 {
		t.Fatalf("expected warning/missing-only report to pass in incomplete mode, got %d", code)
	}
}

func TestReadReportDecodesStoredAuditJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transwarp-audit.json")
	err := os.WriteFile(path, []byte(`{
		"generated_at": "2026-07-26T11:30:00Z",
		"app_path": ".build/Transwarp.app",
		"release_archive_path": ".build/Transwarp-release.zip",
		"strict": false,
		"overall": "missing",
		"checks": [
			{"id": "bundle", "status": "pass", "summary": "Bundle exists"},
			{"id": "named_tunnel", "status": "missing", "summary": "Named tunnel evidence missing"}
		]
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	report, err := ReadReport(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Overall != StatusMissing {
		t.Fatalf("unexpected overall status: %s", report.Overall)
	}
	if len(report.Checks) != 2 {
		t.Fatalf("unexpected checks: %#v", report.Checks)
	}
	if !strings.Contains(report.SummaryLines()[1], "missing: named_tunnel") {
		t.Fatalf("unexpected summary: %#v", report.SummaryLines())
	}
}

func TestReadReportRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transwarp-audit.json")
	err := os.WriteFile(path, []byte(`{
		"generated_at": "2026-07-26T11:30:00Z",
		"app_path": ".build/Transwarp.app",
		"release_archive_path": ".build/Transwarp-release.zip",
		"strict": false,
		"overall": "missing",
		"unexpected": true,
		"checks": []
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ReadReport(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestReadReportRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transwarp-audit.json")
	err := os.WriteFile(path, []byte(`{
		"generated_at": "2026-07-26T11:30:00Z",
		"app_path": ".build/Transwarp.app",
		"release_archive_path": ".build/Transwarp-release.zip",
		"strict": false,
		"overall": "missing",
		"checks": []
	} {"overall":"pass"}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ReadReport(path)
	if err == nil || !strings.Contains(err.Error(), "one object") {
		t.Fatalf("expected trailing JSON error, got %v", err)
	}
}

func TestValidateReceiptShapeAcceptsExpectedReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"status": "pass"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ValidateReceiptShape(path, "transwarp-clean-mac-evidence"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateReceiptShapeRejectsUnexpectedKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := os.WriteFile(path, []byte(`{
		"kind": "wrong",
		"schema_version": 1,
		"status": "pass"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ValidateReceiptShape(path, "transwarp-clean-mac-evidence")
	if err == nil || !strings.Contains(err.Error(), "kind must be transwarp-clean-mac-evidence") {
		t.Fatalf("expected kind error, got %v", err)
	}
}

func TestValidateReceiptShapeRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := os.WriteFile(path, []byte(`{"kind":"transwarp-clean-mac-evidence","schema_version":1,"status":"pass"} {}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ValidateReceiptShape(path, "transwarp-clean-mac-evidence")
	if err == nil || !strings.Contains(err.Error(), "one object") {
		t.Fatalf("expected trailing JSON error, got %v", err)
	}
}

func TestExactValueCheckReportsMismatch(t *testing.T) {
	check := exactValueCheck("bundle_identifier", "wrong", "co.charliewil.transwarp", "Bundle identifier is correct")
	if check.Status != StatusFail {
		t.Fatalf("expected failed check, got %+v", check)
	}
	if check.Detail == "" {
		t.Fatalf("expected mismatch detail: %+v", check)
	}
}

func TestBundleChecksRequireManifestReleaseProvenance(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Transwarp.app")
	infoPlist := filepath.Join(app, "Contents", "Info.plist")
	manifest := filepath.Join(app, "Contents", "Resources", "TranswarpManifest.json")
	runnerPath := filepath.Join(app, "Contents", "Resources", "transwarp-runner")
	cloudflaredPath := filepath.Join(app, "Contents", "Resources", "cloudflared")
	for _, path := range []string{infoPlist, manifest, runnerPath, cloudflaredPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runnerHash, err := sha256File(runnerPath)
	if err != nil {
		t.Fatal(err)
	}
	cloudflaredHash, err := sha256File(cloudflaredPath)
	if err != nil {
		t.Fatal(err)
	}

	infoValues := map[string]string{
		"CFBundleIdentifier":         "co.charliewil.transwarp",
		"CFBundleShortVersionString": "0.2.0",
		"CFBundleVersion":            "42",
		"LSMinimumSystemVersion":     "14.0",
	}
	manifestValues := map[string]string{
		"schema_version":               "1",
		"bundle_identifier":            "co.charliewil.transwarp",
		"app_version":                  "0.2.0",
		"build_number":                 "42",
		"minimum_system_version":       "14.0",
		"architecture":                 "arm64",
		"runner_sha256":                runnerHash,
		"cloudflared_sha256":           cloudflaredHash,
		"cloudflared_version":          "cloudflared version 2026.7.0",
		"expected_cloudflared_version": "cloudflared version 2026.7.0",
	}
	auditor := auditor{
		ctx: context.Background(),
		options: Options{
			AppPath:    app,
			RunCommand: plistFixtureRunner(infoPlist, manifest, infoValues, manifestValues),
		},
	}

	checks := auditor.bundleChecks()
	for _, id := range []string{"app_version", "build_number", "manifest_app_version", "manifest_build_number"} {
		check := findCheck(t, checks, id)
		if check.Status != StatusPass {
			t.Fatalf("expected %s to pass, got %+v", id, check)
		}
	}
	check := findCheck(t, checks, "cloudflared_version_policy")
	if check.Status != StatusPass {
		t.Fatalf("expected cloudflared_version_policy to pass, got %+v", check)
	}
}

func TestBundleChecksFailWhenManifestBuildDoesNotMatchApp(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Transwarp.app")
	infoPlist := filepath.Join(app, "Contents", "Info.plist")
	manifest := filepath.Join(app, "Contents", "Resources", "TranswarpManifest.json")
	for _, path := range []string{infoPlist, manifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	infoValues := map[string]string{
		"CFBundleIdentifier":         "co.charliewil.transwarp",
		"CFBundleShortVersionString": "0.2.0",
		"CFBundleVersion":            "42",
		"LSMinimumSystemVersion":     "14.0",
	}
	manifestValues := map[string]string{
		"schema_version":               "1",
		"bundle_identifier":            "co.charliewil.transwarp",
		"app_version":                  "0.2.0",
		"build_number":                 "41",
		"minimum_system_version":       "14.0",
		"architecture":                 "arm64",
		"cloudflared_version":          "cloudflared version 2026.7.0",
		"expected_cloudflared_version": "cloudflared version 2026.7.0",
	}
	auditor := auditor{
		ctx: context.Background(),
		options: Options{
			AppPath:    app,
			RunCommand: plistFixtureRunner(infoPlist, manifest, infoValues, manifestValues),
		},
	}

	check := findCheck(t, auditor.bundleChecks(), "manifest_build_number")
	if check.Status != StatusFail {
		t.Fatalf("expected manifest_build_number to fail, got %+v", check)
	}
}

func TestBundleChecksWarnWhenCloudflaredVersionPolicyIsMissing(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Transwarp.app")
	infoPlist := filepath.Join(app, "Contents", "Info.plist")
	manifest := filepath.Join(app, "Contents", "Resources", "TranswarpManifest.json")
	runnerPath := filepath.Join(app, "Contents", "Resources", "transwarp-runner")
	cloudflaredPath := filepath.Join(app, "Contents", "Resources", "cloudflared")
	for _, path := range []string{infoPlist, manifest, runnerPath, cloudflaredPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runnerHash, err := sha256File(runnerPath)
	if err != nil {
		t.Fatal(err)
	}
	cloudflaredHash, err := sha256File(cloudflaredPath)
	if err != nil {
		t.Fatal(err)
	}

	auditor := auditor{
		ctx: context.Background(),
		options: Options{
			AppPath: app,
			RunCommand: plistFixtureRunner(infoPlist, manifest, map[string]string{
				"CFBundleIdentifier":         "co.charliewil.transwarp",
				"CFBundleShortVersionString": "0.2.0",
				"CFBundleVersion":            "42",
				"LSMinimumSystemVersion":     "14.0",
			}, map[string]string{
				"schema_version":         "1",
				"bundle_identifier":      "co.charliewil.transwarp",
				"app_version":            "0.2.0",
				"build_number":           "42",
				"minimum_system_version": "14.0",
				"architecture":           "arm64",
				"runner_sha256":          runnerHash,
				"cloudflared_sha256":     cloudflaredHash,
				"cloudflared_version":    "cloudflared version 2026.7.0",
			}),
		},
	}

	check := findCheck(t, auditor.bundleChecks(), "cloudflared_version_policy")
	if check.Status != StatusWarning {
		t.Fatalf("expected warning check, got %+v", check)
	}
}

func TestBundleChecksCanEnforceCloudflaredVersionPolicyFromOptions(t *testing.T) {
	check := cloudflaredVersionPolicyCheck("cloudflared version 2026.7.0", "cloudflared version 2026.6.0")
	if check.Status != StatusFail {
		t.Fatalf("expected failed version policy check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "2026.6.0") || !strings.Contains(check.Detail, "2026.7.0") {
		t.Fatalf("expected mismatch detail, got %+v", check)
	}
}

func TestNotarizationStapleCheckWarnsOutsideStrictMode(t *testing.T) {
	auditor := auditor{
		ctx: context.Background(),
		options: Options{
			RunCommand: func(context.Context, string, ...string) ([]byte, error) {
				return []byte("The staple and validate action failed!"), nil
			},
		},
	}

	check := auditor.notarizationStapleCheck("Transwarp.app")
	if check.Status != StatusWarning {
		t.Fatalf("expected warning check, got %+v", check)
	}
}

func TestNotarizationStapleCheckFailsInStrictMode(t *testing.T) {
	auditor := auditor{
		ctx: context.Background(),
		options: Options{
			Strict: true,
			RunCommand: func(context.Context, string, ...string) ([]byte, error) {
				return []byte("The staple and validate action failed!"), nil
			},
		},
	}

	check := auditor.notarizationStapleCheck("Transwarp.app")
	if check.Status != StatusFail {
		t.Fatalf("expected failed check, got %+v", check)
	}
}

func TestNotarizationStapleCheckPassesWhenStaplerValidates(t *testing.T) {
	auditor := auditor{
		ctx: context.Background(),
		options: Options{
			RunCommand: func(context.Context, string, ...string) ([]byte, error) {
				return []byte("The validate action worked!"), nil
			},
		},
	}

	check := auditor.notarizationStapleCheck("Transwarp.app")
	if check.Status != StatusPass {
		t.Fatalf("expected passed check, got %+v", check)
	}
}

func TestCleanMacEvidenceRejectsPlainValidationLog(t *testing.T) {
	dir := t.TempDir()
	appPath, _ := writeCleanMacBundleFixture(t, dir)
	path := filepath.Join(dir, "clean-mac.log")
	err := os.WriteFile(path, []byte("clean-mac-validation\nGatekeeper accepted\nfirst launch passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
}

func TestCleanMacEvidencePassesWithStructuredReceipt(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["clean-mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"app": "Transwarp.app",
		"architecture": "arm64",
		"app_executable_sha256": "`+hashes["app_executable_sha256"]+`",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean-mac-123",
		"job_id": "clean-mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"exit_code": 0,
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
			"machine_id_matched": true,
			"job_advertised": true,
			"first_launch_passed": true,
			"build_started": true,
			"log_stream_passed": true,
			"build_status_passed": true,
			"recent_build_recorded": true,
			"codesign_log": "clean-mac-codesign.log",
			"stapler_log": "clean-mac-stapler.log",
			"gatekeeper_log": "clean-mac-gatekeeper.log",
			"status_json": "clean-mac-status.json",
			"build_log": "clean-mac-build.ndjson",
			"build_status_json": "clean-mac-build-status.json",
			"app_log": "clean-mac-app.log",
			"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusPass {
		t.Fatalf("expected passed check, got %+v", check)
	}
}

func TestWriteCleanMacEvidenceWritesSelfContainedReceipt(t *testing.T) {
	dir := t.TempDir()
	appPath, _ := writeCleanMacBundleFixture(t, dir)
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(sourceDir, "status.json")
	buildLogPath := filepath.Join(sourceDir, "build.ndjson")
	buildStatusPath := filepath.Join(sourceDir, "build-status.json")
	codesignLogPath := filepath.Join(sourceDir, "codesign.log")
	staplerLogPath := filepath.Join(sourceDir, "stapler.log")
	gatekeeperLogPath := filepath.Join(sourceDir, "gatekeeper.log")
	appLogPath := filepath.Join(sourceDir, "stdout.log")
	appStderrPath := filepath.Join(sourceDir, "stderr.log")
	if err := os.WriteFile(statusPath, []byte(`{"machine_id":"clean-mac-123","jobs":["clean-mac-launch"],"recent_builds":[{"build_id":"build-123","request_id":"request-123","status":"passed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildLogPath, []byte(`{"kind":"build","message":"starting Clean Mac Launch","build_id":"build-123","job_id":"clean-mac-launch"}
{"kind":"build","message":"passed","build_id":"build-123","job_id":"clean-mac-launch"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildStatusPath, []byte(`{"build_id":"build-123","job_id":"clean-mac-launch","request_id":"request-123","status":"passed","result":{"build_id":"build-123","job_id":"clean-mac-launch","request_id":"request-123","exit_code":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codesignLogPath, []byte("Transwarp.app: valid on disk\nTranswarp.app: satisfies its Designated Requirement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staplerLogPath, []byte("The validate action worked!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatekeeperLogPath, []byte("Transwarp.app: accepted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appLogPath, []byte("clean Mac first launch stdout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appStderrPath, []byte("clean Mac first launch stderr\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(dir, "evidence", "clean-mac-evidence.json")
	err := WriteCleanMacEvidence(CleanMacEvidenceWriteOptions{
		OutputPath:        outputPath,
		AppPath:           appPath,
		Architecture:      "arm64",
		MacOS:             "macOS 15.6",
		MachineID:         "clean-mac-123",
		JobID:             "clean-mac-launch",
		RequestID:         "request-123",
		BuildID:           "build-123",
		StatusJSONPath:    statusPath,
		BuildLogPath:      buildLogPath,
		BuildStatusPath:   buildStatusPath,
		CodesignLogPath:   codesignLogPath,
		StaplerLogPath:    staplerLogPath,
		GatekeeperLogPath: gatekeeperLogPath,
		AppLogPath:        appLogPath,
		AppStderrPath:     appStderrPath,
		Now:               fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	report := EvidenceOnly(Options{
		AppPath:          appPath,
		CleanMacEvidence: outputPath,
		Now:              fixedNow,
	})
	if report.Overall != StatusPass {
		t.Fatalf("expected evidence-only report to pass, got %+v", report)
	}
	check := findCheck(t, report.Checks, "clean_mac_validation")
	if check.Status != StatusPass {
		t.Fatalf("expected clean-Mac evidence to pass, got %+v", check)
	}

	for _, name := range []string{
		"clean-mac-status.json",
		"clean-mac-build.ndjson",
		"clean-mac-build-status.json",
		"clean-mac-codesign.log",
		"clean-mac-stapler.log",
		"clean-mac-gatekeeper.log",
		"clean-mac-app.log",
		"clean-mac-app.err",
	} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(outputPath), name)); err != nil {
			t.Fatalf("expected copied companion %s: %v", name, err)
		}
	}
	receipt, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(receipt), `"generated_at": "2026-07-27T12:00:00Z"`) {
		t.Fatalf("expected fixed generated_at in receipt:\n%s", receipt)
	}
	if !strings.Contains(string(receipt), `"app": "Transwarp.app"`) {
		t.Fatalf("expected path-neutral app name in receipt:\n%s", receipt)
	}
	if !strings.Contains(string(receipt), `"exit_code": 0`) {
		t.Fatalf("expected terminal exit code in receipt:\n%s", receipt)
	}
	if strings.Contains(string(receipt), appPath) || strings.Contains(string(receipt), dir) {
		t.Fatalf("expected receipt to omit absolute local paths:\n%s", receipt)
	}
}

func TestCleanMacEvidenceRequiresTerminalExitCode(t *testing.T) {
	dir := t.TempDir()
	appPath, _ := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["clean-mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(dir, "clean-mac-evidence.json")
	if err := WriteCleanMacEvidence(CleanMacEvidenceWriteOptions{
		OutputPath:        outputPath,
		AppPath:           appPath,
		Architecture:      "arm64",
		MacOS:             "macOS 15.6",
		MachineID:         "clean-mac-123",
		JobID:             "clean-mac-launch",
		RequestID:         "request-123",
		BuildID:           "build-123",
		StatusJSONPath:    filepath.Join(dir, "clean-mac-status.json"),
		BuildLogPath:      filepath.Join(dir, "clean-mac-build.ndjson"),
		BuildStatusPath:   filepath.Join(dir, "clean-mac-build-status.json"),
		CodesignLogPath:   filepath.Join(dir, "clean-mac-codesign.log"),
		StaplerLogPath:    filepath.Join(dir, "clean-mac-stapler.log"),
		GatekeeperLogPath: filepath.Join(dir, "clean-mac-gatekeeper.log"),
		AppLogPath:        filepath.Join(dir, "clean-mac-app.log"),
		AppStderrPath:     filepath.Join(dir, "clean-mac-app.err"),
		Now:               fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(data), "\n\t\"exit_code\": 0,", "", 1)
	if modified == string(data) {
		t.Fatal("fixture did not contain exit_code")
	}
	if err := os.WriteFile(outputPath, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(outputPath, appPath)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "exit_code") {
		t.Fatalf("expected exit_code missing detail, got %+v", check)
	}
}

func TestCleanMacEvidenceRejectsAbsoluteAppPath(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["clean-mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"app": "`+appPath+`",
		"architecture": "arm64",
		"app_executable_sha256": "`+hashes["app_executable_sha256"]+`",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean-mac-123",
		"job_id": "clean-mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
		"machine_id_matched": true,
		"job_advertised": true,
		"first_launch_passed": true,
		"build_started": true,
		"log_stream_passed": true,
		"build_status_passed": true,
		"recent_build_recorded": true,
		"status_json": "clean-mac-status.json",
		"build_log": "clean-mac-build.ndjson",
		"build_status_json": "clean-mac-build-status.json",
		"app_log": "clean-mac-app.log",
		"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "app") {
		t.Fatalf("expected app detail, got %+v", check)
	}
}

func TestCleanMacEvidenceRequiresCommandReceipts(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-codesign.log"), []byte("Transwarp.app: valid on disk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["clean-mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"app": "Transwarp.app",
		"architecture": "arm64",
		"app_executable_sha256": "`+hashes["app_executable_sha256"]+`",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean-mac-123",
		"job_id": "clean-mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
		"machine_id_matched": true,
		"job_advertised": true,
		"first_launch_passed": true,
		"build_started": true,
		"log_stream_passed": true,
		"build_status_passed": true,
		"recent_build_recorded": true,
		"codesign_log": "clean-mac-codesign.log",
		"stapler_log": "clean-mac-stapler.log",
		"gatekeeper_log": "clean-mac-gatekeeper.log",
		"status_json": "clean-mac-status.json",
		"build_log": "clean-mac-build.ndjson",
		"build_status_json": "clean-mac-build-status.json",
		"app_log": "clean-mac-app.log",
		"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "codesign_log") {
		t.Fatalf("expected codesign_log detail, got %+v", check)
	}
}

func TestWriteCleanMacEvidenceRejectsMismatchedStatus(t *testing.T) {
	dir := t.TempDir()
	appPath, _ := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	statusPath := filepath.Join(dir, "clean-mac-status.json")
	if err := os.WriteFile(statusPath, []byte(`{"machine_id":"other-machine","jobs":["clean-mac-launch"],"recent_builds":[{"build_id":"build-123","request_id":"request-123","status":"passed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteCleanMacEvidence(CleanMacEvidenceWriteOptions{
		OutputPath:        filepath.Join(dir, "clean-mac-evidence.json"),
		AppPath:           appPath,
		Architecture:      "arm64",
		MacOS:             "macOS 15.6",
		MachineID:         "clean-mac-123",
		JobID:             "clean-mac-launch",
		RequestID:         "request-123",
		BuildID:           "build-123",
		StatusJSONPath:    statusPath,
		BuildLogPath:      filepath.Join(dir, "clean-mac-build.ndjson"),
		BuildStatusPath:   filepath.Join(dir, "clean-mac-build-status.json"),
		CodesignLogPath:   filepath.Join(dir, "clean-mac-codesign.log"),
		StaplerLogPath:    filepath.Join(dir, "clean-mac-stapler.log"),
		GatekeeperLogPath: filepath.Join(dir, "clean-mac-gatekeeper.log"),
		AppLogPath:        filepath.Join(dir, "clean-mac-app.log"),
		AppStderrPath:     filepath.Join(dir, "clean-mac-app.err"),
		Now:               fixedNow,
	})
	if err == nil || !strings.Contains(err.Error(), "clean-Mac evidence invalid") {
		t.Fatalf("expected mismatched status rejection, got %v", err)
	}
}

func TestCleanMacEvidenceRejectsUnsafeMachineID(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean/mac-123",
		"jobs": ["clean-mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"architecture": "arm64",
		"app_executable_sha256": "`+hashes["app_executable_sha256"]+`",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean/mac-123",
		"job_id": "clean-mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
		"machine_id_matched": true,
		"job_advertised": true,
		"first_launch_passed": true,
			"build_started": true,
			"log_stream_passed": true,
			"build_status_passed": true,
			"recent_build_recorded": true,
		"status_json": "clean-mac-status.json",
			"build_log": "clean-mac-build.ndjson",
			"build_status_json": "clean-mac-build-status.json",
		"app_log": "clean-mac-app.log",
		"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "machine_id") {
		t.Fatalf("expected unsafe machine_id rejection, got %+v", check)
	}
}

func TestCleanMacEvidenceRejectsUnsafeJobID(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["clean/mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"app": "Transwarp.app",
		"architecture": "arm64",
		"app_executable_sha256": "`+hashes["app_executable_sha256"]+`",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean-mac-123",
		"job_id": "clean/mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
		"machine_id_matched": true,
		"job_advertised": true,
		"first_launch_passed": true,
			"build_started": true,
			"log_stream_passed": true,
			"build_status_passed": true,
			"recent_build_recorded": true,
		"status_json": "clean-mac-status.json",
			"build_log": "clean-mac-build.ndjson",
			"build_status_json": "clean-mac-build-status.json",
		"app_log": "clean-mac-app.log",
		"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "job_id") {
		t.Fatalf("expected unsafe job_id rejection, got %+v", check)
	}
}

func TestCleanMacEvidenceRequiresAppLogCompanions(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["clean-mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"app": "Transwarp.app",
		"architecture": "arm64",
		"app_executable_sha256": "`+hashes["app_executable_sha256"]+`",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean-mac-123",
		"job_id": "clean-mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
		"machine_id_matched": true,
		"job_advertised": true,
		"first_launch_passed": true,
			"build_started": true,
			"log_stream_passed": true,
			"build_status_passed": true,
			"recent_build_recorded": true,
		"status_json": "clean-mac-status.json",
			"build_log": "clean-mac-build.ndjson",
			"build_status_json": "clean-mac-build-status.json",
		"app_log": "clean-mac-app.log",
		"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "app_log") || !strings.Contains(check.Detail, "app_stderr") {
		t.Fatalf("expected app companion detail, got %+v", check)
	}
}

func TestCleanMacEvidenceRequiresMatchingStatusJSON(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["other-job"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"app": "Transwarp.app",
		"architecture": "arm64",
		"app_executable_sha256": "`+hashes["app_executable_sha256"]+`",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean-mac-123",
		"job_id": "clean-mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
			"machine_id_matched": true,
			"job_advertised": true,
			"first_launch_passed": true,
			"build_started": true,
			"log_stream_passed": true,
			"build_status_passed": true,
			"recent_build_recorded": true,
			"status_json": "clean-mac-status.json",
			"build_log": "clean-mac-build.ndjson",
			"build_status_json": "clean-mac-build-status.json",
			"app_log": "clean-mac-app.log",
			"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "status_json") {
		t.Fatalf("expected status_json detail, got %+v", check)
	}
}

func TestCleanMacEvidenceRejectsAbsoluteStatusJSON(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	statusPath := filepath.Join(dir, "clean-mac-status.json")
	err := os.WriteFile(statusPath, []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["clean-mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"app": "Transwarp.app",
		"architecture": "arm64",
		"app_executable_sha256": "`+hashes["app_executable_sha256"]+`",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean-mac-123",
		"job_id": "clean-mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
			"machine_id_matched": true,
			"job_advertised": true,
			"first_launch_passed": true,
			"build_started": true,
			"log_stream_passed": true,
			"build_status_passed": true,
			"recent_build_recorded": true,
			"status_json": "`+statusPath+`",
			"build_log": "clean-mac-build.ndjson",
			"build_status_json": "clean-mac-build-status.json",
			"app_log": "clean-mac-app.log",
			"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "status_json") {
		t.Fatalf("expected status_json detail, got %+v", check)
	}
}

func TestCleanMacEvidenceRequiresMatchingBundleHashes(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, dir)
	writeCleanMacEvidenceCompanionLogs(t, dir)
	err := os.WriteFile(filepath.Join(dir, "clean-mac-status.json"), []byte(`{
		"machine_id": "clean-mac-123",
		"jobs": ["clean-mac-launch"],
		"recent_builds": [{"build_id":"build-123","request_id":"request-123","status":"passed"}]
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "clean-mac.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-clean-mac-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"app": "Transwarp.app",
		"architecture": "arm64",
		"app_executable_sha256": "wrong",
		"runner_sha256": "`+hashes["runner_sha256"]+`",
		"cloudflared_sha256": "`+hashes["cloudflared_sha256"]+`",
		"manifest_sha256": "`+hashes["manifest_sha256"]+`",
		"machine_id": "clean-mac-123",
		"job_id": "clean-mac-launch",
		"request_id": "request-123",
		"build_id": "build-123",
		"strict_codesign": true,
		"notarization_staple": true,
		"gatekeeper_accepted": true,
		"authenticated_status": true,
			"machine_id_matched": true,
			"job_advertised": true,
			"first_launch_passed": true,
			"build_started": true,
			"log_stream_passed": true,
			"build_status_passed": true,
			"recent_build_recorded": true,
			"status_json": "clean-mac-status.json",
			"build_log": "clean-mac-build.ndjson",
			"build_status_json": "clean-mac-build-status.json",
			"app_log": "clean-mac-app.log",
			"app_stderr": "clean-mac-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := cleanMacEvidenceCheck(path, appPath)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "app_executable_sha256") {
		t.Fatalf("expected app_executable_sha256 detail, got %+v", check)
	}
}

func writeCleanMacEvidenceCompanionLogs(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-build.ndjson"), []byte(`{"kind":"build","message":"starting Clean Mac Launch","build_id":"build-123","job_id":"clean-mac-launch"}
{"kind":"build","message":"passed","build_id":"build-123","job_id":"clean-mac-launch"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-build-status.json"), []byte(`{"build_id":"build-123","job_id":"clean-mac-launch","request_id":"request-123","status":"passed","result":{"build_id":"build-123","job_id":"clean-mac-launch","request_id":"request-123","exit_code":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-codesign.log"), []byte("Transwarp.app: valid on disk\nTranswarp.app: satisfies its Designated Requirement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-stapler.log"), []byte("The validate action worked!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-gatekeeper.log"), []byte("Transwarp.app: accepted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-app.log"), []byte("clean Mac first launch stdout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clean-mac-app.err"), []byte("clean Mac first launch stderr\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCopyEvidenceFileRejectsDestinationSymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.log")
	target := filepath.Join(dir, "target.log")
	destination := filepath.Join(dir, "destination.log")
	if err := os.WriteFile(source, []byte("source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, destination); err != nil {
		t.Fatal(err)
	}

	err := copyEvidenceFile(source, destination, "test evidence")
	if err == nil || !strings.Contains(err.Error(), "destination must be a regular file") {
		t.Fatalf("expected destination symlink rejection, got %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "target\n" {
		t.Fatalf("destination symlink target was modified: %q", data)
	}
}

func writeCleanMacBundleFixture(t *testing.T, dir string) (string, map[string]string) {
	t.Helper()
	appPath := filepath.Join(dir, "Transwarp.app")
	files := map[string]string{
		"app_executable_sha256": filepath.Join(appPath, "Contents", "MacOS", "Transwarp"),
		"runner_sha256":         filepath.Join(appPath, "Contents", "Resources", "transwarp-runner"),
		"cloudflared_sha256":    filepath.Join(appPath, "Contents", "Resources", "cloudflared"),
		"manifest_sha256":       filepath.Join(appPath, "Contents", "Resources", "TranswarpManifest.json"),
	}
	for field, path := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(field), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	hashes := map[string]string{}
	for field, path := range files {
		hash, err := sha256File(path)
		if err != nil {
			t.Fatal(err)
		}
		hashes[field] = hash
	}
	return appPath, hashes
}

func TestSelfHostedEvidenceRequiresArm64AndXcodeProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "self-hosted.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-self-hosted-mac-readiness",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"architecture": "x86_64"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := selfHostedEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
}

func TestSelfHostedEvidencePassesWithArm64AndXcodeProof(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "self-hosted-readiness.log"), []byte("self-hosted Mac readiness passed\narchitecture=arm64\nxcode=Xcode 26.6 Build version 17F113\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "self-hosted.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-self-hosted-mac-readiness",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"architecture": "arm64",
		"macos": "26.5.2",
		"developer_dir": "/Applications/Xcode.app/Contents/Developer",
		"xcode": "Xcode 26.6 Build version 17F113",
		"source_log": "self-hosted-readiness.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := selfHostedEvidenceCheck(path)
	if check.Status != StatusPass {
		t.Fatalf("expected passed check, got %+v", check)
	}
}

func TestWriteSelfHostedEvidenceWritesAuditValidReceipt(t *testing.T) {
	dir := t.TempDir()
	sourceLog := filepath.Join(dir, "source.log")
	if err := os.WriteFile(sourceLog, []byte("self-hosted Mac readiness passed\narchitecture=arm64\nxcode=Xcode 26.6 Build version 17F113\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "evidence", "self-hosted-mac.json")

	err := WriteSelfHostedEvidence(SelfHostedEvidenceWriteOptions{
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
	})
	if err != nil {
		t.Fatal(err)
	}

	report := EvidenceOnly(Options{
		SelfHostedEvidence: outputPath,
		Now:                fixedNow,
	})
	if report.Overall != StatusPass {
		t.Fatalf("expected evidence-only report to pass, got %+v", report)
	}
	check := findCheck(t, report.Checks, "self_hosted_mac_readiness")
	if check.Status != StatusPass {
		t.Fatalf("expected self-hosted evidence to pass, got %+v", check)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(outputPath), "self-hosted-readiness.log")); err != nil {
		t.Fatalf("expected copied source log: %v", err)
	}
	receipt, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(receipt), `"generated_at": "2026-07-27T12:00:00Z"`) {
		t.Fatalf("expected fixed generated_at in receipt:\n%s", receipt)
	}
	if !strings.Contains(string(receipt), `"runner_name": "Mac Studio"`) {
		t.Fatalf("expected runner name in receipt:\n%s", receipt)
	}
}

func TestWriteSelfHostedEvidenceRejectsIncompleteSourceLog(t *testing.T) {
	dir := t.TempDir()
	sourceLog := filepath.Join(dir, "source.log")
	if err := os.WriteFile(sourceLog, []byte("architecture=arm64\nxcode=Xcode 26.6 Build version 17F113\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteSelfHostedEvidence(SelfHostedEvidenceWriteOptions{
		OutputPath:    filepath.Join(dir, "self-hosted-mac.json"),
		Architecture:  "arm64",
		MacOS:         "26.5.2",
		DeveloperDir:  "/Applications/Xcode.app/Contents/Developer",
		Xcode:         "Xcode 26.6 Build version 17F113",
		SourceLogPath: sourceLog,
		Now:           fixedNow,
	})
	if err == nil || !strings.Contains(err.Error(), "self-hosted evidence invalid") {
		t.Fatalf("expected incomplete source log rejection, got %v", err)
	}
}

func TestEvidenceReceiptRejectsTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "self-hosted-readiness.log"), []byte("self-hosted Mac readiness passed\narchitecture=arm64\nxcode=Xcode 26.6 Build version 17F113\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "self-hosted.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-self-hosted-mac-readiness",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"architecture": "arm64",
		"macos": "26.5.2",
		"developer_dir": "/Applications/Xcode.app/Contents/Developer",
		"xcode": "Xcode 26.6 Build version 17F113",
		"source_log": "self-hosted-readiness.log"
	} {"kind":"transwarp-self-hosted-mac-readiness"}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := selfHostedEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "one object") {
		t.Fatalf("expected trailing JSON detail, got %+v", check)
	}
}

func TestSelfHostedEvidenceRequiresSourceLog(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "self-hosted-readiness.log"), []byte("architecture=arm64\nxcode=Xcode 26.6 Build version 17F113\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "self-hosted.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-self-hosted-mac-readiness",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"architecture": "arm64",
		"macos": "26.5.2",
		"developer_dir": "/Applications/Xcode.app/Contents/Developer",
		"xcode": "Xcode 26.6 Build version 17F113",
		"source_log": "self-hosted-readiness.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := selfHostedEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestAppLaunchEvidencePassesWithBuildAndStatusProof(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-launch-build.ndjson"), []byte(`{"kind":"build","message":"starting Xcode Version","build_id":"build-123","job_id":"xcode-version"}
{"kind":"log","message":"Xcode 26.6","build_id":"build-123","job_id":"xcode-version"}
{"kind":"build","message":"passed","build_id":"build-123","job_id":"xcode-version"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-build-status.json"), []byte(`{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed","result":{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","exit_code":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-status.json"), []byte(`{"machine_id":"machine-123","recent_builds":[{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-app.log"), []byte("app stdout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-app.err"), []byte("app stderr\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app-launch.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-app-launch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"machine_id": "machine-123",
		"job_id": "xcode-version",
		"request_id": "request-123",
		"build_id": "build-123",
		"exit_code": 0,
		"keychain_migrated": true,
		"app_launch_passed": true,
		"helper_status_authenticated": true,
		"build_started": true,
		"log_stream_passed": true,
		"build_status_passed": true,
		"recent_build_recorded": true,
		"build_log": "app-launch-build.ndjson",
		"build_status_json": "app-launch-build-status.json",
		"status_json": "app-launch-status.json",
		"app_log": "app-launch-app.log",
		"app_stderr": "app-launch-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := appLaunchEvidenceCheck(path)
	if check.Status != StatusPass {
		t.Fatalf("expected passed check, got %+v", check)
	}
}

func TestAppLaunchEvidencePassesWithQuickTunnelProof(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-launch-build.ndjson"), []byte(`{"kind":"build","message":"starting Xcode Version","build_id":"build-123","job_id":"xcode-version"}
{"kind":"log","message":"Xcode 26.6","build_id":"build-123","job_id":"xcode-version"}
{"kind":"build","message":"passed","build_id":"build-123","job_id":"xcode-version"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-build-status.json"), []byte(`{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed","result":{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","exit_code":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status := `{"machine_id":"machine-123","public_url":"https://quick.trycloudflare.com","tunnel":{"mode":"quick","ready":true},"recent_builds":[{"build_id":"build-123","job_id":"xcode-version","request_id":"request-123","status":"passed"}]}`
	if err := os.WriteFile(filepath.Join(dir, "app-launch-status.json"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-public-diagnose.log"), []byte("[ok] authenticated status reachable for Transwarp Launch Smoke (machine-123)\n[ok] tunnel mode=quick state=running connected=true ready=true\n[ok] runner public_url=https://quick.trycloudflare.com\n[ok] job \"xcode-version\" is advertised\ndiagnosis passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-public-dispatch.log"), []byte("public url: https://quick.trycloudflare.com\n[build] starting Xcode Version\nXcode 26.6\n[build] passed\n[result] recorded passed\n[result] request_id request-123\n[result] build_id build-123\n[result] job_id xcode-version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-app.log"), []byte("app stdout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-app.err"), []byte("app stderr\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app-launch.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-app-launch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"machine_id": "machine-123",
		"job_id": "xcode-version",
		"request_id": "request-123",
		"build_id": "build-123",
		"exit_code": 0,
		"tunnel_mode": "quick",
		"public_url": "https://quick.trycloudflare.com",
		"tunnel_ready": true,
		"public_status_authenticated": true,
		"keychain_migrated": true,
		"app_launch_passed": true,
		"helper_status_authenticated": true,
		"build_started": true,
		"log_stream_passed": true,
		"build_status_passed": true,
		"recent_build_recorded": true,
		"build_log": "app-launch-build.ndjson",
			"build_status_json": "app-launch-build-status.json",
			"status_json": "app-launch-status.json",
			"public_diagnose_log": "app-launch-public-diagnose.log",
			"public_dispatch_log": "app-launch-public-dispatch.log",
		"app_log": "app-launch-app.log",
		"app_stderr": "app-launch-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := appLaunchEvidenceCheck(path)
	if check.Status != StatusPass {
		t.Fatalf("expected passed check, got %+v", check)
	}
}

func TestAppLaunchEvidenceRequiresQuickTunnelPublicDiagnoseProof(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-launch-build.ndjson"), []byte(`{"kind":"log","message":"Xcode 26.6","build_id":"build-123"}
{"kind":"build","message":"passed","build_id":"build-123"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-build-status.json"), []byte(`{"build_id":"build-123","request_id":"request-123","status":"passed","result":{"build_id":"build-123","request_id":"request-123","exit_code":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-status.json"), []byte(`{"machine_id":"machine-123","public_url":"https://quick.trycloudflare.com","tunnel":{"mode":"quick","ready":true},"recent_builds":[{"build_id":"build-123","request_id":"request-123","status":"passed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-app.log"), []byte("app stdout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-app.err"), []byte("app stderr\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app-launch.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-app-launch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"machine_id": "machine-123",
		"job_id": "xcode-version",
		"request_id": "request-123",
		"build_id": "build-123",
		"exit_code": 0,
		"tunnel_mode": "quick",
		"public_url": "https://quick.trycloudflare.com",
		"tunnel_ready": true,
		"public_status_authenticated": true,
		"keychain_migrated": true,
		"app_launch_passed": true,
		"helper_status_authenticated": true,
		"build_started": true,
		"log_stream_passed": true,
		"build_status_passed": true,
		"recent_build_recorded": true,
		"build_log": "app-launch-build.ndjson",
		"build_status_json": "app-launch-build-status.json",
		"status_json": "app-launch-status.json",
		"public_diagnose_log": "app-launch-public-diagnose.log",
		"public_dispatch_log": "app-launch-public-dispatch.log",
		"app_log": "app-launch-app.log",
		"app_stderr": "app-launch-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := appLaunchEvidenceCheck(path)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "public_diagnose_log") {
		t.Fatalf("expected public_diagnose_log missing detail, got %+v", check)
	}
}

func TestAppLaunchEvidenceRequiresRecentBuildProof(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-launch-build.ndjson"), []byte(`{"kind":"log","message":"Xcode 26.6","build_id":"build-123"}
{"kind":"build","message":"passed","build_id":"build-123"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-build-status.json"), []byte(`{"build_id":"build-123","request_id":"request-123","status":"passed","result":{"build_id":"build-123","request_id":"request-123","exit_code":0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-status.json"), []byte(`{"machine_id":"machine-123","recent_builds":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-app.log"), []byte("app stdout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-launch-app.err"), []byte("app stderr\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app-launch.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-app-launch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"machine_id": "machine-123",
		"job_id": "xcode-version",
		"request_id": "request-123",
		"build_id": "build-123",
		"exit_code": 0,
		"keychain_migrated": true,
		"app_launch_passed": true,
		"helper_status_authenticated": true,
		"build_started": true,
		"log_stream_passed": true,
		"build_status_passed": true,
		"recent_build_recorded": true,
		"build_log": "app-launch-build.ndjson",
		"build_status_json": "app-launch-build-status.json",
		"status_json": "app-launch-status.json",
		"app_log": "app-launch-app.log",
		"app_stderr": "app-launch-app.err"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := appLaunchEvidenceCheck(path)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "status_json") {
		t.Fatalf("expected status_json missing detail, got %+v", check)
	}
}

func TestSelfHostedEvidenceRejectsAbsoluteSourceLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "self-hosted-readiness.log")
	err := os.WriteFile(logPath, []byte("self-hosted Mac readiness passed\narchitecture=arm64\nxcode=Xcode 26.6 Build version 17F113\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "self-hosted.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-self-hosted-mac-readiness",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"architecture": "arm64",
		"macos": "26.5.2",
		"developer_dir": "/Applications/Xcode.app/Contents/Developer",
		"xcode": "Xcode 26.6 Build version 17F113",
		"source_log": "`+logPath+`"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := selfHostedEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestSelfHostedEvidenceRejectsSymlinkSourceLog(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outsideLog := filepath.Join(outsideDir, "self-hosted-readiness.log")
	err := os.WriteFile(outsideLog, []byte("self-hosted Mac readiness passed\narchitecture=arm64\nxcode=Xcode 26.6 Build version 17F113\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideLog, filepath.Join(dir, "self-hosted-readiness.log")); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "self-hosted.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-self-hosted-mac-readiness",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"architecture": "arm64",
		"macos": "26.5.2",
		"developer_dir": "/Applications/Xcode.app/Contents/Developer",
		"xcode": "Xcode 26.6 Build version 17F113",
		"source_log": "self-hosted-readiness.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := selfHostedEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestEvidenceRequiresSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "self-hosted.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-self-hosted-mac-readiness",
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"architecture": "arm64",
		"xcode": "Xcode 26.6 Build version 17F113"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := selfHostedEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "schema_version") {
		t.Fatalf("expected schema_version detail, got %+v", check)
	}
}

func TestEvidenceRequiresGeneratedAtTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "self-hosted.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-self-hosted-mac-readiness",
		"schema_version": 1,
		"generated_at": "not a timestamp",
		"status": "pass",
		"architecture": "arm64",
		"xcode": "Xcode 26.6 Build version 17F113"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := selfHostedEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "generated_at") {
		t.Fatalf("expected generated_at detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRejectsPlainSmokeLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named-tunnel.log")
	err := os.WriteFile(path, []byte(`named-tunnel-smoke-coordinator
{"status":"passed"}
[result] recorded passed
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
}

func TestNamedTunnelEvidencePassesWithStructuredReceipt(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] target public_url=https://transwarp.example.com\n[ok] selected runner health reachable through public_url\n[ok] tunnel mode=named state=running connected=true ready=true\ndiagnosis passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("[build] starting Echo Smoke\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\"build-123\",\"job_id\":\"echo\",\"request_id\":\"request-123\",\"machine_id\":\"machine-123\",\"public_url\":\"https://transwarp.example.com\"}\nhello through named coordinator tunnel\n[result] recorded passed\n[result] request_id request-123\n[result] build_id build-123\n[result] job_id echo\n[result] machine_id machine-123\n[result] public_url https://transwarp.example.com\n[result] exit_code 0\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "runner.log"), []byte("[info] Started transwarp-runner\n[tunnel] INF Registered tunnel connection\n[tunnel] tunnel ready at https://transwarp.example.com\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "app.log"), []byte("[info] Started transwarp-runner\n[tunnel] INF Registered tunnel connection\n[tunnel] tunnel ready at https://transwarp.example.com\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "app.err"), []byte(""), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "targets-registered.json"), []byte(`[{"machine_id":"machine-123","public_url":"https://transwarp.example.com","accepting_builds":true,"jobs":["echo"]}]`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "targets-after-deregister.json"), []byte(`[]`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "results.json"), []byte(`[{"request_id":"request-123","build_id":"build-123","job_id":"echo","status":"passed","exit_code":0}]`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "named-tunnel.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"launch_mode": "app",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"app_launch_passed": true,
		"keychain_migrated": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"exit_code": 0,
		"machine_id": "machine-123",
		"diagnose_log": "diagnose.log",
		"dispatch_log": "dispatch.log",
		"runner_log": "runner.log",
		"app_log": "app.log",
		"app_stderr": "app.err",
		"results_json": "results.json",
		"targets_registered_json": "targets-registered.json",
		"targets_after_deregister_json": "targets-after-deregister.json"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusPass {
		t.Fatalf("expected passed check, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRequiresNamedTunnelStatusInDiagnoseLog(t *testing.T) {
	dir := t.TempDir()
	path := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	if err := os.WriteFile(filepath.Join(dir, "named-diagnose.log"), []byte("[ok] target public_url=https://transwarp.example.com\n[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "diagnose_log") {
		t.Fatalf("expected diagnose_log detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRequiresRunnerTunnelLog(t *testing.T) {
	dir := t.TempDir()
	path := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	if err := os.WriteFile(filepath.Join(dir, "named-runner.log"), []byte("{\"kind\":\"tunnel\",\"message\":\"started cloudflared\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "runner_log") {
		t.Fatalf("expected runner_log detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRejectsUnsafeBuildID(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] target public_url=https://transwarp.example.com\n[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("[build] starting Echo Smoke\n{\"build_id\":\"build/123\"}\nhello through named coordinator tunnel\n[result] recorded passed\n[result] request_id request-123\n[result] public_url https://transwarp.example.com\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "named-tunnel.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build/123",
		"request_id": "request-123",
		"diagnose_log": "diagnose.log",
		"dispatch_log": "dispatch.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "build_id") {
		t.Fatalf("expected unsafe build_id rejection, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRejectsUnsafeRequestID(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] target public_url=https://transwarp.example.com\n[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("[build] starting Echo Smoke\n{\"build_id\":\"build-123\"}\nhello through named coordinator tunnel\n[result] recorded passed\n[result] request_id request/123\n[result] public_url https://transwarp.example.com\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "named-tunnel.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build-123",
		"request_id": "request/123",
		"diagnose_log": "diagnose.log",
		"dispatch_log": "dispatch.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "request_id") {
		t.Fatalf("expected unsafe request_id rejection, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRequiresSmokeLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named-tunnel.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	for _, field := range []string{"diagnose_log", "dispatch_log", "build_id", "request_id", "machine_id"} {
		if !strings.Contains(check.Detail, field) {
			t.Fatalf("expected %s detail, got %+v", field, check)
		}
	}
}

func TestNamedTunnelEvidenceRequiresResultIDMarkers(t *testing.T) {
	dir := t.TempDir()
	path := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	dispatchLog := filepath.Join(dir, "named-dispatch.log")
	if err := os.WriteFile(dispatchLog, []byte("[build] starting Echo Smoke\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\"build-123\",\"job_id\":\"echo\",\"request_id\":\"request-123\",\"machine_id\":\"machine-123\",\"public_url\":\"https://transwarp.example.com\"}\nhello through named coordinator tunnel\n[result] recorded passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "dispatch_log") {
		t.Fatalf("expected dispatch_log detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRequiresTargetSnapshotFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] target public_url=https://transwarp.example.com\n[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("[build] starting Echo Smoke\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\"build-123\",\"job_id\":\"echo\",\"request_id\":\"request-123\",\"machine_id\":\"machine-123\",\"public_url\":\"https://transwarp.example.com\"}\nhello through named coordinator tunnel\n[result] recorded passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "named-tunnel.json")
	if err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"diagnose_log": "diagnose.log",
		"dispatch_log": "dispatch.log"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	for _, field := range []string{"targets_registered_json", "targets_after_deregister_json"} {
		if !strings.Contains(check.Detail, field) {
			t.Fatalf("expected %s detail, got %+v", field, check)
		}
	}
}

func TestNamedTunnelEvidenceRequiresRegisteredTargetSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	if err := os.WriteFile(filepath.Join(dir, "named-targets-registered.json"), []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "targets_registered_json") {
		t.Fatalf("expected targets_registered_json detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRequiresDeregisteredTargetSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	if err := os.WriteFile(filepath.Join(dir, "named-targets-after-deregister.json"), []byte(`[{"machine_id":"machine-123","public_url":"https://transwarp.example.com","accepting_builds":true,"jobs":["echo"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "targets_after_deregister_json") {
		t.Fatalf("expected targets_after_deregister_json detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRejectsParentDirectoryLogs(t *testing.T) {
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("{\"build_id\":\"build-123\"}\nhello through named coordinator tunnel\n[result] recorded passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(evidenceDir, "named-tunnel.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build-123",
		"diagnose_log": "../diagnose.log",
		"dispatch_log": "../dispatch.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	for _, field := range []string{"diagnose_log", "dispatch_log"} {
		if !strings.Contains(check.Detail, field) {
			t.Fatalf("expected %s detail, got %+v", field, check)
		}
	}
}

func TestNamedTunnelEvidenceRequiresBuildIDInDispatchLog(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] target public_url=https://transwarp.example.com\n[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("[build] starting Echo Smoke\n{\"build_id\":\"build-actual\"}\nhello through named coordinator tunnel\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "named-tunnel.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build-other",
		"job_id": "echo",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"diagnose_log": "diagnose.log",
		"dispatch_log": "dispatch.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "dispatch_log") {
		t.Fatalf("expected dispatch_log detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRequiresAcceptedBuildMetadataEvent(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] target public_url=https://transwarp.example.com\n[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("[build] starting Echo Smoke\nhello through named coordinator tunnel\n[result] recorded passed\n[result] build_id build-123\n[result] request_id request-123\n[result] machine_id machine-123\n[result] public_url https://transwarp.example.com\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "named-tunnel.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"diagnose_log": "diagnose.log",
		"dispatch_log": "dispatch.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "dispatch_log") {
		t.Fatalf("expected dispatch_log detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRequiresPublicURLInDiagnoseLog(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("[build] starting Echo Smoke\n{\"build_id\":\"build-123\"}\nhello through named coordinator tunnel\n[result] recorded passed\n[result] request_id request-123\n[result] public_url https://transwarp.example.com\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "named-tunnel.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"diagnose_log": "diagnose.log",
		"dispatch_log": "dispatch.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "diagnose_log") {
		t.Fatalf("expected diagnose_log detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRequiresPublicURLInDispatchLog(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "diagnose.log"), []byte("[ok] target public_url=https://transwarp.example.com\n[ok] selected runner health reachable through public_url\ndiagnosis passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "dispatch.log"), []byte("[build] starting Echo Smoke\n{\"build_id\":\"build-123\"}\nhello through named coordinator tunnel\n[result] recorded passed\n[result] request_id request-123\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "named-tunnel.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "https://transwarp.example.com",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"diagnose_log": "diagnose.log",
		"dispatch_log": "dispatch.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "dispatch_log") {
		t.Fatalf("expected dispatch_log detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRejectsHTTPPublicURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named-tunnel.json")
	err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"public_url": "http://127.0.0.1:8188",
		"coordinator": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "public_url") {
		t.Fatalf("expected public_url detail, got %+v", check)
	}
}

func TestNamedTunnelEvidenceRejectsNonBasePublicURL(t *testing.T) {
	tests := []struct {
		name      string
		publicURL string
	}{
		{name: "path", publicURL: "https://transwarp.example.com/status"},
		{name: "query", publicURL: "https://transwarp.example.com?token=secret"},
		{name: "fragment", publicURL: "https://transwarp.example.com#runner"},
		{name: "credentials", publicURL: "https://user:pass@transwarp.example.com"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "named-tunnel.json")
			err := os.WriteFile(path, []byte(`{
				"kind": "transwarp-named-tunnel-evidence",
				"schema_version": 1,
				"generated_at": "2026-07-26T11:30:00Z",
				"status": "pass",
				"tunnel_mode": "named",
				"public_url": "`+test.publicURL+`",
				"coordinator": true,
				"target_registered": true,
				"diagnose_passed": true,
				"dispatch_passed": true,
				"result_recorded": true,
				"deregistered": true
			}`), 0o600)
			if err != nil {
				t.Fatal(err)
			}

			check := namedTunnelEvidenceCheck(path)
			if check.Status != StatusMissing {
				t.Fatalf("expected missing check, got %+v", check)
			}
			if !strings.Contains(check.Detail, "public_url") {
				t.Fatalf("expected public_url detail, got %+v", check)
			}
		})
	}
}

func TestCIDispatchEvidenceRejectsLocalDispatchLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatch.log")
	err := os.WriteFile(path, []byte("[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
}

func TestCIDispatchEvidencePassesWithHostedGitHubActionsReceipt(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\npublic url: https://transwarp.example.com\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\"build-123\",\"job_id\":\"echo\",\"request_id\":\"request-123\",\"machine_id\":\"machine-123\",\"public_url\":\"https://transwarp.example.com\"}\nhello through named coordinator tunnel\n[build] passed\n[result] recorded passed\n[result] request_id request-123\n[result] build_id build-123\n[result] job_id echo\n[result] machine_id machine-123\n[result] public_url https://transwarp.example.com\n[result] exit_code 0\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "Linux",
		"runner_arch": "X64",
		"public_url": "https://transwarp.example.com",
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"exit_code": 0,
		"machine_id": "machine-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusPass {
		t.Fatalf("expected passed check, got %+v", check)
	}
}

func TestCIDispatchEvidenceRequiresResultIDMarkers(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\npublic url: https://transwarp.example.com\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\"build-123\",\"job_id\":\"echo\",\"request_id\":\"request-123\",\"machine_id\":\"machine-123\",\"public_url\":\"https://transwarp.example.com\"}\nhello through named coordinator tunnel\n[build] passed\n[result] recorded passed\n[result] exit_code 0\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"public_url": "https://transwarp.example.com",
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"exit_code": 0,
		"machine_id": "machine-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestCIDispatchEvidenceRejectsUnsafeBuildID(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\npublic url: https://transwarp.example.com\n{\"build_id\":\"build/123\"}\n[result] request_id request-123\n[build] passed\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"public_url": "https://transwarp.example.com",
		"build_id": "build/123",
		"request_id": "request-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "build_id") {
		t.Fatalf("expected unsafe build_id rejection, got %+v", check)
	}
}

func TestCIDispatchEvidenceRejectsUnsafeRequestID(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\npublic url: https://transwarp.example.com\n{\"build_id\":\"build-123\"}\n[result] request_id request/123\n[build] passed\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"public_url": "https://transwarp.example.com",
		"build_id": "build-123",
		"request_id": "request/123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "request_id") {
		t.Fatalf("expected unsafe request_id rejection, got %+v", check)
	}
}

func TestCIDispatchEvidenceRequiresAcceptedBuildMetadataEvent(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\npublic url: https://transwarp.example.com\n[result] build_id build-123\n[result] request_id request-123\n[result] machine_id machine-123\n[result] public_url https://transwarp.example.com\n[build] passed\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"public_url": "https://transwarp.example.com",
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestCIDispatchEvidenceRequiresPublicURL(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\n{\"build_id\":\"build-123\"}\n[build] passed\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"build_id": "build-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "public_url") {
		t.Fatalf("expected public_url detail, got %+v", check)
	}
}

func TestCIDispatchEvidenceRejectsAbsoluteSourceLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "named-tunnel-coordinator-smoke.log")
	err := os.WriteFile(logPath, []byte("diagnosis passed\n{\"build_id\":\"build-123\"}\n[build] passed\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"build_id": "build-123",
		"source_log": "`+logPath+`"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestCIDispatchEvidenceRequiresResultLog(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\n{\"build_id\":\"build-123\"}\nhello through named coordinator tunnel\n[build] passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"build_id": "build-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestCIDispatchEvidenceRequiresStreamedBuildOutput(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\npublic url: https://transwarp.example.com\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\"build-123\",\"job_id\":\"echo\",\"request_id\":\"request-123\",\"machine_id\":\"machine-123\",\"public_url\":\"https://transwarp.example.com\"}\n[build] passed\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"public_url": "https://transwarp.example.com",
		"build_id": "build-123",
		"job_id": "echo",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestCIDispatchEvidenceRequiresDiagnoseLog(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("{\"build_id\":\"build-123\"}\nhello through named coordinator tunnel\n[build] passed\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"build_id": "build-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestCIDispatchEvidenceRequiresRunnerContext(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\n{\"build_id\":\"build-123\"}\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"build_id": "build-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	for _, field := range []string{"runner_os", "runner_arch"} {
		if !strings.Contains(check.Detail, field) {
			t.Fatalf("expected %s detail, got %+v", field, check)
		}
	}
}

func TestCIDispatchEvidenceRequiresRunID(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\n{\"build_id\":\"build-123\"}\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_attempt": "2",
		"build_id": "build-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "run_id") {
		t.Fatalf("expected run_id detail, got %+v", check)
	}
}

func TestCIDispatchEvidenceRequiresGitHubRunContext(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\n{\"build_id\":\"build-123\"}\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"build_id": "build-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	for _, field := range []string{"workflow", "job", "repository", "sha", "runner_os", "runner_arch"} {
		if !strings.Contains(check.Detail, field) {
			t.Fatalf("expected %s detail, got %+v", field, check)
		}
	}
}

func TestCIDispatchEvidenceRejectsMalformedGitHubRunContext(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		details []string
	}{
		{name: "run id", field: "run_id", value: "run-1234", details: []string{"run_id"}},
		{name: "run attempt", field: "run_attempt", value: "second", details: []string{"run_attempt"}},
		{name: "repository", field: "repository", value: "https://github.com/charliewilco/transwarp", details: []string{"repository"}},
		{name: "sha", field: "sha", value: "not-a-sha", details: []string{"sha"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\n{\"build_id\":\"build-123\"}\n[result] recorded passed\n"), 0o600)
			if err != nil {
				t.Fatal(err)
			}
			values := map[string]string{
				"run_id":      "1234",
				"run_attempt": "2",
				"repository":  "charliewilco/transwarp",
				"sha":         "0123456789abcdef0123456789abcdef01234567",
			}
			values[test.field] = test.value
			path := filepath.Join(dir, "ci-dispatch.json")
			err = os.WriteFile(path, []byte(`{
				"kind": "transwarp-ci-dispatch-evidence",
				"schema_version": 1,
				"generated_at": "2026-07-26T11:30:00Z",
				"status": "pass",
				"github_actions": true,
				"result_recorded": true,
				"run_id": "`+values["run_id"]+`",
				"run_attempt": "`+values["run_attempt"]+`",
				"workflow": "Release Evidence",
				"job": "release-evidence",
				"repository": "`+values["repository"]+`",
				"sha": "`+values["sha"]+`",
				"runner_os": "macOS",
				"runner_arch": "ARM64",
				"build_id": "build-123",
				"source_log": "named-tunnel-coordinator-smoke.log"
			}`), 0o600)
			if err != nil {
				t.Fatal(err)
			}

			check := ciDispatchEvidenceCheck(path)
			if check.Status != StatusMissing {
				t.Fatalf("expected missing check, got %+v", check)
			}
			for _, detail := range test.details {
				if !strings.Contains(check.Detail, detail) {
					t.Fatalf("expected %s detail, got %+v", detail, check)
				}
			}
		})
	}
}

func TestCIDispatchEvidenceRequiresBuildIDInReceiptAndSourceLog(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "named-tunnel-coordinator-smoke.log"), []byte("diagnosis passed\n{\"build_id\":\"build-actual\"}\n[result] recorded passed\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "ci-dispatch.json")
	err = os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"public_url": "https://transwarp.example.com",
		"build_id": "build-other",
		"job_id": "echo",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"source_log": "named-tunnel-coordinator-smoke.log"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	check := ciDispatchEvidenceCheck(path)
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "source_log") {
		t.Fatalf("expected source_log detail, got %+v", check)
	}
}

func TestEvidenceCorrelationSkippedWithoutBothReceipts(t *testing.T) {
	if check, ok := evidenceCorrelationCheck("", "ci-dispatch.json"); ok {
		t.Fatalf("expected missing named-tunnel receipt to skip correlation, got %+v", check)
	}
	if check, ok := evidenceCorrelationCheck("named-tunnel.json", ""); ok {
		t.Fatalf("expected missing CI-dispatch receipt to skip correlation, got %+v", check)
	}
}

func TestEvidenceCorrelationPassesWhenBuildIDsMatch(t *testing.T) {
	dir := t.TempDir()
	namedPath := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	ciPath := writeCIDispatchEvidenceFixture(t, dir, "ci", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")

	check, ok := evidenceCorrelationCheck(namedPath, ciPath)
	if !ok {
		t.Fatal("expected correlation check")
	}
	if check.Status != StatusPass {
		t.Fatalf("expected passed check, got %+v", check)
	}
}

func TestEvidenceCorrelationRejectsMismatchedPublicURLs(t *testing.T) {
	dir := t.TempDir()
	namedPath := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	ciPath := writeCIDispatchEvidenceFixture(t, dir, "ci", "build-123", "echo", "request-123", "machine-123", "https://other.example.com")

	check, ok := evidenceCorrelationCheck(namedPath, ciPath)
	if !ok {
		t.Fatal("expected correlation check")
	}
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "https://transwarp.example.com") || !strings.Contains(check.Detail, "https://other.example.com") {
		t.Fatalf("expected both public URLs in detail, got %+v", check)
	}
}

func TestEvidenceCorrelationRejectsMismatchedMachineIDs(t *testing.T) {
	dir := t.TempDir()
	namedPath := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-a", "https://transwarp.example.com")
	ciPath := writeCIDispatchEvidenceFixture(t, dir, "ci", "build-123", "echo", "request-123", "machine-b", "https://transwarp.example.com")

	check, ok := evidenceCorrelationCheck(namedPath, ciPath)
	if !ok {
		t.Fatal("expected correlation check")
	}
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "machine-a") || !strings.Contains(check.Detail, "machine-b") {
		t.Fatalf("expected both machine IDs in detail, got %+v", check)
	}
}

func TestEvidenceCorrelationRejectsMismatchedBuildIDs(t *testing.T) {
	dir := t.TempDir()
	namedPath := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-named", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	ciPath := writeCIDispatchEvidenceFixture(t, dir, "ci", "build-ci", "echo", "request-123", "machine-123", "https://transwarp.example.com")

	check, ok := evidenceCorrelationCheck(namedPath, ciPath)
	if !ok {
		t.Fatal("expected correlation check")
	}
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "build-named") || !strings.Contains(check.Detail, "build-ci") {
		t.Fatalf("expected both build IDs in detail, got %+v", check)
	}
}

func TestEvidenceCorrelationRejectsMismatchedJobIDs(t *testing.T) {
	dir := t.TempDir()
	namedPath := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	ciPath := writeCIDispatchEvidenceFixture(t, dir, "ci", "build-123", "release", "request-123", "machine-123", "https://transwarp.example.com")

	check, ok := evidenceCorrelationCheck(namedPath, ciPath)
	if !ok {
		t.Fatal("expected correlation check")
	}
	if check.Status != StatusMissing {
		t.Fatalf("expected missing check, got %+v", check)
	}
	if !strings.Contains(check.Detail, "echo") || !strings.Contains(check.Detail, "release") {
		t.Fatalf("expected both job IDs in detail, got %+v", check)
	}
}

func TestEvidenceCorrelationRejectsIncompleteMatchingReceipts(t *testing.T) {
	dir := t.TempDir()
	namedPath := filepath.Join(dir, "named-tunnel.json")
	ciPath := filepath.Join(dir, "ci-dispatch.json")
	if err := os.WriteFile(namedPath, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"build_id": "build-123",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"public_url": "https://transwarp.example.com"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ciPath, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"build_id": "build-123",
		"request_id": "request-123",
		"machine_id": "machine-123",
		"public_url": "https://transwarp.example.com"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	check, ok := evidenceCorrelationCheck(namedPath, ciPath)
	if !ok {
		t.Fatal("expected correlation check")
	}
	if check.Status != StatusMissing {
		t.Fatalf("expected incomplete receipts to keep correlation missing, got %+v", check)
	}
	if !strings.Contains(check.Detail, "named_tunnel_evidence") {
		t.Fatalf("expected prerequisite detail, got %+v", check)
	}
}

func TestArchiveChecksPassForReleaseArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Transwarp-release.zip")
	writeTestReleaseArchive(t, path, false)

	auditor := auditor{ctx: context.Background(), options: Options{ReleaseArchivePath: path}}
	checks := auditor.archiveChecks()

	for _, id := range []string{"release_archive_present", "release_archive_readable", "release_archive_contents", "release_archive_checksums"} {
		check := findCheck(t, checks, id)
		if check.Status != StatusPass {
			t.Fatalf("expected %s to pass, got %+v", id, check)
		}
	}
}

func TestArchiveChecksFailWhenChecksumMismatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Transwarp-release.zip")
	writeTestReleaseArchive(t, path, true)

	auditor := auditor{ctx: context.Background(), options: Options{ReleaseArchivePath: path}}
	check := findCheck(t, auditor.archiveChecks(), "release_archive_checksums")
	if check.Status != StatusFail {
		t.Fatalf("expected checksum failure, got %+v", check)
	}
	if !strings.Contains(check.Detail, "checksum mismatch") {
		t.Fatalf("expected mismatch detail, got %+v", check)
	}
}

func TestArchiveChecksReportMissingArchive(t *testing.T) {
	auditor := auditor{ctx: context.Background(), options: Options{ReleaseArchivePath: filepath.Join(t.TempDir(), "missing.zip")}}
	check := findCheck(t, auditor.archiveChecks(), "release_archive_present")
	if check.Status != StatusMissing {
		t.Fatalf("expected missing archive, got %+v", check)
	}
}

func TestEvidenceOnlyValidatesOnlyProvidedReceipts(t *testing.T) {
	dir := t.TempDir()
	namedPath, appPath := writeAppBoundNamedTunnelEvidence(t, dir, "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")

	report := EvidenceOnly(Options{
		NamedTunnelEvidence: namedPath,
		AppPath:             appPath,
		Now:                 fixedNow,
	})

	if report.Overall != StatusPass {
		t.Fatalf("expected evidence-only report to pass, got %+v", report)
	}
	if len(report.Checks) != 1 {
		t.Fatalf("expected only provided evidence to be checked, got %+v", report.Checks)
	}
	check := findCheck(t, report.Checks, "named_tunnel_smoke")
	if check.Status != StatusPass {
		t.Fatalf("expected named tunnel evidence to pass, got %+v", check)
	}
}

func TestEvidenceOnlyIncludesCorrelationWhenBothReceiptsAreProvided(t *testing.T) {
	dir := t.TempDir()
	namedPath, appPath := writeAppBoundNamedTunnelEvidence(t, dir, "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	ciPath := writeCIDispatchEvidenceFixture(t, dir, "ci", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")

	report := EvidenceOnly(Options{
		NamedTunnelEvidence: namedPath,
		CIDispatchEvidence:  ciPath,
		AppPath:             appPath,
		Now:                 fixedNow,
	})

	if report.Overall != StatusPass {
		t.Fatalf("expected evidence-only report to pass, got %+v", report)
	}
	for _, id := range []string{"named_tunnel_smoke", "ci_dispatch_smoke", "release_evidence_correlation"} {
		check := findCheck(t, report.Checks, id)
		if check.Status != StatusPass {
			t.Fatalf("expected %s to pass, got %+v", id, check)
		}
	}
}

func TestEvidenceOnlyRejectsPlaceholderReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named-tunnel.json")
	if err := os.WriteFile(path, []byte(`{"kind":"transwarp-named-tunnel-evidence","schema_version":1,"status":"pass"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	report := EvidenceOnly(Options{
		NamedTunnelEvidence: path,
		Now:                 fixedNow,
	})

	if report.Overall != StatusMissing {
		t.Fatalf("expected incomplete evidence-only report, got %+v", report)
	}
	check := findCheck(t, report.Checks, "named_tunnel_smoke")
	if check.Status != StatusMissing {
		t.Fatalf("expected missing named tunnel evidence, got %+v", check)
	}
	if !strings.Contains(check.Detail, "public_url") {
		t.Fatalf("expected missing public_url detail, got %+v", check)
	}
}

func TestWriteCIDispatchEvidenceFromNamedTunnelEvidence(t *testing.T) {
	dir := t.TempDir()
	namedPath, appPath := writeAppBoundNamedTunnelEvidence(t, dir, "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	sourceLogPath := filepath.Join(t.TempDir(), "github-source.log")
	if err := os.WriteFile(sourceLogPath, []byte(`diagnosis passed
{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"echo","request_id":"request-123","machine_id":"machine-123","public_url":"https://transwarp.example.com"}
hello through named coordinator tunnel
[build] passed
[result] recorded passed
[result] request_id request-123
[result] build_id build-123
[result] job_id echo
[result] machine_id machine-123
[result] public_url https://transwarp.example.com
[result] exit_code 0
`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "ci-dispatch.json")

	err := WriteCIDispatchEvidence(CIDispatchEvidenceWriteOptions{
		NamedTunnelEvidencePath: namedPath,
		SourceLogPath:           sourceLogPath,
		SourceLogName:           "ci-source.log",
		OutputPath:              outputPath,
		GitHub:                  validGitHubActionsEvidenceContext(),
		Now:                     fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "ci-source.log")); err != nil {
		t.Fatal(err)
	}
	report := EvidenceOnly(Options{
		NamedTunnelEvidence: namedPath,
		CIDispatchEvidence:  outputPath,
		AppPath:             appPath,
		Now:                 fixedNow,
	})
	if report.Overall != StatusPass {
		t.Fatalf("expected generated CI evidence to pass, got %+v", report)
	}
	for _, id := range []string{"ci_dispatch_smoke", "release_evidence_correlation"} {
		check := findCheck(t, report.Checks, id)
		if check.Status != StatusPass {
			t.Fatalf("expected %s to pass, got %+v", id, check)
		}
	}
}

func TestWriteCIDispatchEvidenceRejectsMismatchedAcceptedBuild(t *testing.T) {
	dir := t.TempDir()
	namedPath := writeNamedTunnelEvidenceFixture(t, dir, "named", "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	sourceLogPath := filepath.Join(dir, "github-source.log")
	if err := os.WriteFile(sourceLogPath, []byte(`diagnosis passed
{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"echo","request_id":"request-123","machine_id":"other-machine","public_url":"https://transwarp.example.com"}
hello through named coordinator tunnel
[result] recorded passed
`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteCIDispatchEvidence(CIDispatchEvidenceWriteOptions{
		NamedTunnelEvidencePath: namedPath,
		SourceLogPath:           sourceLogPath,
		OutputPath:              filepath.Join(dir, "ci-dispatch.json"),
		GitHub:                  validGitHubActionsEvidenceContext(),
		Now:                     fixedNow,
	})
	if err == nil || !strings.Contains(err.Error(), "CI dispatch source log did not include matching accepted runner build metadata") {
		t.Fatalf("expected accepted-build mismatch error, got %v", err)
	}
}

func TestWriteNamedTunnelEvidenceFromSmokeLogs(t *testing.T) {
	dir := t.TempDir()
	appPath, hashes := writeCleanMacBundleFixture(t, filepath.Join(dir, "app"))
	inputs := writeNamedTunnelSmokeInputs(t, dir, "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	outputPath := filepath.Join(dir, "named-tunnel-evidence.json")

	err := WriteNamedTunnelEvidence(NamedTunnelEvidenceWriteOptions{
		OutputPath:                 outputPath,
		AppPath:                    appPath,
		LaunchMode:                 "app",
		PublicURL:                  "https://transwarp.example.com",
		MachineID:                  "machine-123",
		JobID:                      "echo",
		RequestID:                  "request-123",
		DiagnoseLogPath:            inputs.diagnoseLog,
		DispatchLogPath:            inputs.dispatchLog,
		RunnerLogPath:              inputs.runnerLog,
		AppLogPath:                 inputs.appLog,
		AppStderrPath:              inputs.appStderr,
		TargetsRegisteredPath:      inputs.targetsRegistered,
		TargetsAfterDeregisterPath: inputs.targetsAfterDeregister,
		ResultsPath:                inputs.results,
		Now:                        fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	report := EvidenceOnly(Options{
		NamedTunnelEvidence: outputPath,
		AppPath:             appPath,
		Now:                 fixedNow,
	})
	if report.Overall != StatusPass {
		t.Fatalf("expected generated named-tunnel evidence to pass, got %+v", report)
	}
	check := findCheck(t, report.Checks, "named_tunnel_smoke")
	if check.Status != StatusPass {
		t.Fatalf("expected named-tunnel evidence to pass, got %+v", check)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for field, expected := range hashes {
		if !strings.Contains(string(data), `"`+field+`": "`+expected+`"`) {
			t.Fatalf("receipt did not contain %s=%s: %s", field, expected, data)
		}
	}
}

func TestWriteNamedTunnelEvidenceRequiresAppLaunchMode(t *testing.T) {
	dir := t.TempDir()
	inputs := writeNamedTunnelSmokeInputs(t, dir, "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")

	err := WriteNamedTunnelEvidence(NamedTunnelEvidenceWriteOptions{
		OutputPath:                 filepath.Join(dir, "named-tunnel-evidence.json"),
		LaunchMode:                 "runner",
		PublicURL:                  "https://transwarp.example.com",
		MachineID:                  "machine-123",
		JobID:                      "echo",
		RequestID:                  "request-123",
		DiagnoseLogPath:            inputs.diagnoseLog,
		DispatchLogPath:            inputs.dispatchLog,
		RunnerLogPath:              inputs.runnerLog,
		TargetsRegisteredPath:      inputs.targetsRegistered,
		TargetsAfterDeregisterPath: inputs.targetsAfterDeregister,
		ResultsPath:                inputs.results,
		Now:                        fixedNow,
	})
	if err == nil || !strings.Contains(err.Error(), "requires app launch mode") {
		t.Fatalf("expected app launch mode error, got %v", err)
	}
}

func TestNamedTunnelEvidenceRejectsDifferentAppBundle(t *testing.T) {
	dir := t.TempDir()
	appPath, _ := writeCleanMacBundleFixture(t, filepath.Join(dir, "launched"))
	otherAppPath, _ := writeCleanMacBundleFixture(t, filepath.Join(dir, "audited"))
	if err := os.WriteFile(filepath.Join(otherAppPath, "Contents", "Resources", "cloudflared"), []byte("different cloudflared"), 0o755); err != nil {
		t.Fatal(err)
	}
	inputs := writeNamedTunnelSmokeInputs(t, dir, "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	outputPath := filepath.Join(dir, "named-tunnel-evidence.json")

	if err := WriteNamedTunnelEvidence(NamedTunnelEvidenceWriteOptions{
		OutputPath:                 outputPath,
		AppPath:                    appPath,
		LaunchMode:                 "app",
		PublicURL:                  "https://transwarp.example.com",
		MachineID:                  "machine-123",
		JobID:                      "echo",
		RequestID:                  "request-123",
		DiagnoseLogPath:            inputs.diagnoseLog,
		DispatchLogPath:            inputs.dispatchLog,
		RunnerLogPath:              inputs.runnerLog,
		AppLogPath:                 inputs.appLog,
		AppStderrPath:              inputs.appStderr,
		TargetsRegisteredPath:      inputs.targetsRegistered,
		TargetsAfterDeregisterPath: inputs.targetsAfterDeregister,
		ResultsPath:                inputs.results,
		Now:                        fixedNow,
	}); err != nil {
		t.Fatal(err)
	}

	check := namedTunnelEvidenceCheck(outputPath, otherAppPath)
	if check.Status != StatusMissing || !strings.Contains(check.Detail, "cloudflared_sha256") {
		t.Fatalf("expected named-tunnel bundle hash mismatch, got %+v", check)
	}
}

func TestWriteNamedTunnelEvidenceRejectsMismatchedResultBuild(t *testing.T) {
	dir := t.TempDir()
	inputs := writeNamedTunnelSmokeInputs(t, dir, "build-123", "echo", "request-123", "machine-123", "https://transwarp.example.com")
	if err := os.WriteFile(inputs.results, []byte(`[{"request_id":"request-123","build_id":"build-other","job_id":"echo","status":"passed"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteNamedTunnelEvidence(NamedTunnelEvidenceWriteOptions{
		OutputPath:                 filepath.Join(dir, "named-tunnel-evidence.json"),
		LaunchMode:                 "app",
		PublicURL:                  "https://transwarp.example.com",
		MachineID:                  "machine-123",
		JobID:                      "echo",
		RequestID:                  "request-123",
		DiagnoseLogPath:            inputs.diagnoseLog,
		DispatchLogPath:            inputs.dispatchLog,
		RunnerLogPath:              inputs.runnerLog,
		AppLogPath:                 inputs.appLog,
		AppStderrPath:              inputs.appStderr,
		TargetsRegisteredPath:      inputs.targetsRegistered,
		TargetsAfterDeregisterPath: inputs.targetsAfterDeregister,
		ResultsPath:                inputs.results,
		Now:                        fixedNow,
	})
	if err == nil || !strings.Contains(err.Error(), "named tunnel result build_id did not match accepted-build metadata") {
		t.Fatalf("expected mismatched result build error, got %v", err)
	}
}

func writeAppBoundNamedTunnelEvidence(t *testing.T, dir string, buildID string, jobID string, requestID string, machineID string, publicURL string) (string, string) {
	t.Helper()
	appPath, _ := writeCleanMacBundleFixture(t, filepath.Join(dir, "app"))
	sourceDir := filepath.Join(dir, "named-source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inputs := writeNamedTunnelSmokeInputs(t, sourceDir, buildID, jobID, requestID, machineID, publicURL)
	outputPath := filepath.Join(dir, "named-tunnel-evidence.json")
	if err := WriteNamedTunnelEvidence(NamedTunnelEvidenceWriteOptions{
		OutputPath:                 outputPath,
		AppPath:                    appPath,
		LaunchMode:                 "app",
		PublicURL:                  publicURL,
		MachineID:                  machineID,
		JobID:                      jobID,
		RequestID:                  requestID,
		DiagnoseLogPath:            inputs.diagnoseLog,
		DispatchLogPath:            inputs.dispatchLog,
		RunnerLogPath:              inputs.runnerLog,
		AppLogPath:                 inputs.appLog,
		AppStderrPath:              inputs.appStderr,
		TargetsRegisteredPath:      inputs.targetsRegistered,
		TargetsAfterDeregisterPath: inputs.targetsAfterDeregister,
		ResultsPath:                inputs.results,
		Now:                        fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
	return outputPath, appPath
}

func writeNamedTunnelEvidenceFixture(t *testing.T, dir string, name string, buildID string, jobID string, requestID string, machineID string, publicURL string) string {
	t.Helper()
	diagnoseLog := name + "-diagnose.log"
	dispatchLog := name + "-dispatch.log"
	runnerLog := name + "-runner.log"
	resultsJSON := name + "-results.json"
	targetsRegistered := name + "-targets-registered.json"
	targetsAfterDeregister := name + "-targets-after-deregister.json"
	path := filepath.Join(dir, name+"-named-tunnel.json")
	if err := os.WriteFile(filepath.Join(dir, diagnoseLog), []byte("[ok] target public_url="+publicURL+"\n[ok] selected runner health reachable through public_url\n[ok] tunnel mode=named state=running connected=true ready=true\ndiagnosis passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, dispatchLog), []byte("[build] starting Echo Smoke\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\""+buildID+"\",\"job_id\":\""+jobID+"\",\"request_id\":\""+requestID+"\",\"machine_id\":\""+machineID+"\",\"public_url\":\""+publicURL+"\"}\nhello through named coordinator tunnel\n[result] recorded passed\n[result] request_id "+requestID+"\n[result] build_id "+buildID+"\n[result] job_id "+jobID+"\n[result] machine_id "+machineID+"\n[result] public_url "+publicURL+"\n[result] exit_code 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runnerLog), []byte("[info] Started transwarp-runner\n[tunnel] INF Registered tunnel connection\n[tunnel] tunnel ready at "+publicURL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appLog := name + "-app.log"
	appStderr := name + "-app.err"
	if err := os.WriteFile(filepath.Join(dir, appLog), []byte("[info] Started transwarp-runner\n[tunnel] INF Registered tunnel connection\n[tunnel] tunnel ready at "+publicURL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, appStderr), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, targetsRegistered), []byte(`[{"machine_id":"`+machineID+`","public_url":"`+publicURL+`","accepting_builds":true,"jobs":["`+jobID+`"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, targetsAfterDeregister), []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, resultsJSON), []byte(`[{"request_id":"`+requestID+`","build_id":"`+buildID+`","job_id":"`+jobID+`","status":"passed","exit_code":0}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-named-tunnel-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"tunnel_mode": "named",
		"launch_mode": "app",
		"public_url": "`+publicURL+`",
		"coordinator": true,
		"app_launch_passed": true,
		"keychain_migrated": true,
		"target_registered": true,
		"diagnose_passed": true,
		"dispatch_passed": true,
		"result_recorded": true,
		"deregistered": true,
		"build_id": "`+buildID+`",
		"job_id": "`+jobID+`",
		"request_id": "`+requestID+`",
		"exit_code": 0,
		"machine_id": "`+machineID+`",
		"diagnose_log": "`+diagnoseLog+`",
		"dispatch_log": "`+dispatchLog+`",
		"runner_log": "`+runnerLog+`",
		"app_log": "`+appLog+`",
		"app_stderr": "`+appStderr+`",
		"results_json": "`+resultsJSON+`",
		"targets_registered_json": "`+targetsRegistered+`",
		"targets_after_deregister_json": "`+targetsAfterDeregister+`"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type namedTunnelSmokeInputs struct {
	diagnoseLog            string
	dispatchLog            string
	runnerLog              string
	appLog                 string
	appStderr              string
	targetsRegistered      string
	targetsAfterDeregister string
	results                string
}

func writeNamedTunnelSmokeInputs(t *testing.T, dir string, buildID string, jobID string, requestID string, machineID string, publicURL string) namedTunnelSmokeInputs {
	t.Helper()
	inputs := namedTunnelSmokeInputs{
		diagnoseLog:            filepath.Join(dir, "diagnose.log"),
		dispatchLog:            filepath.Join(dir, "dispatch.log"),
		runnerLog:              filepath.Join(dir, "runner.log"),
		appLog:                 filepath.Join(dir, "app.log"),
		appStderr:              filepath.Join(dir, "app.err"),
		targetsRegistered:      filepath.Join(dir, "targets-registered.json"),
		targetsAfterDeregister: filepath.Join(dir, "targets-after-deregister.json"),
		results:                filepath.Join(dir, "results.json"),
	}
	files := map[string]string{
		inputs.diagnoseLog:            "[ok] target public_url=" + publicURL + "\n[ok] selected runner health reachable through public_url\n[ok] tunnel mode=named state=running connected=true ready=true\ndiagnosis passed\n",
		inputs.dispatchLog:            "[build] starting Echo Smoke\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\"" + buildID + "\",\"job_id\":\"" + jobID + "\",\"request_id\":\"" + requestID + "\",\"machine_id\":\"" + machineID + "\",\"public_url\":\"" + publicURL + "\"}\nhello through named coordinator tunnel\n[result] recorded passed\n[result] request_id " + requestID + "\n[result] build_id " + buildID + "\n[result] job_id " + jobID + "\n[result] machine_id " + machineID + "\n[result] public_url " + publicURL + "\n[result] exit_code 0\n",
		inputs.runnerLog:              "[info] Started transwarp-runner\n[tunnel] INF Registered tunnel connection\n[tunnel] tunnel ready at " + publicURL + "\n",
		inputs.appLog:                 "[info] Started transwarp-runner\n[tunnel] INF Registered tunnel connection\n[tunnel] tunnel ready at " + publicURL + "\n",
		inputs.appStderr:              "",
		inputs.targetsRegistered:      `[{"machine_id":"` + machineID + `","public_url":"` + publicURL + `","accepting_builds":true,"jobs":["` + jobID + `"]}]`,
		inputs.targetsAfterDeregister: `[]`,
		inputs.results:                `[{"request_id":"` + requestID + `","build_id":"` + buildID + `","job_id":"` + jobID + `","status":"passed","exit_code":0}]`,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return inputs
}

func validGitHubActionsEvidenceContext() GitHubActionsEvidenceContext {
	return GitHubActionsEvidenceContext{
		GitHubActions: true,
		RunID:         "1234",
		RunAttempt:    "1",
		Workflow:      "Release Evidence",
		Job:           "release-evidence",
		Repository:    "charliewilco/transwarp",
		SHA:           "0123456789abcdef0123456789abcdef01234567",
		RunnerOS:      "macOS",
		RunnerArch:    "ARM64",
		RunnerName:    "transwarp-smoke",
	}
}

func writeCIDispatchEvidenceFixture(t *testing.T, dir string, name string, buildID string, jobID string, requestID string, machineID string, publicURL string) string {
	t.Helper()
	sourceLog := name + "-source.log"
	path := filepath.Join(dir, name+"-ci-dispatch.json")
	if err := os.WriteFile(filepath.Join(dir, sourceLog), []byte("diagnosis passed\npublic url: "+publicURL+"\n{\"kind\":\"coordinator\",\"message\":\"accepted runner build\",\"build_id\":\""+buildID+"\",\"job_id\":\""+jobID+"\",\"request_id\":\""+requestID+"\",\"machine_id\":\""+machineID+"\",\"public_url\":\""+publicURL+"\"}\nhello through named coordinator tunnel\n[build] passed\n[result] recorded passed\n[result] request_id "+requestID+"\n[result] build_id "+buildID+"\n[result] job_id "+jobID+"\n[result] machine_id "+machineID+"\n[result] public_url "+publicURL+"\n[result] exit_code 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
		"kind": "transwarp-ci-dispatch-evidence",
		"schema_version": 1,
		"generated_at": "2026-07-26T11:30:00Z",
		"status": "pass",
		"github_actions": true,
		"result_recorded": true,
		"run_id": "1234",
		"run_attempt": "2",
		"workflow": "Release Evidence",
		"job": "release-evidence",
		"repository": "charliewilco/transwarp",
		"sha": "0123456789abcdef0123456789abcdef01234567",
		"runner_os": "macOS",
		"runner_arch": "ARM64",
		"public_url": "`+publicURL+`",
		"build_id": "`+buildID+`",
		"job_id": "`+jobID+`",
		"request_id": "`+requestID+`",
		"exit_code": 0,
		"machine_id": "`+machineID+`",
		"source_log": "`+sourceLog+`"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func findCheck(t *testing.T, checks []Check, id string) Check {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing check %s in %+v", id, checks)
	return Check{}
}

func plistFixtureRunner(infoPlist string, manifest string, infoValues map[string]string, manifestValues map[string]string) CommandRunner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "/usr/bin/plutil" || len(args) != 4 || args[0] != "-extract" || args[2] != "raw" {
			return nil, os.ErrInvalid
		}
		key := args[1]
		path := args[3]
		values := map[string]string{}
		switch path {
		case infoPlist:
			values = infoValues
		case manifest:
			values = manifestValues
		default:
			return nil, os.ErrNotExist
		}
		value, ok := values[key]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(value), nil
	}
}

func writeTestReleaseArchive(t *testing.T, path string, corruptChecksum bool) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()

	entries := map[string]string{
		"TranswarpRelease/Transwarp.app/Contents/MacOS/Transwarp":                  "app",
		"TranswarpRelease/Transwarp.app/Contents/Resources/transwarp-runner":       "runner",
		"TranswarpRelease/Transwarp.app/Contents/Resources/cloudflared":            "cloudflared",
		"TranswarpRelease/Transwarp.app/Contents/Resources/TranswarpManifest.json": "{}",
		"TranswarpRelease/Validation/clean-mac-validate.sh":                        "#!/bin/sh\n",
		"TranswarpRelease/Validation/validate-clean-mac-status.sh":                 "#!/bin/sh\n",
		"TranswarpRelease/Validation/transwarp-audit":                              "audit",
		"TranswarpRelease/Validation/transwarp-config":                             "config",
		"TranswarpRelease/Validation/README.txt":                                   "readme",
	}
	checksums := []string{}
	for name, contents := range entries {
		writeZipEntry(t, writer, name, contents)
		relativeName := strings.TrimPrefix(name, "TranswarpRelease/")
		checksum := sha256.Sum256([]byte(contents))
		if corruptChecksum && strings.HasSuffix(name, "clean-mac-validate.sh") {
			checksums = append(checksums, strings.Repeat("0", 64)+"  "+relativeName)
		} else {
			checksums = append(checksums, hex.EncodeToString(checksum[:])+"  "+relativeName)
		}
	}
	writeZipEntry(t, writer, "TranswarpRelease/Validation/SHA256SUMS", strings.Join(checksums, "\n")+"\n")
}

func writeZipEntry(t *testing.T, writer *zip.Writer, name string, contents string) {
	t.Helper()
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
}

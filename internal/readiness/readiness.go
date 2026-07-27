package readiness

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charliewilco/transwarp/internal/endpoint"
	"github.com/charliewilco/transwarp/internal/requestmeta"
)

type Options struct {
	AppPath                    string
	ReleaseArchivePath         string
	Strict                     bool
	SelfHostedEvidence         string
	AppLaunchEvidence          string
	NamedTunnelEvidence        string
	CIDispatchEvidence         string
	CleanMacEvidence           string
	ExpectedCloudflaredVersion string
	RunCommand                 CommandRunner
	Now                        func() time.Time
}

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Report struct {
	GeneratedAt        time.Time `json:"generated_at"`
	AppPath            string    `json:"app_path"`
	ReleaseArchivePath string    `json:"release_archive_path"`
	Strict             bool      `json:"strict"`
	Overall            string    `json:"overall"`
	Checks             []Check   `json:"checks"`
}

type Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

const (
	StatusPass    = "pass"
	StatusWarning = "warning"
	StatusFail    = "fail"
	StatusMissing = "missing"
)

func Audit(ctx context.Context, options Options) Report {
	if options.AppPath == "" {
		options.AppPath = filepath.Join(".build", "Transwarp.app")
	}
	if options.ReleaseArchivePath == "" {
		options.ReleaseArchivePath = filepath.Join(".build", "Transwarp-release.zip")
	}
	if options.RunCommand == nil {
		options.RunCommand = runCommand
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	auditor := auditor{ctx: ctx, options: options}
	report := Report{
		GeneratedAt:        options.Now(),
		AppPath:            options.AppPath,
		ReleaseArchivePath: options.ReleaseArchivePath,
		Strict:             options.Strict,
	}
	report.Checks = append(report.Checks, auditor.bundleChecks()...)
	report.Checks = append(report.Checks, auditor.binaryChecks()...)
	report.Checks = append(report.Checks, auditor.signatureChecks()...)
	report.Checks = append(report.Checks, auditor.archiveChecks()...)
	report.Checks = append(report.Checks, auditor.externalEvidenceChecks()...)
	report.Overall = overallStatus(report.Checks, options.Strict)
	return report
}

func EvidenceOnly(options Options) Report {
	if options.AppPath == "" {
		options.AppPath = filepath.Join(".build", "Transwarp.app")
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	report := Report{
		GeneratedAt: options.Now(),
		AppPath:     options.AppPath,
		Strict:      options.Strict,
	}
	if strings.TrimSpace(options.SelfHostedEvidence) != "" {
		report.Checks = append(report.Checks, selfHostedEvidenceCheck(options.SelfHostedEvidence))
	}
	if strings.TrimSpace(options.AppLaunchEvidence) != "" {
		report.Checks = append(report.Checks, appLaunchEvidenceCheck(options.AppLaunchEvidence, options.AppPath))
	}
	if strings.TrimSpace(options.NamedTunnelEvidence) != "" {
		report.Checks = append(report.Checks, namedTunnelEvidenceCheck(options.NamedTunnelEvidence, options.AppPath))
	}
	if strings.TrimSpace(options.CIDispatchEvidence) != "" {
		report.Checks = append(report.Checks, ciDispatchEvidenceCheck(options.CIDispatchEvidence))
	}
	if strings.TrimSpace(options.CleanMacEvidence) != "" {
		report.Checks = append(report.Checks, cleanMacEvidenceCheck(options.CleanMacEvidence, options.AppPath))
	}
	if check, ok := evidenceCorrelationCheck(options.NamedTunnelEvidence, options.CIDispatchEvidence); ok {
		report.Checks = append(report.Checks, check)
	}
	report.Overall = overallStatus(report.Checks, options.Strict)
	return report
}

func ReadReport(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := decodeSingleJSON(data, &report, true); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (report Report) ExitCode() int {
	if report.Overall == StatusPass || report.Overall == StatusWarning {
		return 0
	}
	return 1
}

func (report Report) ExitCodeAllowingIncomplete() int {
	if report.Strict {
		return report.ExitCode()
	}
	for _, check := range report.Checks {
		if check.Status == StatusFail {
			return 1
		}
	}
	return 0
}

func (report Report) SummaryLines() []string {
	counts := map[string]int{
		StatusPass:    0,
		StatusWarning: 0,
		StatusMissing: 0,
		StatusFail:    0,
	}
	for _, check := range report.Checks {
		counts[check.Status]++
	}
	lines := []string{
		fmt.Sprintf(
			"overall=%s checks=%d pass, %d warning, %d missing, %d fail",
			report.Overall,
			counts[StatusPass],
			counts[StatusWarning],
			counts[StatusMissing],
			counts[StatusFail],
		),
	}
	for _, check := range report.Checks {
		if check.Status == StatusPass {
			continue
		}
		line := fmt.Sprintf("%s: %s - %s", check.Status, check.ID, check.Summary)
		if strings.TrimSpace(check.Detail) != "" {
			line += " " + compactSummaryDetail(check.Detail)
		}
		lines = append(lines, line)
	}
	return lines
}

type auditor struct {
	ctx     context.Context
	options Options
}

func (auditor auditor) bundleChecks() []Check {
	app := auditor.options.AppPath
	infoPlist := filepath.Join(app, "Contents", "Info.plist")
	manifest := filepath.Join(app, "Contents", "Resources", "TranswarpManifest.json")
	checks := []Check{}

	checks = append(checks, fileExistsCheck("app_bundle_present", app, "Transwarp.app bundle exists"))
	checks = append(checks, fileExistsCheck("info_plist_present", infoPlist, "Info.plist exists"))
	checks = append(checks, fileExistsCheck("manifest_present", manifest, "TranswarpManifest.json exists"))
	if !fileExists(app) || !fileExists(infoPlist) || !fileExists(manifest) {
		return checks
	}

	bundleID := auditor.plistValue(infoPlist, "CFBundleIdentifier")
	checks = append(checks, exactValueCheck("bundle_identifier", bundleID, "co.charliewil.transwarp", "Bundle identifier is co.charliewil.transwarp"))
	appVersion := auditor.plistValue(infoPlist, "CFBundleShortVersionString")
	checks = append(checks, nonEmptyValueCheck("app_version", appVersion, "App version is present"))
	buildNumber := auditor.plistValue(infoPlist, "CFBundleVersion")
	checks = append(checks, nonEmptyValueCheck("build_number", buildNumber, "Build number is present"))
	minSystem := auditor.plistValue(infoPlist, "LSMinimumSystemVersion")
	checks = append(checks, exactValueCheck("minimum_system", minSystem, "14.0", "Minimum macOS version is modern-only"))

	checks = append(checks, exactValueCheck("manifest_schema", auditor.plistValue(manifest, "schema_version"), "1", "Manifest schema version is current"))
	checks = append(checks, exactValueCheck("manifest_bundle_identifier", auditor.plistValue(manifest, "bundle_identifier"), bundleID, "Manifest bundle identifier matches app"))
	checks = append(checks, exactValueCheck("manifest_app_version", auditor.plistValue(manifest, "app_version"), appVersion, "Manifest app version matches app"))
	checks = append(checks, exactValueCheck("manifest_build_number", auditor.plistValue(manifest, "build_number"), buildNumber, "Manifest build number matches app"))
	checks = append(checks, exactValueCheck("manifest_minimum_system", auditor.plistValue(manifest, "minimum_system_version"), minSystem, "Manifest minimum macOS version matches app"))
	checks = append(checks, exactValueCheck("manifest_architecture", auditor.plistValue(manifest, "architecture"), "arm64", "Manifest architecture is arm64"))

	runnerPath := filepath.Join(app, "Contents", "Resources", "transwarp-runner")
	cloudflaredPath := filepath.Join(app, "Contents", "Resources", "cloudflared")
	checks = append(checks, hashCheck("manifest_runner_hash", auditor.plistValue(manifest, "runner_sha256"), runnerPath, "Manifest runner hash matches bundled helper"))
	checks = append(checks, hashCheck("manifest_cloudflared_hash", auditor.plistValue(manifest, "cloudflared_sha256"), cloudflaredPath, "Manifest cloudflared hash matches bundled connector"))
	cloudflaredVersion := strings.TrimSpace(auditor.plistValue(manifest, "cloudflared_version"))
	if cloudflaredVersion == "" {
		checks = append(checks, Check{ID: "manifest_cloudflared_version", Status: StatusFail, Summary: "Manifest cloudflared version is missing"})
	} else {
		checks = append(checks, Check{ID: "manifest_cloudflared_version", Status: StatusPass, Summary: "Manifest includes cloudflared version", Detail: cloudflaredVersion})
	}
	expectedCloudflaredVersion := strings.TrimSpace(auditor.options.ExpectedCloudflaredVersion)
	if expectedCloudflaredVersion == "" {
		expectedCloudflaredVersion = strings.TrimSpace(auditor.plistValue(manifest, "expected_cloudflared_version"))
	}
	checks = append(checks, cloudflaredVersionPolicyCheck(cloudflaredVersion, expectedCloudflaredVersion))

	return checks
}

func (auditor auditor) binaryChecks() []Check {
	app := auditor.options.AppPath
	paths := map[string]string{
		"app_executable_arm64": filepath.Join(app, "Contents", "MacOS", "Transwarp"),
		"runner_arm64":         filepath.Join(app, "Contents", "Resources", "transwarp-runner"),
		"cloudflared_arm64":    filepath.Join(app, "Contents", "Resources", "cloudflared"),
	}
	checks := []Check{}
	for id, path := range paths {
		checks = append(checks, auditor.arm64Check(id, path))
	}
	return checks
}

func (auditor auditor) signatureChecks() []Check {
	app := auditor.options.AppPath
	appExecutable := filepath.Join(app, "Contents", "MacOS", "Transwarp")
	runnerPath := filepath.Join(app, "Contents", "Resources", "transwarp-runner")
	cloudflaredPath := filepath.Join(app, "Contents", "Resources", "cloudflared")

	checks := []Check{
		auditor.codesignVerifyCheck("app_codesign_deep", app, true),
		auditor.codesignVerifyCheck("app_executable_codesign", appExecutable, false),
		auditor.codesignVerifyCheck("runner_codesign", runnerPath, false),
		auditor.codesignVerifyCheck("cloudflared_codesign", cloudflaredPath, false),
	}
	for _, item := range []struct {
		id   string
		path string
	}{
		{"app_distribution_signature", app},
		{"app_executable_distribution_signature", appExecutable},
		{"runner_distribution_signature", runnerPath},
		{"cloudflared_distribution_signature", cloudflaredPath},
	} {
		checks = append(checks, auditor.distributionSignatureCheck(item.id, item.path))
	}
	checks = append(checks, auditor.notarizationStapleCheck(app))
	checks = append(checks, auditor.gatekeeperCheck(app))
	return checks
}

func (auditor auditor) externalEvidenceChecks() []Check {
	checks := []Check{
		selfHostedEvidenceCheck(auditor.options.SelfHostedEvidence),
		appLaunchEvidenceCheck(auditor.options.AppLaunchEvidence, auditor.options.AppPath),
		namedTunnelEvidenceCheck(auditor.options.NamedTunnelEvidence, auditor.options.AppPath),
		ciDispatchEvidenceCheck(auditor.options.CIDispatchEvidence),
		cleanMacEvidenceCheck(auditor.options.CleanMacEvidence, auditor.options.AppPath),
	}
	if check, ok := evidenceCorrelationCheck(auditor.options.NamedTunnelEvidence, auditor.options.CIDispatchEvidence); ok {
		checks = append(checks, check)
	}
	return checks
}

func (auditor auditor) archiveChecks() []Check {
	archivePath := auditor.options.ReleaseArchivePath
	checks := []Check{fileExistsCheck("release_archive_present", archivePath, "Release archive exists")}
	if !fileExists(archivePath) {
		return checks
	}

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return append(checks, Check{ID: "release_archive_readable", Status: StatusFail, Summary: "Release archive is not readable", Detail: err.Error()})
	}
	defer reader.Close()

	files := map[string]*zip.File{}
	for _, file := range reader.File {
		files[file.Name] = file
	}
	checks = append(checks, Check{ID: "release_archive_readable", Status: StatusPass, Summary: "Release archive is readable", Detail: archivePath})

	required := []string{
		"TranswarpRelease/Transwarp.app/Contents/MacOS/Transwarp",
		"TranswarpRelease/Transwarp.app/Contents/Resources/transwarp-runner",
		"TranswarpRelease/Transwarp.app/Contents/Resources/cloudflared",
		"TranswarpRelease/Transwarp.app/Contents/Resources/TranswarpManifest.json",
		"TranswarpRelease/Validation/clean-mac-validate.sh",
		"TranswarpRelease/Validation/validate-clean-mac-status.sh",
		"TranswarpRelease/Validation/transwarp-audit",
		"TranswarpRelease/Validation/transwarp-config",
		"TranswarpRelease/Validation/README.txt",
		"TranswarpRelease/Validation/SHA256SUMS",
	}
	missing := []string{}
	for _, name := range required {
		if files[name] == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		checks = append(checks, Check{ID: "release_archive_contents", Status: StatusFail, Summary: "Release archive is missing required files", Detail: strings.Join(missing, ", ")})
		return checks
	}
	checks = append(checks, Check{ID: "release_archive_contents", Status: StatusPass, Summary: "Release archive includes app and validation files"})
	checks = append(checks, checksumArchiveCheck(files))
	return checks
}

func fileExistsCheck(id string, path string, summary string) Check {
	if fileExists(path) {
		return Check{ID: id, Status: StatusPass, Summary: summary, Detail: path}
	}
	return Check{ID: id, Status: StatusMissing, Summary: summary + " is missing", Detail: path}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func exactValueCheck(id string, actual string, expected string, summary string) Check {
	if actual == expected {
		return Check{ID: id, Status: StatusPass, Summary: summary, Detail: actual}
	}
	return Check{ID: id, Status: StatusFail, Summary: summary + " failed", Detail: fmt.Sprintf("expected %q, got %q", expected, actual)}
}

func cloudflaredVersionPolicyCheck(actual string, expected string) Check {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return Check{
			ID:      "cloudflared_version_policy",
			Status:  StatusWarning,
			Summary: "Expected cloudflared version is not configured",
			Detail:  "set TRANSWARP_EXPECTED_CLOUDFLARED_VERSION or manifest expected_cloudflared_version",
		}
	}
	if actual == expected {
		return Check{ID: "cloudflared_version_policy", Status: StatusPass, Summary: "Bundled cloudflared version matches release policy", Detail: actual}
	}
	return Check{
		ID:      "cloudflared_version_policy",
		Status:  StatusFail,
		Summary: "Bundled cloudflared version does not match release policy",
		Detail:  fmt.Sprintf("expected %q, got %q", expected, actual),
	}
}

func nonEmptyValueCheck(id string, actual string, summary string) Check {
	if strings.TrimSpace(actual) != "" {
		return Check{ID: id, Status: StatusPass, Summary: summary, Detail: actual}
	}
	return Check{ID: id, Status: StatusFail, Summary: summary + " failed", Detail: "value is empty"}
}

func hashCheck(id string, expected string, path string, summary string) Check {
	actual, err := sha256File(path)
	if err != nil {
		return Check{ID: id, Status: StatusFail, Summary: summary + " failed", Detail: err.Error()}
	}
	if expected == actual {
		return Check{ID: id, Status: StatusPass, Summary: summary, Detail: actual}
	}
	return Check{ID: id, Status: StatusFail, Summary: summary + " failed", Detail: fmt.Sprintf("expected %s, got %s", expected, actual)}
}

func checksumArchiveCheck(files map[string]*zip.File) Check {
	const checksumPath = "TranswarpRelease/Validation/SHA256SUMS"
	checksumFile := files[checksumPath]
	data, err := readZipFile(checksumFile)
	if err != nil {
		return Check{ID: "release_archive_checksums", Status: StatusFail, Summary: "Release archive checksums are unreadable", Detail: err.Error()}
	}

	failures := []string{}
	checked := 0
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			failures = append(failures, "malformed checksum line")
			continue
		}
		expected := fields[0]
		name := "TranswarpRelease/" + strings.TrimPrefix(fields[1], "./")
		file := files[name]
		if file == nil {
			failures = append(failures, name+" missing")
			continue
		}
		actual, err := sha256ZipFile(file)
		if err != nil {
			failures = append(failures, name+" unreadable: "+err.Error())
			continue
		}
		if actual != expected {
			failures = append(failures, name+" checksum mismatch")
			continue
		}
		checked++
	}
	if len(failures) > 0 {
		return Check{ID: "release_archive_checksums", Status: StatusFail, Summary: "Release archive checksums failed", Detail: strings.Join(failures, ", ")}
	}
	if checked == 0 {
		return Check{ID: "release_archive_checksums", Status: StatusFail, Summary: "Release archive checksums are empty"}
	}
	return Check{ID: "release_archive_checksums", Status: StatusPass, Summary: "Release archive checksums match", Detail: fmt.Sprintf("%d files checked", checked)}
}

func sha256ZipFile(file *zip.File) (string, error) {
	data, err := readZipFile(file)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	if file == nil {
		return nil, errors.New("zip entry is missing")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func (auditor auditor) arm64Check(id string, path string) Check {
	output, err := auditor.options.RunCommand(auditor.ctx, "/usr/bin/file", path)
	if err != nil {
		return Check{ID: id, Status: StatusFail, Summary: "Binary architecture check failed", Detail: commandDetail(output, err)}
	}
	detail := strings.TrimSpace(string(output))
	if strings.Contains(detail, "Mach-O 64-bit executable arm64") && !strings.Contains(detail, "x86_64") {
		return Check{ID: id, Status: StatusPass, Summary: "Binary is thin arm64", Detail: detail}
	}
	return Check{ID: id, Status: StatusFail, Summary: "Binary is not thin arm64", Detail: detail}
}

func (auditor auditor) codesignVerifyCheck(id string, path string, deep bool) Check {
	args := []string{"--verify"}
	if deep {
		args = append(args, "--deep")
	}
	args = append(args, "--strict", "--verbose=2", path)
	output, err := auditor.options.RunCommand(auditor.ctx, "/usr/bin/codesign", args...)
	if err != nil {
		return Check{ID: id, Status: StatusFail, Summary: "Strict codesign verification failed", Detail: commandDetail(output, err)}
	}
	return Check{ID: id, Status: StatusPass, Summary: "Strict codesign verification passed", Detail: path}
}

func (auditor auditor) distributionSignatureCheck(id string, path string) Check {
	output, err := auditor.options.RunCommand(auditor.ctx, "/usr/bin/codesign", "-dvvv", path)
	detail := commandDetail(output, err)
	if err != nil && strings.TrimSpace(detail) == "" {
		return Check{ID: id, Status: StatusFail, Summary: "Distribution signature inspection failed", Detail: err.Error()}
	}

	issues := []string{}
	if strings.Contains(detail, "Signature=adhoc") {
		issues = append(issues, "ad-hoc signature")
	}
	if !strings.Contains(detail, "Runtime Version=") {
		issues = append(issues, "missing hardened runtime")
	}
	if strings.Contains(detail, "TeamIdentifier=not set") {
		issues = append(issues, "missing Developer ID team identifier")
	}
	if len(issues) == 0 {
		return Check{ID: id, Status: StatusPass, Summary: "Developer ID signature and hardened runtime are present", Detail: compactDetail(detail)}
	}
	status := StatusWarning
	if auditor.options.Strict {
		status = StatusFail
	}
	return Check{ID: id, Status: status, Summary: "Distribution signature is not release-ready", Detail: strings.Join(issues, ", ")}
}

func (auditor auditor) gatekeeperCheck(app string) Check {
	output, err := auditor.options.RunCommand(auditor.ctx, "/usr/sbin/spctl", "-a", "-vv", app)
	detail := commandDetail(output, err)
	if strings.Contains(detail, "accepted") {
		return Check{ID: "gatekeeper", Status: StatusPass, Summary: "Gatekeeper accepts the app", Detail: compactDetail(detail)}
	}
	status := StatusWarning
	if auditor.options.Strict {
		status = StatusFail
	}
	return Check{ID: "gatekeeper", Status: status, Summary: "Gatekeeper acceptance is not proven", Detail: compactDetail(detail)}
}

func (auditor auditor) notarizationStapleCheck(app string) Check {
	output, err := auditor.options.RunCommand(auditor.ctx, "/usr/bin/xcrun", "stapler", "validate", app)
	detail := commandDetail(output, err)
	if strings.Contains(detail, "The validate action worked") {
		return Check{ID: "notarization_staple", Status: StatusPass, Summary: "Stapled notarization ticket validates", Detail: compactDetail(detail)}
	}
	status := StatusWarning
	if auditor.options.Strict {
		status = StatusFail
	}
	return Check{ID: "notarization_staple", Status: status, Summary: "Stapled notarization ticket is not proven", Detail: compactDetail(detail)}
}

func selfHostedEvidenceCheck(path string) Check {
	return jsonEvidenceCheck("self_hosted_mac_readiness", path, "Self-hosted Mac readiness evidence", func(receiptPath string, receipt map[string]any) []string {
		missing := []string{}
		missing = append(missing, requireString(receipt, "kind", "transwarp-self-hosted-mac-readiness")...)
		missing = append(missing, requireSchemaVersion(receipt)...)
		missing = append(missing, requireGeneratedAt(receipt)...)
		missing = append(missing, requireString(receipt, "status", "pass")...)
		missing = append(missing, requireString(receipt, "architecture", "arm64")...)
		missing = append(missing, requireNonEmptyString(receipt, "macos")...)
		missing = append(missing, requireNonEmptyString(receipt, "developer_dir")...)
		missing = append(missing, requireNonEmptyString(receipt, "xcode")...)
		missing = append(missing, requireNonEmptyString(receipt, "source_log")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "source_log", "self-hosted Mac readiness passed", "architecture=arm64", "xcode=")...)
		return missing
	})
}

func appLaunchEvidenceCheck(path string, appPath ...string) Check {
	return jsonEvidenceCheck("app_launch_smoke", path, "Packaged app launch smoke evidence", func(receiptPath string, receipt map[string]any) []string {
		missing := []string{}
		missing = append(missing, requireString(receipt, "kind", "transwarp-app-launch-evidence")...)
		missing = append(missing, requireSchemaVersion(receipt)...)
		missing = append(missing, requireGeneratedAt(receipt)...)
		missing = append(missing, requireString(receipt, "status", "pass")...)
		missing = append(missing, requireMachineID(receipt, "machine_id")...)
		missing = append(missing, requireJobID(receipt, "job_id")...)
		missing = append(missing, requireRequestID(receipt, "request_id")...)
		missing = append(missing, requireBuildID(receipt, "build_id")...)
		if len(appPath) > 0 && strings.TrimSpace(appPath[0]) != "" {
			missing = append(missing, requireAppLaunchBundleHashes(receipt, appPath[0])...)
		}
		missing = append(missing, requireBool(receipt, "keychain_migrated", true)...)
		missing = append(missing, requireBool(receipt, "app_launch_passed", true)...)
		missing = append(missing, requireBool(receipt, "helper_status_authenticated", true)...)
		missing = append(missing, requireBool(receipt, "build_started", true)...)
		missing = append(missing, requireBool(receipt, "log_stream_passed", true)...)
		missing = append(missing, requireBool(receipt, "build_status_passed", true)...)
		missing = append(missing, requireBool(receipt, "recent_build_recorded", true)...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "build_log", `"message":"Xcode `, `"message":"passed"`)...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "build_log", "build_id")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "build_status_json", `"status":"passed"`)...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "build_status_json", "build_id")...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "build_status_json", "request_id")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "status_json", `"status":"passed"`)...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "status_json", "request_id")...)
		missing = append(missing, requireEvidenceSiblingFile(receiptPath, receipt, "app_log")...)
		missing = append(missing, requireEvidenceSiblingFile(receiptPath, receipt, "app_stderr")...)
		missing = append(missing, requireAppLaunchTunnelEvidence(receiptPath, receipt)...)
		return missing
	})
}

func namedTunnelEvidenceCheck(path string, appPath ...string) Check {
	return jsonEvidenceCheck("named_tunnel_smoke", path, "Named Cloudflare Tunnel smoke evidence", func(receiptPath string, receipt map[string]any) []string {
		missing := []string{}
		missing = append(missing, requireString(receipt, "kind", "transwarp-named-tunnel-evidence")...)
		missing = append(missing, requireSchemaVersion(receipt)...)
		missing = append(missing, requireGeneratedAt(receipt)...)
		missing = append(missing, requireString(receipt, "status", "pass")...)
		missing = append(missing, requireString(receipt, "tunnel_mode", "named")...)
		missing = append(missing, requireString(receipt, "launch_mode", "app")...)
		if len(appPath) > 0 && strings.TrimSpace(appPath[0]) != "" {
			missing = append(missing, requireAppLaunchBundleHashes(receipt, appPath[0])...)
		}
		missing = append(missing, requireHTTPSBaseURL(receipt, "public_url")...)
		missing = append(missing, requireBool(receipt, "coordinator", true)...)
		missing = append(missing, requireBool(receipt, "app_launch_passed", true)...)
		missing = append(missing, requireBool(receipt, "keychain_migrated", true)...)
		missing = append(missing, requireBool(receipt, "target_registered", true)...)
		missing = append(missing, requireBool(receipt, "diagnose_passed", true)...)
		missing = append(missing, requireBool(receipt, "dispatch_passed", true)...)
		missing = append(missing, requireBool(receipt, "result_recorded", true)...)
		missing = append(missing, requireBool(receipt, "deregistered", true)...)
		missing = append(missing, requireBuildID(receipt, "build_id")...)
		missing = append(missing, requireJobID(receipt, "job_id")...)
		missing = append(missing, requireRequestID(receipt, "request_id")...)
		missing = append(missing, requireMachineID(receipt, "machine_id")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "diagnose_log", "diagnosis passed", "selected runner health reachable through public_url", "tunnel mode=named", "state=running", "connected=true", "ready=true")...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "diagnose_log", "public_url")...)
		missing = append(missing, requireNamedTunnelRunnerLog(receiptPath, receipt)...)
		missing = append(missing, requireEvidenceSiblingFile(receiptPath, receipt, "app_log")...)
		missing = append(missing, requireEvidenceSiblingFile(receiptPath, receipt, "app_stderr")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "dispatch_log", "hello through named coordinator tunnel", "[result] recorded passed")...)
		missing = append(missing, requireResultMarkers(receiptPath, receipt, "dispatch_log", "build_id", "job_id", "request_id", "machine_id", "public_url")...)
		missing = append(missing, requireAcceptedBuildMetadata(receiptPath, receipt, "dispatch_log")...)
		missing = append(missing, requireNamedTunnelResults(receiptPath, receipt)...)
		missing = append(missing, requireNamedTunnelTargetEvidence(receiptPath, receipt)...)
		return missing
	})
}

func ciDispatchEvidenceCheck(path string) Check {
	return jsonEvidenceCheck("ci_dispatch_smoke", path, "CI dispatch evidence", func(receiptPath string, receipt map[string]any) []string {
		missing := []string{}
		missing = append(missing, requireString(receipt, "kind", "transwarp-ci-dispatch-evidence")...)
		missing = append(missing, requireSchemaVersion(receipt)...)
		missing = append(missing, requireGeneratedAt(receipt)...)
		missing = append(missing, requireString(receipt, "status", "pass")...)
		missing = append(missing, requireBool(receipt, "github_actions", true)...)
		missing = append(missing, requireBool(receipt, "result_recorded", true)...)
		missing = append(missing, requireUnsignedDecimalString(receipt, "run_id")...)
		missing = append(missing, requireUnsignedDecimalString(receipt, "run_attempt")...)
		missing = append(missing, requireNonEmptyString(receipt, "workflow")...)
		missing = append(missing, requireNonEmptyString(receipt, "job")...)
		missing = append(missing, requireGitHubRepository(receipt, "repository")...)
		missing = append(missing, requireGitSHA(receipt, "sha")...)
		missing = append(missing, requireString(receipt, "runner_os", "macOS")...)
		missing = append(missing, requireString(receipt, "runner_arch", "ARM64")...)
		missing = append(missing, requireHTTPSBaseURL(receipt, "public_url")...)
		missing = append(missing, requireBuildID(receipt, "build_id")...)
		missing = append(missing, requireJobID(receipt, "job_id")...)
		missing = append(missing, requireRequestID(receipt, "request_id")...)
		missing = append(missing, requireMachineID(receipt, "machine_id")...)
		missing = append(missing, requireNonEmptyString(receipt, "source_log")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "source_log", "diagnosis passed", "hello through named coordinator tunnel", "[result] recorded passed")...)
		missing = append(missing, requireResultMarkers(receiptPath, receipt, "source_log", "build_id", "job_id", "request_id", "machine_id", "public_url")...)
		missing = append(missing, requireAcceptedBuildMetadata(receiptPath, receipt, "source_log")...)
		return missing
	})
}

func evidenceCorrelationCheck(namedTunnelPath string, ciDispatchPath string) (Check, bool) {
	if strings.TrimSpace(namedTunnelPath) == "" || strings.TrimSpace(ciDispatchPath) == "" {
		return Check{}, false
	}
	if check := namedTunnelEvidenceCheck(namedTunnelPath); check.Status != StatusPass {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  evidenceCorrelationPrerequisiteDetail("named_tunnel_evidence", check),
		}, true
	}
	if check := ciDispatchEvidenceCheck(ciDispatchPath); check.Status != StatusPass {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  evidenceCorrelationPrerequisiteDetail("ci_dispatch_evidence", check),
		}, true
	}
	namedBuildID, err := evidenceStringField(namedTunnelPath, "transwarp-named-tunnel-evidence", "build_id")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "named_tunnel_evidence " + err.Error(),
		}, true
	}
	ciBuildID, err := evidenceStringField(ciDispatchPath, "transwarp-ci-dispatch-evidence", "build_id")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "ci_dispatch_evidence " + err.Error(),
		}, true
	}
	if namedBuildID != ciBuildID {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence receipts do not describe the same runner build",
			Detail:  fmt.Sprintf("named_tunnel build_id=%q ci_dispatch build_id=%q", namedBuildID, ciBuildID),
		}, true
	}
	namedJobID, err := evidenceStringField(namedTunnelPath, "transwarp-named-tunnel-evidence", "job_id")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "named_tunnel_evidence " + err.Error(),
		}, true
	}
	ciJobID, err := evidenceStringField(ciDispatchPath, "transwarp-ci-dispatch-evidence", "job_id")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "ci_dispatch_evidence " + err.Error(),
		}, true
	}
	if namedJobID != ciJobID {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence receipts do not describe the same CI job",
			Detail:  fmt.Sprintf("named_tunnel job_id=%q ci_dispatch job_id=%q", namedJobID, ciJobID),
		}, true
	}
	namedRequestID, err := evidenceStringField(namedTunnelPath, "transwarp-named-tunnel-evidence", "request_id")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "named_tunnel_evidence " + err.Error(),
		}, true
	}
	ciRequestID, err := evidenceStringField(ciDispatchPath, "transwarp-ci-dispatch-evidence", "request_id")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "ci_dispatch_evidence " + err.Error(),
		}, true
	}
	if namedRequestID != ciRequestID {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence receipts do not describe the same CI request",
			Detail:  fmt.Sprintf("named_tunnel request_id=%q ci_dispatch request_id=%q", namedRequestID, ciRequestID),
		}, true
	}
	namedPublicURL, err := evidenceStringField(namedTunnelPath, "transwarp-named-tunnel-evidence", "public_url")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "named_tunnel_evidence " + err.Error(),
		}, true
	}
	ciPublicURL, err := evidenceStringField(ciDispatchPath, "transwarp-ci-dispatch-evidence", "public_url")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "ci_dispatch_evidence " + err.Error(),
		}, true
	}
	if namedPublicURL != ciPublicURL {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence receipts do not describe the same tunnel URL",
			Detail:  fmt.Sprintf("named_tunnel public_url=%q ci_dispatch public_url=%q", namedPublicURL, ciPublicURL),
		}, true
	}
	namedMachineID, err := evidenceStringField(namedTunnelPath, "transwarp-named-tunnel-evidence", "machine_id")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "named_tunnel_evidence " + err.Error(),
		}, true
	}
	ciMachineID, err := evidenceStringField(ciDispatchPath, "transwarp-ci-dispatch-evidence", "machine_id")
	if err != nil {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence correlation incomplete",
			Detail:  "ci_dispatch_evidence " + err.Error(),
		}, true
	}
	if namedMachineID != ciMachineID {
		return Check{
			ID:      "release_evidence_correlation",
			Status:  StatusMissing,
			Summary: "Release evidence receipts do not describe the same machine",
			Detail:  fmt.Sprintf("named_tunnel machine_id=%q ci_dispatch machine_id=%q", namedMachineID, ciMachineID),
		}, true
	}
	return Check{
		ID:      "release_evidence_correlation",
		Status:  StatusPass,
		Summary: "Named tunnel and CI dispatch evidence describe the same CI job, request, runner build, machine, and tunnel URL",
		Detail:  fmt.Sprintf("job_id=%s request_id=%s build_id=%s machine_id=%s public_url=%s", namedJobID, namedRequestID, namedBuildID, namedMachineID, namedPublicURL),
	}, true
}

func evidenceCorrelationPrerequisiteDetail(label string, check Check) string {
	detail := strings.TrimSpace(check.Detail)
	if detail == "" {
		return label + " " + check.Summary
	}
	return label + " " + check.Summary + ": " + compactSummaryDetail(detail)
}

func evidenceStringField(path string, expectedKind string, field string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var receipt map[string]any
	if err := decodeSingleJSON(data, &receipt, false); err != nil {
		return "", err
	}
	kind, ok := receipt["kind"].(string)
	if !ok || kind != expectedKind {
		return "", fmt.Errorf("kind must be %q", expectedKind)
	}
	value, ok := receipt[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	return strings.TrimSpace(value), nil
}

func cleanMacEvidenceCheck(path string, appPath string) Check {
	return jsonEvidenceCheck("clean_mac_validation", path, "Clean-Mac validation evidence", func(receiptPath string, receipt map[string]any) []string {
		missing := []string{}
		missing = append(missing, requireString(receipt, "kind", "transwarp-clean-mac-evidence")...)
		missing = append(missing, requireSchemaVersion(receipt)...)
		missing = append(missing, requireGeneratedAt(receipt)...)
		missing = append(missing, requireString(receipt, "status", "pass")...)
		missing = append(missing, requireAppBundleName(receipt, "app")...)
		missing = append(missing, requireString(receipt, "architecture", "arm64")...)
		missing = append(missing, requireBundleHash(receipt, "app_executable_sha256", filepath.Join(appPath, "Contents", "MacOS", "Transwarp"))...)
		missing = append(missing, requireBundleHash(receipt, "runner_sha256", filepath.Join(appPath, "Contents", "Resources", "transwarp-runner"))...)
		missing = append(missing, requireBundleHash(receipt, "cloudflared_sha256", filepath.Join(appPath, "Contents", "Resources", "cloudflared"))...)
		missing = append(missing, requireBundleHash(receipt, "manifest_sha256", filepath.Join(appPath, "Contents", "Resources", "TranswarpManifest.json"))...)
		missing = append(missing, requireMachineID(receipt, "machine_id")...)
		missing = append(missing, requireJobID(receipt, "job_id")...)
		missing = append(missing, requireRequestID(receipt, "request_id")...)
		missing = append(missing, requireBuildID(receipt, "build_id")...)
		missing = append(missing, requireBool(receipt, "strict_codesign", true)...)
		missing = append(missing, requireBool(receipt, "notarization_staple", true)...)
		missing = append(missing, requireBool(receipt, "gatekeeper_accepted", true)...)
		missing = append(missing, requireBool(receipt, "authenticated_status", true)...)
		missing = append(missing, requireBool(receipt, "machine_id_matched", true)...)
		missing = append(missing, requireBool(receipt, "job_advertised", true)...)
		missing = append(missing, requireBool(receipt, "first_launch_passed", true)...)
		missing = append(missing, requireBool(receipt, "build_started", true)...)
		missing = append(missing, requireBool(receipt, "log_stream_passed", true)...)
		missing = append(missing, requireBool(receipt, "build_status_passed", true)...)
		missing = append(missing, requireBool(receipt, "recent_build_recorded", true)...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "codesign_log", "valid on disk", "satisfies its Designated Requirement")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "stapler_log", "The validate action worked")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "gatekeeper_log", "accepted")...)
		missing = append(missing, requireCleanMacStatus(receiptPath, receipt)...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "build_log", `"message":"passed"`)...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "build_log", "build_id")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "build_status_json", `"status":"passed"`)...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "build_status_json", "build_id")...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "build_status_json", "request_id")...)
		missing = append(missing, requireLogContains(receiptPath, receipt, "status_json", `"status":"passed"`)...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "status_json", "build_id")...)
		missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "status_json", "request_id")...)
		missing = append(missing, requireEvidenceSiblingFile(receiptPath, receipt, "app_log")...)
		missing = append(missing, requireEvidenceSiblingFile(receiptPath, receipt, "app_stderr")...)
		return missing
	})
}

func jsonEvidenceCheck(id string, path string, summary string, validate func(string, map[string]any) []string) Check {
	if strings.TrimSpace(path) == "" {
		return Check{ID: id, Status: StatusMissing, Summary: summary + " missing", Detail: "provide an evidence JSON path"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Check{ID: id, Status: StatusMissing, Summary: summary + " unreadable", Detail: err.Error()}
	}
	var receipt map[string]any
	if err := decodeSingleJSON(data, &receipt, false); err != nil {
		return Check{ID: id, Status: StatusMissing, Summary: summary + " invalid", Detail: err.Error()}
	}
	missing := validate(path, receipt)
	if len(missing) == 0 {
		return Check{ID: id, Status: StatusPass, Summary: summary, Detail: path}
	}
	return Check{ID: id, Status: StatusMissing, Summary: summary + " incomplete", Detail: "invalid " + strings.Join(missing, ", ")}
}

func requireString(receipt map[string]any, field string, expected string) []string {
	value, ok := receipt[field].(string)
	if !ok || value != expected {
		return []string{field}
	}
	return nil
}

func requireSchemaVersion(receipt map[string]any) []string {
	value, ok := receipt["schema_version"].(float64)
	if !ok || value != 1 {
		return []string{"schema_version"}
	}
	return nil
}

func requireGeneratedAt(receipt map[string]any) []string {
	value, ok := receipt["generated_at"].(string)
	if !ok {
		return []string{"generated_at"}
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return []string{"generated_at"}
	}
	return nil
}

func requireNonEmptyString(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return []string{field}
	}
	return nil
}

func requireAppBundleName(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	trimmed := strings.TrimSpace(value)
	cleaned := filepath.Clean(trimmed)
	if filepath.IsAbs(trimmed) || cleaned != filepath.Base(cleaned) || cleaned == "." || cleaned == ".." || filepath.Ext(cleaned) != ".app" {
		return []string{field}
	}
	return nil
}

func requireBuildID(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	if err := requestmeta.ValidateBuildID(value); err != nil {
		return []string{field}
	}
	return nil
}

func requireMachineID(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	if err := requestmeta.ValidateMachineID(value); err != nil {
		return []string{field}
	}
	return nil
}

func requireJobID(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	if err := requestmeta.ValidateJobID(value); err != nil {
		return []string{field}
	}
	return nil
}

func requireRequestID(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	if err := requestmeta.ValidateRequestID(value); err != nil {
		return []string{field}
	}
	return nil
}

func requireUnsignedDecimalString(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{field}
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return []string{field}
		}
	}
	return nil
}

func requireGitHubRepository(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return []string{field}
	}
	for _, part := range parts {
		for _, character := range part {
			if (character >= 'A' && character <= 'Z') ||
				(character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') ||
				character == '.' ||
				character == '_' ||
				character == '-' {
				continue
			}
			return []string{field}
		}
	}
	return nil
}

func requireGitSHA(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	value = strings.TrimSpace(value)
	if len(value) != 40 {
		return []string{field}
	}
	for _, character := range value {
		if (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F') {
			continue
		}
		return []string{field}
	}
	return nil
}

func requireBool(receipt map[string]any, field string, expected bool) []string {
	value, ok := receipt[field].(bool)
	if !ok || value != expected {
		return []string{field}
	}
	return nil
}

func requireHTTPSBaseURL(receipt map[string]any, field string) []string {
	value, ok := receipt[field].(string)
	if !ok {
		return []string{field}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return []string{field}
	}
	if err := endpoint.ValidateBaseURL(value, field); err != nil {
		return []string{field}
	}
	return nil
}

func requireLogContains(receiptPath string, receipt map[string]any, field string, markers ...string) []string {
	logPath, ok := evidenceSiblingPath(receiptPath, receipt, field)
	if !ok {
		return []string{field}
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return []string{field}
	}
	content := string(data)
	for _, marker := range markers {
		if !strings.Contains(content, marker) {
			return []string{field}
		}
	}
	return nil
}

func requireLogContainsReceiptValue(receiptPath string, receipt map[string]any, logField string, valueField string) []string {
	value, ok := receipt[valueField].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return []string{valueField}
	}
	return requireLogContains(receiptPath, receipt, logField, value)
}

func requireResultMarkers(receiptPath string, receipt map[string]any, logField string, valueFields ...string) []string {
	markers := make([]string, 0, len(valueFields))
	for _, valueField := range valueFields {
		value, ok := receipt[valueField].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return []string{valueField}
		}
		markers = append(markers, "[result] "+valueField+" "+value)
	}
	return requireLogContains(receiptPath, receipt, logField, markers...)
}

func requireNamedTunnelRunnerLog(receiptPath string, receipt map[string]any) []string {
	publicURL, ok := receipt["public_url"].(string)
	if !ok || strings.TrimSpace(publicURL) == "" {
		return []string{"public_url"}
	}
	return requireLogContains(receiptPath, receipt, "runner_log", "Started transwarp-runner", "Registered tunnel connection", "tunnel ready at "+publicURL)
}

func requireAppLaunchTunnelEvidence(receiptPath string, receipt map[string]any) []string {
	mode, ok := receipt["tunnel_mode"].(string)
	if !ok || strings.TrimSpace(mode) == "" || mode == "off" {
		return nil
	}
	if mode != "quick" {
		return []string{"tunnel_mode"}
	}

	missing := []string{}
	missing = append(missing, requireBool(receipt, "tunnel_ready", true)...)
	missing = append(missing, requireBool(receipt, "public_status_authenticated", true)...)
	missing = append(missing, requireHTTPSBaseURL(receipt, "public_url")...)
	missing = append(missing, requireLogContains(receiptPath, receipt, "public_diagnose_log", "diagnosis passed", "authenticated status reachable", "tunnel mode=quick", "ready=true", "job \"")...)
	missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "public_diagnose_log", "machine_id")...)
	missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "public_diagnose_log", "public_url")...)
	missing = append(missing, requireLogContains(receiptPath, receipt, "public_dispatch_log", "public url:", "Xcode ", "[build] passed")...)
	missing = append(missing, requireResultMarkers(receiptPath, receipt, "public_dispatch_log", "build_id", "job_id", "request_id")...)
	missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "public_dispatch_log", "public_url")...)
	missing = append(missing, requireLogContainsReceiptValue(receiptPath, receipt, "status_json", "public_url")...)
	return missing
}

func requireAppLaunchBundleHashes(receipt map[string]any, appPath string) []string {
	appExecutableHash, runnerHash, cloudflaredHash, manifestHash, err := appBundleHashes(appPath)
	if err != nil {
		return []string{"app_hashes"}
	}
	missing := []string{}
	for field, expected := range map[string]string{
		"app_executable_sha256": appExecutableHash,
		"runner_sha256":         runnerHash,
		"cloudflared_sha256":    cloudflaredHash,
		"manifest_sha256":       manifestHash,
	} {
		value, ok := receipt[field].(string)
		if !ok || value != expected {
			missing = append(missing, field)
		}
	}
	return missing
}

func appBundleHashes(appPath string) (string, string, string, string, error) {
	appExecutable := filepath.Join(appPath, "Contents", "MacOS", "Transwarp")
	runner := filepath.Join(appPath, "Contents", "Resources", "transwarp-runner")
	cloudflared := filepath.Join(appPath, "Contents", "Resources", "cloudflared")
	manifest := filepath.Join(appPath, "Contents", "Resources", "TranswarpManifest.json")
	appExecutableHash, err := sha256File(appExecutable)
	if err != nil {
		return "", "", "", "", err
	}
	runnerHash, err := sha256File(runner)
	if err != nil {
		return "", "", "", "", err
	}
	cloudflaredHash, err := sha256File(cloudflared)
	if err != nil {
		return "", "", "", "", err
	}
	manifestHash, err := sha256File(manifest)
	if err != nil {
		return "", "", "", "", err
	}
	return appExecutableHash, runnerHash, cloudflaredHash, manifestHash, nil
}

func requireAcceptedBuildMetadata(receiptPath string, receipt map[string]any, logField string) []string {
	logPath, ok := evidenceSiblingPath(receiptPath, receipt, logField)
	if !ok {
		return []string{logField}
	}
	buildID, ok := receipt["build_id"].(string)
	if !ok || strings.TrimSpace(buildID) == "" {
		return []string{"build_id"}
	}
	jobID, ok := receipt["job_id"].(string)
	if !ok || strings.TrimSpace(jobID) == "" {
		return []string{"job_id"}
	}
	requestID, ok := receipt["request_id"].(string)
	if !ok || strings.TrimSpace(requestID) == "" {
		return []string{"request_id"}
	}
	machineID, ok := receipt["machine_id"].(string)
	if !ok || strings.TrimSpace(machineID) == "" {
		return []string{"machine_id"}
	}
	publicURL, ok := receipt["public_url"].(string)
	if !ok || strings.TrimSpace(publicURL) == "" {
		return []string{"public_url"}
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		return []string{logField}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event struct {
			Kind      string `json:"kind"`
			Message   string `json:"message"`
			BuildID   string `json:"build_id"`
			JobID     string `json:"job_id"`
			RequestID string `json:"request_id"`
			MachineID string `json:"machine_id"`
			PublicURL string `json:"public_url"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Kind != "coordinator" || event.Message != "accepted runner build" {
			continue
		}
		if event.BuildID == buildID &&
			event.JobID == jobID &&
			event.RequestID == requestID &&
			event.MachineID == machineID &&
			event.PublicURL == publicURL {
			return nil
		}
	}
	return []string{logField}
}

func requireNamedTunnelResults(receiptPath string, receipt map[string]any) []string {
	resultsPath, ok := evidenceSiblingPath(receiptPath, receipt, "results_json")
	if !ok {
		return []string{"results_json"}
	}
	buildID, ok := receipt["build_id"].(string)
	if !ok || strings.TrimSpace(buildID) == "" {
		return []string{"build_id"}
	}
	jobID, ok := receipt["job_id"].(string)
	if !ok || strings.TrimSpace(jobID) == "" {
		return []string{"job_id"}
	}
	requestID, ok := receipt["request_id"].(string)
	if !ok || strings.TrimSpace(requestID) == "" {
		return []string{"request_id"}
	}
	if err := validateCoordinatorResults(resultsPath, acceptedRunnerBuild{
		BuildID:   buildID,
		JobID:     jobID,
		RequestID: requestID,
	}); err != nil {
		return []string{"results_json"}
	}
	return nil
}

func requireNamedTunnelTargetEvidence(receiptPath string, receipt map[string]any) []string {
	machineID, ok := receipt["machine_id"].(string)
	if !ok || strings.TrimSpace(machineID) == "" {
		return []string{"machine_id"}
	}
	jobID, ok := receipt["job_id"].(string)
	if !ok || strings.TrimSpace(jobID) == "" {
		return []string{"job_id"}
	}
	publicURL, ok := receipt["public_url"].(string)
	if !ok || strings.TrimSpace(publicURL) == "" {
		return []string{"public_url"}
	}

	missing := []string{}
	if !targetsContainRegisteredMachine(receiptPath, receipt, "targets_registered_json", machineID, jobID, publicURL) {
		missing = append(missing, "targets_registered_json")
	}
	if !targetsExcludeMachine(receiptPath, receipt, "targets_after_deregister_json", machineID) {
		missing = append(missing, "targets_after_deregister_json")
	}
	return missing
}

func targetsContainRegisteredMachine(receiptPath string, receipt map[string]any, field string, machineID string, jobID string, publicURL string) bool {
	targets, ok := readEvidenceTargets(receiptPath, receipt, field)
	if !ok {
		return false
	}
	for _, target := range targets {
		if target.MachineID != machineID || target.PublicURL != publicURL {
			continue
		}
		if target.AcceptingBuilds != nil && !*target.AcceptingBuilds {
			return false
		}
		for _, job := range target.Jobs {
			if job == jobID {
				return true
			}
		}
		return false
	}
	return false
}

func targetsExcludeMachine(receiptPath string, receipt map[string]any, field string, machineID string) bool {
	targets, ok := readEvidenceTargets(receiptPath, receipt, field)
	if !ok {
		return false
	}
	for _, target := range targets {
		if target.MachineID == machineID {
			return false
		}
	}
	return true
}

func readEvidenceTargets(receiptPath string, receipt map[string]any, field string) ([]evidenceTarget, bool) {
	path, ok := evidenceSiblingPath(receiptPath, receipt, field)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var targets []evidenceTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, false
	}
	return targets, true
}

type evidenceTarget struct {
	MachineID       string   `json:"machine_id"`
	PublicURL       string   `json:"public_url"`
	AcceptingBuilds *bool    `json:"accepting_builds"`
	Jobs            []string `json:"jobs"`
}

func evidenceSiblingPath(receiptPath string, receipt map[string]any, field string) (string, bool) {
	value, ok := receipt[field].(string)
	if !ok || strings.TrimSpace(value) == "" || filepath.IsAbs(value) {
		return "", false
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", false
	}
	base := filepath.Dir(receiptPath)
	path := filepath.Join(base, cleaned)
	relative, err := filepath.Rel(base, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", false
	}
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	return path, true
}

func requireEvidenceSiblingFile(receiptPath string, receipt map[string]any, field string) []string {
	path, ok := evidenceSiblingPath(receiptPath, receipt, field)
	if !ok {
		return []string{field}
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return []string{field}
	}
	return nil
}

func requireBundleHash(receipt map[string]any, field string, path string) []string {
	value, ok := receipt[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return []string{field}
	}
	actual, err := sha256File(path)
	if err != nil || value != actual {
		return []string{field}
	}
	return nil
}

func requireCleanMacStatus(receiptPath string, receipt map[string]any) []string {
	statusPath, ok := evidenceSiblingPath(receiptPath, receipt, "status_json")
	if !ok {
		return []string{"status_json"}
	}
	machineID, ok := receipt["machine_id"].(string)
	if !ok || strings.TrimSpace(machineID) == "" {
		return []string{"machine_id"}
	}
	jobID, ok := receipt["job_id"].(string)
	if !ok || strings.TrimSpace(jobID) == "" {
		return []string{"job_id"}
	}
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return []string{"status_json"}
	}
	var status struct {
		MachineID string   `json:"machine_id"`
		Jobs      []string `json:"jobs"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return []string{"status_json"}
	}
	if status.MachineID != machineID {
		return []string{"status_json"}
	}
	for _, job := range status.Jobs {
		if job == jobID {
			return nil
		}
	}
	return []string{"status_json"}
}

func decodeSingleJSON(data []byte, value any, disallowUnknownFields bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if disallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("JSON must contain one object")
}

func (auditor auditor) plistValue(path string, key string) string {
	output, err := auditor.options.RunCommand(auditor.ctx, "/usr/bin/plutil", "-extract", key, "raw", path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func commandDetail(output []byte, err error) string {
	detail := strings.TrimSpace(string(output))
	if err != nil {
		if detail == "" {
			return err.Error()
		}
		return detail + ": " + err.Error()
	}
	return detail
}

func compactDetail(detail string) string {
	lines := strings.Split(strings.TrimSpace(detail), "\n")
	if len(lines) > 5 {
		lines = lines[:5]
	}
	return strings.Join(lines, "\n")
}

func compactSummaryDetail(detail string) string {
	lines := strings.Split(strings.TrimSpace(detail), "\n")
	compacted := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		compacted = append(compacted, line)
		if len(compacted) == 3 {
			break
		}
	}
	return strings.Join(compacted, "; ")
}

func overallStatus(checks []Check, strict bool) string {
	hasMissing := false
	hasWarning := false
	for _, check := range checks {
		switch check.Status {
		case StatusFail:
			return StatusFail
		case StatusMissing:
			hasMissing = true
		case StatusWarning:
			hasWarning = true
		}
	}
	if strict && (hasMissing || hasWarning) {
		return StatusFail
	}
	if hasMissing {
		return StatusMissing
	}
	if hasWarning {
		return StatusWarning
	}
	return StatusPass
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return output, err
	}
	return output, nil
}

func ValidateEvidencePaths(paths ...string) error {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return errors.New(path + " is a directory")
		}
	}
	return nil
}

func ValidateOptionalFilePaths(paths ...string) error {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if info.IsDir() {
			return errors.New(path + " is a directory")
		}
	}
	return nil
}

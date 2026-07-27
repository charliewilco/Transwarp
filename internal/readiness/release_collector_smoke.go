package readiness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type ReleaseEvidenceCollectorSmokeOptions struct {
	AuditPath              string
	AuditStderrPath        string
	SelfHostedEvidencePath string
	ReleaseArchivePath     string
}

func ValidateReleaseEvidenceCollectorSmoke(options ReleaseEvidenceCollectorSmokeOptions) error {
	if options.AuditPath == "" {
		return errors.New("audit JSON path is required")
	}
	if options.AuditStderrPath == "" {
		return errors.New("audit stderr path is required")
	}
	if options.SelfHostedEvidencePath == "" {
		return errors.New("self-hosted evidence path is required")
	}
	if options.ReleaseArchivePath == "" {
		return errors.New("release archive path is required")
	}
	if err := requireRegularFile(options.AuditPath, "audit JSON"); err != nil {
		return err
	}
	if err := requireRegularFile(options.AuditStderrPath, "audit stderr"); err != nil {
		return err
	}
	if err := requireRegularFile(options.SelfHostedEvidencePath, "self-hosted evidence"); err != nil {
		return err
	}
	if err := requireRegularFile(options.ReleaseArchivePath, "release archive"); err != nil {
		return err
	}

	report, err := ReadReport(options.AuditPath)
	if err != nil {
		return err
	}
	expectedStatuses := map[string]string{
		"self_hosted_mac_readiness": StatusPass,
		"app_launch_smoke":          StatusPass,
		"release_archive_present":   StatusPass,
		"release_archive_readable":  StatusPass,
		"release_archive_contents":  StatusPass,
		"release_archive_checksums": StatusPass,
		"named_tunnel_smoke":        StatusMissing,
		"ci_dispatch_smoke":         StatusMissing,
		"clean_mac_validation":      StatusMissing,
	}
	for id, status := range expectedStatuses {
		check, ok := findReportCheck(report, id)
		if !ok {
			return fmt.Errorf("audit report missing check %s", id)
		}
		if check.Status != status {
			return fmt.Errorf("%s expected %s, got %s", id, status, check.Status)
		}
	}
	if report.Overall != StatusMissing {
		return fmt.Errorf("expected incomplete local audit, got %s", report.Overall)
	}

	appLaunchPath := filepath.Join(filepath.Dir(options.AuditPath), "app-launch-evidence.json")
	if check := appLaunchEvidenceCheck(appLaunchPath); check.Status != StatusPass {
		return fmt.Errorf("app launch evidence invalid: %s", checkSummary(check))
	}
	if check := selfHostedEvidenceCheck(options.SelfHostedEvidencePath); check.Status != StatusPass {
		return fmt.Errorf("self-hosted evidence invalid: %s", checkSummary(check))
	}
	return nil
}

func requireRegularFile(path string, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("missing %s: %w", label, err)
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New(label + " must be a regular file")
	}
	return nil
}

func findReportCheck(report Report, id string) (Check, bool) {
	for _, check := range report.Checks {
		if check.ID == id {
			return check, true
		}
	}
	return Check{}, false
}

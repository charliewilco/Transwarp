package readiness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charliewilco/transwarp/internal/endpoint"
	"github.com/charliewilco/transwarp/internal/requestmeta"
)

type AppLaunchEvidenceWriteOptions struct {
	OutputPath                string
	AppPath                   string
	TunnelMode                string
	PublicURL                 string
	MachineID                 string
	JobID                     string
	RequestID                 string
	BuildID                   string
	TunnelReady               bool
	PublicStatusAuthenticated bool
	BuildLogPath              string
	StatusJSONPath            string
	BuildStatusJSONPath       string
	PublicDiagnoseLogPath     string
	PublicDispatchLogPath     string
	AppLogPath                string
	AppStderrPath             string
	Now                       func() time.Time
}

type appLaunchEvidenceReceipt struct {
	Kind                      string `json:"kind"`
	SchemaVersion             int    `json:"schema_version"`
	Status                    string `json:"status"`
	GeneratedAt               string `json:"generated_at"`
	MachineID                 string `json:"machine_id"`
	JobID                     string `json:"job_id"`
	RequestID                 string `json:"request_id"`
	BuildID                   string `json:"build_id"`
	AppExecutableSHA256       string `json:"app_executable_sha256,omitempty"`
	RunnerSHA256              string `json:"runner_sha256,omitempty"`
	CloudflaredSHA256         string `json:"cloudflared_sha256,omitempty"`
	ManifestSHA256            string `json:"manifest_sha256,omitempty"`
	TunnelMode                string `json:"tunnel_mode"`
	PublicURL                 string `json:"public_url"`
	TunnelReady               bool   `json:"tunnel_ready"`
	PublicStatusAuthenticated bool   `json:"public_status_authenticated"`
	KeychainMigrated          bool   `json:"keychain_migrated"`
	AppLaunchPassed           bool   `json:"app_launch_passed"`
	HelperStatusAuthenticated bool   `json:"helper_status_authenticated"`
	BuildStarted              bool   `json:"build_started"`
	LogStreamPassed           bool   `json:"log_stream_passed"`
	BuildStatusPassed         bool   `json:"build_status_passed"`
	RecentBuildRecorded       bool   `json:"recent_build_recorded"`
	BuildLog                  string `json:"build_log"`
	StatusJSON                string `json:"status_json"`
	BuildStatusJSON           string `json:"build_status_json"`
	PublicDiagnoseLog         string `json:"public_diagnose_log,omitempty"`
	PublicDispatchLog         string `json:"public_dispatch_log,omitempty"`
	AppLog                    string `json:"app_log"`
	AppStderr                 string `json:"app_stderr"`
}

func WriteAppLaunchEvidence(options AppLaunchEvidenceWriteOptions) error {
	options.OutputPath = strings.TrimSpace(options.OutputPath)
	options.AppPath = strings.TrimSpace(options.AppPath)
	options.TunnelMode = strings.TrimSpace(options.TunnelMode)
	options.PublicURL = strings.TrimSpace(options.PublicURL)
	options.MachineID = strings.TrimSpace(options.MachineID)
	options.JobID = strings.TrimSpace(options.JobID)
	options.RequestID = strings.TrimSpace(options.RequestID)
	options.BuildID = strings.TrimSpace(options.BuildID)
	if options.OutputPath == "" {
		return errors.New("app launch evidence output path is required")
	}
	if options.TunnelMode == "" {
		options.TunnelMode = "off"
	}
	if options.TunnelMode != "off" && options.TunnelMode != "quick" {
		return errors.New("app launch tunnel mode must be off or quick")
	}
	if options.TunnelMode == "quick" {
		if err := endpoint.ValidateBaseURL(options.PublicURL, "public_url"); err != nil {
			return err
		}
		if strings.TrimSpace(options.PublicDiagnoseLogPath) == "" {
			return errors.New("app launch public diagnose log path is required for quick tunnel mode")
		}
		if strings.TrimSpace(options.PublicDispatchLogPath) == "" {
			return errors.New("app launch public dispatch log path is required for quick tunnel mode")
		}
	}
	if err := requestmeta.ValidateMachineID(options.MachineID); err != nil {
		return fmt.Errorf("machine_id is invalid: %w", err)
	}
	if err := requestmeta.ValidateJobID(options.JobID); err != nil {
		return fmt.Errorf("job_id is invalid: %w", err)
	}
	if err := requestmeta.ValidateRequestID(options.RequestID); err != nil {
		return fmt.Errorf("request_id is invalid: %w", err)
	}
	if err := requestmeta.ValidateBuildID(options.BuildID); err != nil {
		return fmt.Errorf("build_id is invalid: %w", err)
	}
	requiredPaths := map[string]string{
		"build log":         options.BuildLogPath,
		"status JSON":       options.StatusJSONPath,
		"build status JSON": options.BuildStatusJSONPath,
		"app log":           options.AppLogPath,
		"app stderr":        options.AppStderrPath,
	}
	for label, path := range requiredPaths {
		if strings.TrimSpace(path) == "" {
			return errors.New("app launch " + label + " path is required")
		}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	appExecutableHash := ""
	runnerHash := ""
	cloudflaredHash := ""
	manifestHash := ""
	if options.AppPath != "" {
		var err error
		appExecutableHash, runnerHash, cloudflaredHash, manifestHash, err = appBundleHashes(options.AppPath)
		if err != nil {
			return err
		}
	}

	outputDir := filepath.Dir(options.OutputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	receipt := appLaunchEvidenceReceipt{
		Kind:                      "transwarp-app-launch-evidence",
		SchemaVersion:             1,
		Status:                    "pass",
		GeneratedAt:               options.Now().UTC().Format(time.RFC3339Nano),
		MachineID:                 options.MachineID,
		JobID:                     options.JobID,
		RequestID:                 options.RequestID,
		BuildID:                   options.BuildID,
		AppExecutableSHA256:       appExecutableHash,
		RunnerSHA256:              runnerHash,
		CloudflaredSHA256:         cloudflaredHash,
		ManifestSHA256:            manifestHash,
		TunnelMode:                options.TunnelMode,
		PublicURL:                 options.PublicURL,
		TunnelReady:               options.TunnelReady,
		PublicStatusAuthenticated: options.PublicStatusAuthenticated,
		KeychainMigrated:          true,
		AppLaunchPassed:           true,
		HelperStatusAuthenticated: true,
		BuildStarted:              true,
		LogStreamPassed:           true,
		BuildStatusPassed:         true,
		RecentBuildRecorded:       true,
		BuildLog:                  "app-launch-build.ndjson",
		StatusJSON:                "app-launch-status.json",
		BuildStatusJSON:           "app-launch-build-status.json",
		AppLog:                    "app-launch-app.log",
		AppStderr:                 "app-launch-app.err",
	}
	companionNames := map[string]string{
		options.BuildLogPath:        receipt.BuildLog,
		options.StatusJSONPath:      receipt.StatusJSON,
		options.BuildStatusJSONPath: receipt.BuildStatusJSON,
		options.AppLogPath:          receipt.AppLog,
		options.AppStderrPath:       receipt.AppStderr,
	}
	if options.TunnelMode == "quick" {
		receipt.PublicDiagnoseLog = "app-launch-public-diagnose.log"
		receipt.PublicDispatchLog = "app-launch-public-dispatch.log"
		companionNames[options.PublicDiagnoseLogPath] = receipt.PublicDiagnoseLog
		companionNames[options.PublicDispatchLogPath] = receipt.PublicDispatchLog
	}
	for source, name := range companionNames {
		if err := copyEvidenceFile(source, filepath.Join(outputDir, name), name); err != nil {
			return err
		}
	}
	if err := writeJSONFile(options.OutputPath, receipt); err != nil {
		return err
	}
	if check := appLaunchEvidenceCheck(options.OutputPath); check.Status != StatusPass {
		return fmt.Errorf("app launch evidence invalid: %s", checkSummary(check))
	}
	return nil
}

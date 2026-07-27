package readiness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charliewilco/transwarp/internal/requestmeta"
)

type CleanMacEvidenceWriteOptions struct {
	OutputPath        string
	AppPath           string
	Architecture      string
	MacOS             string
	MachineID         string
	JobID             string
	RequestID         string
	BuildID           string
	ExitCode          int
	StatusJSONPath    string
	BuildLogPath      string
	BuildStatusPath   string
	CodesignLogPath   string
	StaplerLogPath    string
	GatekeeperLogPath string
	AppLogPath        string
	AppStderrPath     string
	Now               func() time.Time
}

type cleanMacEvidenceReceipt struct {
	Kind                string `json:"kind"`
	SchemaVersion       int    `json:"schema_version"`
	Status              string `json:"status"`
	GeneratedAt         string `json:"generated_at"`
	App                 string `json:"app"`
	Architecture        string `json:"architecture"`
	MacOS               string `json:"macos"`
	AppExecutableSHA256 string `json:"app_executable_sha256"`
	RunnerSHA256        string `json:"runner_sha256"`
	CloudflaredSHA256   string `json:"cloudflared_sha256"`
	ManifestSHA256      string `json:"manifest_sha256"`
	MachineID           string `json:"machine_id"`
	JobID               string `json:"job_id"`
	RequestID           string `json:"request_id"`
	BuildID             string `json:"build_id"`
	ExitCode            int    `json:"exit_code"`
	StrictCodesign      bool   `json:"strict_codesign"`
	NotarizationStaple  bool   `json:"notarization_staple"`
	GatekeeperAccepted  bool   `json:"gatekeeper_accepted"`
	AuthenticatedStatus bool   `json:"authenticated_status"`
	MachineIDMatched    bool   `json:"machine_id_matched"`
	JobAdvertised       bool   `json:"job_advertised"`
	FirstLaunchPassed   bool   `json:"first_launch_passed"`
	BuildStarted        bool   `json:"build_started"`
	LogStreamPassed     bool   `json:"log_stream_passed"`
	BuildStatusPassed   bool   `json:"build_status_passed"`
	RecentBuildRecorded bool   `json:"recent_build_recorded"`
	CodesignLog         string `json:"codesign_log"`
	StaplerLog          string `json:"stapler_log"`
	GatekeeperLog       string `json:"gatekeeper_log"`
	StatusJSON          string `json:"status_json"`
	BuildLog            string `json:"build_log"`
	BuildStatusJSON     string `json:"build_status_json"`
	AppLog              string `json:"app_log"`
	AppStderr           string `json:"app_stderr"`
}

func WriteCleanMacEvidence(options CleanMacEvidenceWriteOptions) error {
	options.OutputPath = strings.TrimSpace(options.OutputPath)
	options.AppPath = strings.TrimSpace(options.AppPath)
	if options.OutputPath == "" {
		return errors.New("clean-Mac evidence output path is required")
	}
	if options.AppPath == "" {
		return errors.New("app path is required")
	}
	if options.Now == nil {
		options.Now = time.Now
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
	for label, path := range map[string]string{
		"status JSON":       options.StatusJSONPath,
		"build log":         options.BuildLogPath,
		"build status JSON": options.BuildStatusPath,
		"codesign log":      options.CodesignLogPath,
		"stapler log":       options.StaplerLogPath,
		"Gatekeeper log":    options.GatekeeperLogPath,
		"app log":           options.AppLogPath,
		"app stderr":        options.AppStderrPath,
	} {
		if strings.TrimSpace(path) == "" {
			return errors.New(label + " path is required")
		}
	}

	appExecutableHash, runnerHash, cloudflaredHash, manifestHash, err := appBundleHashes(options.AppPath)
	if err != nil {
		return err
	}
	appName := filepath.Base(filepath.Clean(options.AppPath))

	outputDir := filepath.Dir(options.OutputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	for source, name := range map[string]string{
		options.StatusJSONPath:    "clean-mac-status.json",
		options.BuildLogPath:      "clean-mac-build.ndjson",
		options.BuildStatusPath:   "clean-mac-build-status.json",
		options.CodesignLogPath:   "clean-mac-codesign.log",
		options.StaplerLogPath:    "clean-mac-stapler.log",
		options.GatekeeperLogPath: "clean-mac-gatekeeper.log",
		options.AppLogPath:        "clean-mac-app.log",
		options.AppStderrPath:     "clean-mac-app.err",
	} {
		if err := copyEvidenceFile(source, filepath.Join(outputDir, name), name); err != nil {
			return err
		}
	}

	receipt := cleanMacEvidenceReceipt{
		Kind:                "transwarp-clean-mac-evidence",
		SchemaVersion:       1,
		Status:              "pass",
		GeneratedAt:         options.Now().UTC().Format(time.RFC3339Nano),
		App:                 appName,
		Architecture:        strings.TrimSpace(options.Architecture),
		MacOS:               strings.TrimSpace(options.MacOS),
		AppExecutableSHA256: appExecutableHash,
		RunnerSHA256:        runnerHash,
		CloudflaredSHA256:   cloudflaredHash,
		ManifestSHA256:      manifestHash,
		MachineID:           options.MachineID,
		JobID:               options.JobID,
		RequestID:           options.RequestID,
		BuildID:             options.BuildID,
		ExitCode:            options.ExitCode,
		StrictCodesign:      true,
		NotarizationStaple:  true,
		GatekeeperAccepted:  true,
		AuthenticatedStatus: true,
		MachineIDMatched:    true,
		JobAdvertised:       true,
		FirstLaunchPassed:   true,
		BuildStarted:        true,
		LogStreamPassed:     true,
		BuildStatusPassed:   true,
		RecentBuildRecorded: true,
		CodesignLog:         "clean-mac-codesign.log",
		StaplerLog:          "clean-mac-stapler.log",
		GatekeeperLog:       "clean-mac-gatekeeper.log",
		StatusJSON:          "clean-mac-status.json",
		BuildLog:            "clean-mac-build.ndjson",
		BuildStatusJSON:     "clean-mac-build-status.json",
		AppLog:              "clean-mac-app.log",
		AppStderr:           "clean-mac-app.err",
	}
	if err := writeJSONFile(options.OutputPath, receipt); err != nil {
		return err
	}
	if check := cleanMacEvidenceCheck(options.OutputPath, options.AppPath); check.Status != StatusPass {
		return fmt.Errorf("clean-Mac evidence invalid: %s", checkSummary(check))
	}
	return nil
}

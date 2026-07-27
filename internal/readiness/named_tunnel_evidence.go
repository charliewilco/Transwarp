package readiness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charliewilco/transwarp/internal/endpoint"
	"github.com/charliewilco/transwarp/internal/requestmeta"
)

type NamedTunnelEvidenceWriteOptions struct {
	OutputPath                 string
	AppPath                    string
	LaunchMode                 string
	PublicURL                  string
	AccessProtected            bool
	MachineID                  string
	JobID                      string
	RequestID                  string
	DiagnoseLogPath            string
	DispatchLogPath            string
	RunnerLogPath              string
	AppLogPath                 string
	AppStderrPath              string
	TargetsRegisteredPath      string
	TargetsAfterDeregisterPath string
	ResultsPath                string
	Now                        func() time.Time
}

type namedTunnelEvidenceReceipt struct {
	Kind                       string `json:"kind"`
	SchemaVersion              int    `json:"schema_version"`
	Status                     string `json:"status"`
	GeneratedAt                string `json:"generated_at"`
	TunnelMode                 string `json:"tunnel_mode"`
	LaunchMode                 string `json:"launch_mode"`
	PublicURL                  string `json:"public_url"`
	AccessProtected            bool   `json:"access_protected"`
	Coordinator                bool   `json:"coordinator"`
	AppLaunchPassed            bool   `json:"app_launch_passed"`
	KeychainMigrated           bool   `json:"keychain_migrated"`
	AppExecutableSHA256        string `json:"app_executable_sha256,omitempty"`
	RunnerSHA256               string `json:"runner_sha256,omitempty"`
	CloudflaredSHA256          string `json:"cloudflared_sha256,omitempty"`
	ManifestSHA256             string `json:"manifest_sha256,omitempty"`
	TargetRegistered           bool   `json:"target_registered"`
	MachineID                  string `json:"machine_id"`
	DiagnosePassed             bool   `json:"diagnose_passed"`
	DispatchPassed             bool   `json:"dispatch_passed"`
	ResultRecorded             bool   `json:"result_recorded"`
	Deregistered               bool   `json:"deregistered"`
	BuildID                    string `json:"build_id"`
	JobID                      string `json:"job_id"`
	RequestID                  string `json:"request_id"`
	DispatchLog                string `json:"dispatch_log"`
	DiagnoseLog                string `json:"diagnose_log"`
	RunnerLog                  string `json:"runner_log"`
	AppLog                     string `json:"app_log,omitempty"`
	AppStderr                  string `json:"app_stderr,omitempty"`
	ResultsJSON                string `json:"results_json"`
	TargetsRegisteredJSON      string `json:"targets_registered_json"`
	TargetsAfterDeregisterJSON string `json:"targets_after_deregister_json"`
}

type acceptedRunnerBuild struct {
	BuildID   string `json:"build_id"`
	JobID     string `json:"job_id"`
	RequestID string `json:"request_id"`
	MachineID string `json:"machine_id"`
	PublicURL string `json:"public_url"`
}

func WriteNamedTunnelEvidence(options NamedTunnelEvidenceWriteOptions) error {
	options.OutputPath = strings.TrimSpace(options.OutputPath)
	options.AppPath = strings.TrimSpace(options.AppPath)
	if options.OutputPath == "" {
		return errors.New("named tunnel evidence output path is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	options.LaunchMode = strings.TrimSpace(options.LaunchMode)
	if options.LaunchMode == "" {
		options.LaunchMode = "app"
	}
	if options.LaunchMode != "app" && options.LaunchMode != "runner" {
		return errors.New("named tunnel launch mode must be app or runner")
	}
	publicURL := strings.TrimSpace(options.PublicURL)
	if err := endpoint.ValidateBaseURL(publicURL, "public_url"); err != nil {
		return err
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
	requiredPaths := map[string]string{
		"diagnose log":                 options.DiagnoseLogPath,
		"dispatch log":                 options.DispatchLogPath,
		"runner log":                   options.RunnerLogPath,
		"targets registered snapshot":  options.TargetsRegisteredPath,
		"targets after deregister":     options.TargetsAfterDeregisterPath,
		"coordinator results snapshot": options.ResultsPath,
	}
	for label, path := range requiredPaths {
		if strings.TrimSpace(path) == "" {
			return errors.New(label + " path is required")
		}
	}
	if options.LaunchMode == "app" {
		if strings.TrimSpace(options.AppLogPath) == "" {
			return errors.New("app log path is required for app launch mode")
		}
		if strings.TrimSpace(options.AppStderrPath) == "" {
			return errors.New("app stderr path is required for app launch mode")
		}
	}

	dispatchLog, err := os.ReadFile(options.DispatchLogPath)
	if err != nil {
		return err
	}
	accepted, ok := parseAcceptedRunnerBuild(dispatchLog, acceptedRunnerBuild{
		JobID:     options.JobID,
		RequestID: options.RequestID,
		MachineID: options.MachineID,
		PublicURL: publicURL,
	})
	if !ok {
		return errors.New("dispatch log did not include accepted runner build metadata")
	}
	if err := requestmeta.ValidateBuildID(accepted.BuildID); err != nil {
		return fmt.Errorf("accepted build_id is invalid: %w", err)
	}
	if err := validateCoordinatorResults(options.ResultsPath, accepted); err != nil {
		return err
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
	companionNames := map[string]string{
		options.DiagnoseLogPath:            "named-tunnel-diagnose.log",
		options.DispatchLogPath:            "named-tunnel-dispatch.log",
		options.RunnerLogPath:              "named-tunnel-runner.log",
		options.ResultsPath:                "named-tunnel-results.json",
		options.TargetsRegisteredPath:      "named-tunnel-targets-registered.json",
		options.TargetsAfterDeregisterPath: "named-tunnel-targets-after-deregister.json",
	}
	if options.LaunchMode == "app" {
		companionNames[options.AppLogPath] = "named-tunnel-app.log"
		companionNames[options.AppStderrPath] = "named-tunnel-app.err"
	}
	for source, name := range companionNames {
		if err := copyEvidenceFile(source, filepath.Join(outputDir, name), name); err != nil {
			return err
		}
	}

	receipt := namedTunnelEvidenceReceipt{
		Kind:                       "transwarp-named-tunnel-evidence",
		SchemaVersion:              1,
		Status:                     "pass",
		GeneratedAt:                options.Now().UTC().Format(time.RFC3339Nano),
		TunnelMode:                 "named",
		LaunchMode:                 options.LaunchMode,
		PublicURL:                  publicURL,
		AccessProtected:            options.AccessProtected,
		Coordinator:                true,
		AppLaunchPassed:            options.LaunchMode == "app",
		KeychainMigrated:           options.LaunchMode == "app",
		AppExecutableSHA256:        appExecutableHash,
		RunnerSHA256:               runnerHash,
		CloudflaredSHA256:          cloudflaredHash,
		ManifestSHA256:             manifestHash,
		TargetRegistered:           true,
		MachineID:                  options.MachineID,
		DiagnosePassed:             true,
		DispatchPassed:             true,
		ResultRecorded:             true,
		Deregistered:               true,
		BuildID:                    accepted.BuildID,
		JobID:                      options.JobID,
		RequestID:                  options.RequestID,
		DispatchLog:                "named-tunnel-dispatch.log",
		DiagnoseLog:                "named-tunnel-diagnose.log",
		RunnerLog:                  "named-tunnel-runner.log",
		ResultsJSON:                "named-tunnel-results.json",
		TargetsRegisteredJSON:      "named-tunnel-targets-registered.json",
		TargetsAfterDeregisterJSON: "named-tunnel-targets-after-deregister.json",
	}
	if options.LaunchMode == "app" {
		receipt.AppLog = "named-tunnel-app.log"
		receipt.AppStderr = "named-tunnel-app.err"
	}
	if err := writeJSONFile(options.OutputPath, receipt); err != nil {
		return err
	}
	if check := namedTunnelEvidenceCheck(options.OutputPath, options.AppPath); check.Status != StatusPass {
		return fmt.Errorf("named tunnel evidence invalid: %s", checkSummary(check))
	}
	return nil
}

func parseAcceptedRunnerBuild(data []byte, expected acceptedRunnerBuild) (acceptedRunnerBuild, bool) {
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
		if event.JobID == expected.JobID &&
			event.RequestID == expected.RequestID &&
			event.MachineID == expected.MachineID &&
			event.PublicURL == expected.PublicURL {
			return acceptedRunnerBuild{
				BuildID:   event.BuildID,
				JobID:     event.JobID,
				RequestID: event.RequestID,
				MachineID: event.MachineID,
				PublicURL: event.PublicURL,
			}, true
		}
	}
	return acceptedRunnerBuild{}, false
}

func validateCoordinatorResults(path string, expected acceptedRunnerBuild) error {
	return validateAcceptedBuildResults(path, expected, "named tunnel")
}

func validateAcceptedBuildResults(path string, expected acceptedRunnerBuild, label string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var results []struct {
		BuildID   string `json:"build_id"`
		JobID     string `json:"job_id"`
		RequestID string `json:"request_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(data, &results); err != nil {
		return err
	}
	for _, result := range results {
		if result.RequestID != expected.RequestID {
			continue
		}
		if result.BuildID != expected.BuildID {
			return errors.New(label + " result build_id did not match accepted-build metadata")
		}
		if result.JobID != expected.JobID {
			return errors.New(label + " result job_id did not match dispatch job")
		}
		if result.Status != "passed" {
			return errors.New(label + " result did not report passed")
		}
		return nil
	}
	return errors.New(label + " smoke results did not include request_id")
}

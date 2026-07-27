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

type QuickTunnelEvidenceWriteOptions struct {
	OutputPath                    string
	PublicURL                     string
	Coordinator                   bool
	MachineID                     string
	JobID                         string
	RequestID                     string
	DiagnoseLogPath               string
	DispatchLogPath               string
	TargetsBeforeDispatchLogPath  string
	TargetsAfterDeregisterLogPath string
	ResultsPath                   string
	Now                           func() time.Time
}

type quickTunnelEvidenceReceipt struct {
	Kind                      string `json:"kind"`
	SchemaVersion             int    `json:"schema_version"`
	Status                    string `json:"status"`
	GeneratedAt               string `json:"generated_at"`
	TunnelMode                string `json:"tunnel_mode"`
	PublicURL                 string `json:"public_url"`
	BuildID                   string `json:"build_id,omitempty"`
	JobID                     string `json:"job_id,omitempty"`
	RequestID                 string `json:"request_id,omitempty"`
	MachineID                 string `json:"machine_id,omitempty"`
	Coordinator               bool   `json:"coordinator"`
	TargetRegistered          bool   `json:"target_registered,omitempty"`
	DiagnosePassed            bool   `json:"diagnose_passed"`
	DispatchPassed            bool   `json:"dispatch_passed"`
	ResultRecorded            bool   `json:"result_recorded,omitempty"`
	Deregistered              bool   `json:"deregistered,omitempty"`
	ReleaseEvidence           bool   `json:"release_evidence"`
	DispatchLog               string `json:"dispatch_log"`
	DiagnoseLog               string `json:"diagnose_log"`
	TargetsBeforeDispatchLog  string `json:"targets_before_dispatch_log,omitempty"`
	TargetsAfterDeregisterLog string `json:"targets_after_deregister_log,omitempty"`
}

func WriteQuickTunnelEvidence(options QuickTunnelEvidenceWriteOptions) error {
	options.OutputPath = strings.TrimSpace(options.OutputPath)
	options.PublicURL = strings.TrimSpace(options.PublicURL)
	options.DiagnoseLogPath = strings.TrimSpace(options.DiagnoseLogPath)
	options.DispatchLogPath = strings.TrimSpace(options.DispatchLogPath)
	if options.OutputPath == "" {
		return errors.New("quick tunnel evidence output path is required")
	}
	if err := endpoint.ValidateBaseURL(options.PublicURL, "public_url"); err != nil {
		return err
	}
	if options.DiagnoseLogPath == "" {
		return errors.New("quick tunnel diagnose log path is required")
	}
	if options.DispatchLogPath == "" {
		return errors.New("quick tunnel dispatch log path is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	receipt := quickTunnelEvidenceReceipt{
		Kind:            "transwarp-quick-tunnel-diagnostic",
		SchemaVersion:   1,
		Status:          "pass",
		GeneratedAt:     options.Now().UTC().Format(time.RFC3339Nano),
		TunnelMode:      "quick",
		PublicURL:       options.PublicURL,
		Coordinator:     options.Coordinator,
		DiagnosePassed:  true,
		DispatchPassed:  true,
		ReleaseEvidence: false,
		DispatchLog:     "quick-tunnel-direct-dispatch.log",
		DiagnoseLog:     "quick-tunnel-direct-diagnose.log",
	}

	if options.Coordinator {
		accepted, err := quickTunnelCoordinatorAcceptedBuild(options)
		if err != nil {
			return err
		}
		receipt.BuildID = accepted.BuildID
		receipt.JobID = accepted.JobID
		receipt.RequestID = accepted.RequestID
		receipt.MachineID = accepted.MachineID
		receipt.TargetRegistered = true
		receipt.ResultRecorded = true
		receipt.Deregistered = true
		receipt.DispatchLog = "quick-tunnel-coordinator-dispatch.log"
		receipt.DiagnoseLog = "quick-tunnel-coordinator-diagnose.log"
		receipt.TargetsBeforeDispatchLog = "quick-tunnel-coordinator-targets-before-dispatch.json"
		receipt.TargetsAfterDeregisterLog = "quick-tunnel-coordinator-targets-after-deregister.json"
	}

	outputDir := filepath.Dir(options.OutputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	companionNames := map[string]string{
		options.DispatchLogPath: receipt.DispatchLog,
		options.DiagnoseLogPath: receipt.DiagnoseLog,
	}
	if options.Coordinator {
		companionNames[options.TargetsBeforeDispatchLogPath] = receipt.TargetsBeforeDispatchLog
		companionNames[options.TargetsAfterDeregisterLogPath] = receipt.TargetsAfterDeregisterLog
	}
	for source, name := range companionNames {
		if err := copyEvidenceFile(source, filepath.Join(outputDir, name), name); err != nil {
			return err
		}
	}
	if err := writeJSONFile(options.OutputPath, receipt); err != nil {
		return err
	}
	return ValidateReceiptShape(options.OutputPath, "transwarp-quick-tunnel-diagnostic")
}

func quickTunnelCoordinatorAcceptedBuild(options QuickTunnelEvidenceWriteOptions) (acceptedRunnerBuild, error) {
	options.MachineID = strings.TrimSpace(options.MachineID)
	options.JobID = strings.TrimSpace(options.JobID)
	options.RequestID = strings.TrimSpace(options.RequestID)
	options.TargetsBeforeDispatchLogPath = strings.TrimSpace(options.TargetsBeforeDispatchLogPath)
	options.TargetsAfterDeregisterLogPath = strings.TrimSpace(options.TargetsAfterDeregisterLogPath)
	options.ResultsPath = strings.TrimSpace(options.ResultsPath)
	if err := requestmeta.ValidateMachineID(options.MachineID); err != nil {
		return acceptedRunnerBuild{}, fmt.Errorf("machine_id is invalid: %w", err)
	}
	if err := requestmeta.ValidateJobID(options.JobID); err != nil {
		return acceptedRunnerBuild{}, fmt.Errorf("job_id is invalid: %w", err)
	}
	if err := requestmeta.ValidateRequestID(options.RequestID); err != nil {
		return acceptedRunnerBuild{}, fmt.Errorf("request_id is invalid: %w", err)
	}
	requiredPaths := map[string]string{
		"targets before dispatch log":  options.TargetsBeforeDispatchLogPath,
		"targets after deregister log": options.TargetsAfterDeregisterLogPath,
		"coordinator results snapshot": options.ResultsPath,
	}
	for label, path := range requiredPaths {
		if path == "" {
			return acceptedRunnerBuild{}, errors.New(label + " path is required")
		}
	}

	dispatchLog, err := os.ReadFile(options.DispatchLogPath)
	if err != nil {
		return acceptedRunnerBuild{}, err
	}
	accepted, ok := parseAcceptedRunnerBuild(dispatchLog, acceptedRunnerBuild{
		JobID:     options.JobID,
		RequestID: options.RequestID,
		MachineID: options.MachineID,
		PublicURL: options.PublicURL,
	})
	if !ok {
		return acceptedRunnerBuild{}, errors.New("dispatch log did not include accepted runner build metadata")
	}
	if err := requestmeta.ValidateBuildID(accepted.BuildID); err != nil {
		return acceptedRunnerBuild{}, fmt.Errorf("accepted build_id is invalid: %w", err)
	}
	if err := validateAcceptedBuildResults(options.ResultsPath, accepted, "quick tunnel"); err != nil {
		return acceptedRunnerBuild{}, err
	}
	return accepted, nil
}

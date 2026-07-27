package readiness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SelfHostedEvidenceWriteOptions struct {
	OutputPath                   string
	Architecture                 string
	MacOS                        string
	DeveloperDir                 string
	Xcode                        string
	CodeSigningIdentitiesVisible bool
	GitHubActions                bool
	RunnerName                   string
	RunnerOS                     string
	SourceLogPath                string
	Now                          func() time.Time
}

type selfHostedEvidenceReceipt struct {
	Kind                         string `json:"kind"`
	SchemaVersion                int    `json:"schema_version"`
	Status                       string `json:"status"`
	GeneratedAt                  string `json:"generated_at"`
	Architecture                 string `json:"architecture"`
	MacOS                        string `json:"macos"`
	DeveloperDir                 string `json:"developer_dir"`
	Xcode                        string `json:"xcode"`
	CodeSigningIdentitiesVisible bool   `json:"code_signing_identities_visible"`
	GitHubActions                bool   `json:"github_actions"`
	RunnerName                   string `json:"runner_name"`
	RunnerOS                     string `json:"runner_os"`
	SourceLog                    string `json:"source_log"`
}

func WriteSelfHostedEvidence(options SelfHostedEvidenceWriteOptions) error {
	options.OutputPath = strings.TrimSpace(options.OutputPath)
	options.SourceLogPath = strings.TrimSpace(options.SourceLogPath)
	if options.OutputPath == "" {
		return errors.New("self-hosted evidence output path is required")
	}
	if options.SourceLogPath == "" {
		return errors.New("self-hosted source log path is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	outputDir := filepath.Dir(options.OutputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	if err := copyEvidenceFile(options.SourceLogPath, filepath.Join(outputDir, "self-hosted-readiness.log"), "self-hosted readiness source log"); err != nil {
		return err
	}

	receipt := selfHostedEvidenceReceipt{
		Kind:                         "transwarp-self-hosted-mac-readiness",
		SchemaVersion:                1,
		Status:                       "pass",
		GeneratedAt:                  options.Now().UTC().Format(time.RFC3339Nano),
		Architecture:                 strings.TrimSpace(options.Architecture),
		MacOS:                        strings.TrimSpace(options.MacOS),
		DeveloperDir:                 strings.TrimSpace(options.DeveloperDir),
		Xcode:                        strings.TrimSpace(options.Xcode),
		CodeSigningIdentitiesVisible: options.CodeSigningIdentitiesVisible,
		GitHubActions:                options.GitHubActions,
		RunnerName:                   strings.TrimSpace(options.RunnerName),
		RunnerOS:                     strings.TrimSpace(options.RunnerOS),
		SourceLog:                    "self-hosted-readiness.log",
	}
	if err := writeJSONFile(options.OutputPath, receipt); err != nil {
		return err
	}
	if check := selfHostedEvidenceCheck(options.OutputPath); check.Status != StatusPass {
		return fmt.Errorf("self-hosted evidence invalid: %s", checkSummary(check))
	}
	return nil
}

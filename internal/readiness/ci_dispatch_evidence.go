package readiness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charliewilco/transwarp/internal/requestmeta"
)

type GitHubActionsEvidenceContext struct {
	GitHubActions bool
	RunID         string
	RunAttempt    string
	Workflow      string
	Job           string
	Repository    string
	SHA           string
	RunnerOS      string
	RunnerArch    string
	RunnerName    string
}

type CIDispatchEvidenceWriteOptions struct {
	NamedTunnelEvidencePath string
	SourceLogPath           string
	SourceLogName           string
	OutputPath              string
	GitHub                  GitHubActionsEvidenceContext
	Now                     func() time.Time
}

type ciDispatchReceipt struct {
	Kind           string `json:"kind"`
	SchemaVersion  int    `json:"schema_version"`
	Status         string `json:"status"`
	GeneratedAt    string `json:"generated_at"`
	GitHubActions  bool   `json:"github_actions"`
	ResultRecorded bool   `json:"result_recorded"`
	RunID          string `json:"run_id"`
	RunAttempt     string `json:"run_attempt"`
	Workflow       string `json:"workflow"`
	Job            string `json:"job"`
	Repository     string `json:"repository"`
	SHA            string `json:"sha"`
	RunnerOS       string `json:"runner_os"`
	RunnerArch     string `json:"runner_arch"`
	RunnerName     string `json:"runner_name,omitempty"`
	PublicURL      string `json:"public_url"`
	BuildID        string `json:"build_id"`
	JobID          string `json:"job_id"`
	RequestID      string `json:"request_id"`
	MachineID      string `json:"machine_id"`
	SourceLog      string `json:"source_log"`
}

func WriteCIDispatchEvidence(options CIDispatchEvidenceWriteOptions) error {
	options.NamedTunnelEvidencePath = strings.TrimSpace(options.NamedTunnelEvidencePath)
	options.SourceLogPath = strings.TrimSpace(options.SourceLogPath)
	options.SourceLogName = strings.TrimSpace(options.SourceLogName)
	options.OutputPath = strings.TrimSpace(options.OutputPath)
	if options.NamedTunnelEvidencePath == "" {
		return errors.New("named tunnel evidence path is required")
	}
	if options.SourceLogPath == "" {
		return errors.New("CI dispatch source log path is required")
	}
	if options.OutputPath == "" {
		return errors.New("CI dispatch evidence output path is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.SourceLogName == "" {
		options.SourceLogName = filepath.Base(options.SourceLogPath)
	}
	if err := validateEvidenceSourceLogName(options.SourceLogName); err != nil {
		return err
	}
	if err := validateGitHubActionsContext(options.GitHub); err != nil {
		return err
	}
	if check := namedTunnelEvidenceCheck(options.NamedTunnelEvidencePath); check.Status != StatusPass {
		return fmt.Errorf("named tunnel evidence invalid: %s", checkSummary(check))
	}

	namedReceipt, err := readNamedTunnelReceipt(options.NamedTunnelEvidencePath)
	if err != nil {
		return err
	}
	sourceLog, err := os.ReadFile(options.SourceLogPath)
	if err != nil {
		return err
	}
	if !sourceLogContainsAcceptedBuild(sourceLog, namedReceipt) {
		return errors.New("CI dispatch source log did not include matching accepted runner build metadata")
	}

	outputDir := filepath.Dir(options.OutputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	siblingLogPath := filepath.Join(outputDir, options.SourceLogName)
	if err := copyEvidenceSourceLog(options.SourceLogPath, siblingLogPath); err != nil {
		return err
	}

	receipt := ciDispatchReceipt{
		Kind:           "transwarp-ci-dispatch-evidence",
		SchemaVersion:  1,
		Status:         "pass",
		GeneratedAt:    options.Now().UTC().Format(time.RFC3339Nano),
		GitHubActions:  true,
		ResultRecorded: true,
		RunID:          strings.TrimSpace(options.GitHub.RunID),
		RunAttempt:     strings.TrimSpace(options.GitHub.RunAttempt),
		Workflow:       strings.TrimSpace(options.GitHub.Workflow),
		Job:            strings.TrimSpace(options.GitHub.Job),
		Repository:     strings.TrimSpace(options.GitHub.Repository),
		SHA:            strings.TrimSpace(options.GitHub.SHA),
		RunnerOS:       strings.TrimSpace(options.GitHub.RunnerOS),
		RunnerArch:     strings.TrimSpace(options.GitHub.RunnerArch),
		RunnerName:     strings.TrimSpace(options.GitHub.RunnerName),
		PublicURL:      namedReceipt.PublicURL,
		BuildID:        namedReceipt.BuildID,
		JobID:          namedReceipt.JobID,
		RequestID:      namedReceipt.RequestID,
		MachineID:      namedReceipt.MachineID,
		SourceLog:      options.SourceLogName,
	}
	if err := writeJSONFile(options.OutputPath, receipt); err != nil {
		return err
	}
	if check := ciDispatchEvidenceCheck(options.OutputPath); check.Status != StatusPass {
		return fmt.Errorf("CI dispatch evidence invalid: %s", checkSummary(check))
	}
	return nil
}

type namedTunnelReceipt struct {
	Kind          string `json:"kind"`
	SchemaVersion int    `json:"schema_version"`
	Status        string `json:"status"`
	PublicURL     string `json:"public_url"`
	BuildID       string `json:"build_id"`
	JobID         string `json:"job_id"`
	RequestID     string `json:"request_id"`
	MachineID     string `json:"machine_id"`
}

func readNamedTunnelReceipt(path string) (namedTunnelReceipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return namedTunnelReceipt{}, err
	}
	var receipt namedTunnelReceipt
	if err := decodeSingleJSON(data, &receipt, false); err != nil {
		return namedTunnelReceipt{}, err
	}
	if receipt.Kind != "transwarp-named-tunnel-evidence" {
		return namedTunnelReceipt{}, errors.New("named tunnel evidence has unexpected kind")
	}
	if receipt.SchemaVersion != 1 {
		return namedTunnelReceipt{}, errors.New("named tunnel evidence has unexpected schema_version")
	}
	if receipt.Status != "pass" {
		return namedTunnelReceipt{}, errors.New("named tunnel evidence did not pass")
	}
	if err := requestmeta.ValidateBuildID(receipt.BuildID); err != nil {
		return namedTunnelReceipt{}, fmt.Errorf("named tunnel evidence build_id is invalid: %w", err)
	}
	if err := requestmeta.ValidateJobID(receipt.JobID); err != nil {
		return namedTunnelReceipt{}, fmt.Errorf("named tunnel evidence job_id is invalid: %w", err)
	}
	if err := requestmeta.ValidateRequestID(receipt.RequestID); err != nil {
		return namedTunnelReceipt{}, fmt.Errorf("named tunnel evidence request_id is invalid: %w", err)
	}
	if err := requestmeta.ValidateMachineID(receipt.MachineID); err != nil {
		return namedTunnelReceipt{}, fmt.Errorf("named tunnel evidence machine_id is invalid: %w", err)
	}
	receipt.PublicURL = strings.TrimSpace(receipt.PublicURL)
	if receipt.PublicURL == "" {
		return namedTunnelReceipt{}, errors.New("named tunnel evidence public_url is required")
	}
	return receipt, nil
}

func validateGitHubActionsContext(context GitHubActionsEvidenceContext) error {
	receipt := map[string]any{
		"github_actions": context.GitHubActions,
		"run_id":         strings.TrimSpace(context.RunID),
		"run_attempt":    strings.TrimSpace(context.RunAttempt),
		"workflow":       strings.TrimSpace(context.Workflow),
		"job":            strings.TrimSpace(context.Job),
		"repository":     strings.TrimSpace(context.Repository),
		"sha":            strings.TrimSpace(context.SHA),
		"runner_os":      strings.TrimSpace(context.RunnerOS),
		"runner_arch":    strings.TrimSpace(context.RunnerArch),
	}
	checks := [][]string{
		requireBool(receipt, "github_actions", true),
		requireUnsignedDecimalString(receipt, "run_id"),
		requireUnsignedDecimalString(receipt, "run_attempt"),
		requireNonEmptyString(receipt, "workflow"),
		requireNonEmptyString(receipt, "job"),
		requireGitHubRepository(receipt, "repository"),
		requireGitSHA(receipt, "sha"),
		requireString(receipt, "runner_os", "macOS"),
		requireString(receipt, "runner_arch", "ARM64"),
	}
	missing := []string{}
	for _, check := range checks {
		missing = append(missing, check...)
	}
	if len(missing) > 0 {
		return errors.New("GitHub Actions context incomplete: " + strings.Join(missing, ", "))
	}
	return nil
}

func validateEvidenceSourceLogName(name string) error {
	if name == "" || filepath.IsAbs(name) {
		return errors.New("CI dispatch source log name must be a relative file name")
	}
	cleaned := filepath.Clean(name)
	if cleaned != name || cleaned == "." || cleaned == ".." || strings.Contains(name, string(os.PathSeparator)) {
		return errors.New("CI dispatch source log name must be a relative file name")
	}
	return nil
}

func sourceLogContainsAcceptedBuild(data []byte, expected namedTunnelReceipt) bool {
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
		if event.BuildID == expected.BuildID &&
			event.JobID == expected.JobID &&
			event.RequestID == expected.RequestID &&
			event.MachineID == expected.MachineID &&
			event.PublicURL == expected.PublicURL {
			return true
		}
	}
	return false
}

func copyEvidenceSourceLog(source string, destination string) error {
	return copyEvidenceFile(source, destination, "CI dispatch source log")
}

func copyEvidenceFile(source string, destination string, label string) error {
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if sourceInfo.IsDir() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New(label + " must be a regular file")
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	destinationAbs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if sourceAbs == destinationAbs {
		return nil
	}
	if destinationInfo, err := os.Lstat(destination); err == nil {
		if destinationInfo.IsDir() || destinationInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New(label + " destination must be a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writeJSONFile(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "\t")
	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func checkSummary(check Check) string {
	detail := strings.TrimSpace(check.Detail)
	if detail == "" {
		return check.Summary
	}
	return check.Summary + ": " + compactSummaryDetail(detail)
}

package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/charliewilco/transwarp/internal/endpoint"
	"github.com/charliewilco/transwarp/internal/headerutil"
	"github.com/charliewilco/transwarp/internal/requestmeta"
)

type Config struct {
	ListenAddress            string       `json:"listen_address"`
	MachineID                string       `json:"machine_id"`
	MachineName              string       `json:"machine_name"`
	SharedToken              string       `json:"shared_token"`
	WorkspaceRoot            string       `json:"workspace_root"`
	PreventSleep             bool         `json:"prevent_sleep"`
	RedactedValues           []string     `json:"redacted_values"`
	CIRegistrationURL        string       `json:"ci_registration_url"`
	CIHeartbeatURL           string       `json:"ci_heartbeat_url"`
	CIDeregistrationURL      string       `json:"ci_deregistration_url"`
	RegistrationToken        string       `json:"registration_token"`
	CIAccessClientID         string       `json:"ci_access_client_id"`
	CIAccessClientSecret     string       `json:"ci_access_client_secret"`
	RunnerAccessClientID     string       `json:"runner_access_client_id"`
	RunnerAccessClientSecret string       `json:"runner_access_client_secret"`
	AllowedReportOrigins     []string     `json:"allowed_report_origins"`
	HeartbeatSeconds         int          `json:"heartbeat_seconds"`
	Tunnel                   TunnelConfig `json:"tunnel"`
	Jobs                     []JobConfig  `json:"jobs"`
	Capabilities             Capabilities `json:"-"`
}

type TunnelConfig struct {
	Mode            string `json:"mode"`
	CloudflaredPath string `json:"cloudflared_path"`
	Token           string `json:"token"`
	Name            string `json:"name"`
	PublicURL       string `json:"public_url"`
}

type JobConfig struct {
	ID                          string            `json:"id"`
	Label                       string            `json:"label"`
	WorkingDirectory            string            `json:"working_directory"`
	Checkout                    bool              `json:"checkout"`
	AllowedRepositories         []string          `json:"allowed_repositories"`
	CheckoutAuthorizationHeader string            `json:"checkout_authorization_header"`
	Command                     string            `json:"command"`
	Arguments                   []string          `json:"arguments"`
	Environment                 map[string]string `json:"environment"`
	RedactedEnvironmentKeys     []string          `json:"redacted_environment_keys"`
	TimeoutSeconds              int               `json:"timeout_seconds"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	return LoadConfigData(data)
}

func LoadConfigData(data []byte) (Config, error) {
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != nil {
		if !errors.Is(err, io.EOF) {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	} else {
		return Config{}, errors.New("parse config: config must contain one JSON object")
	}

	config, err := resolveSecrets(config)
	if err != nil {
		return Config{}, err
	}

	if err := config.Validate(); err != nil {
		return Config{}, err
	}

	return config, nil
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.ListenAddress) == "" {
		return errors.New("listen_address is required")
	}
	if !strings.HasPrefix(config.ListenAddress, "127.0.0.1:") && !strings.HasPrefix(config.ListenAddress, "localhost:") {
		return fmt.Errorf("listen_address must bind loopback, got %q", config.ListenAddress)
	}
	if strings.TrimSpace(config.MachineName) == "" {
		return errors.New("machine_name is required")
	}
	if err := requestmeta.ValidateMachineID(config.MachineID); err != nil {
		return err
	}
	if strings.TrimSpace(config.SharedToken) == "" {
		return errors.New("shared_token is required")
	}
	if err := validateHeaderValue(config.SharedToken, "shared_token"); err != nil {
		return err
	}
	if strings.TrimSpace(config.WorkspaceRoot) != "" && !filepath.IsAbs(config.WorkspaceRoot) {
		return errors.New("workspace_root must be an absolute path")
	}
	if len(config.Jobs) == 0 {
		return errors.New("at least one build job is required")
	}
	if err := validateHTTPURL(config.CIRegistrationURL, "ci_registration_url"); err != nil {
		return err
	}
	if err := validateHTTPURL(config.CIHeartbeatURL, "ci_heartbeat_url"); err != nil {
		return err
	}
	if err := validateHTTPURL(config.CIDeregistrationURL, "ci_deregistration_url"); err != nil {
		return err
	}
	if strings.TrimSpace(config.CIRegistrationURL) == "" && strings.TrimSpace(config.CIHeartbeatURL) != "" {
		return errors.New("ci_registration_url is required when ci_heartbeat_url is configured")
	}
	if strings.TrimSpace(config.CIRegistrationURL) == "" && strings.TrimSpace(config.CIDeregistrationURL) != "" {
		return errors.New("ci_registration_url is required when ci_deregistration_url is configured")
	}
	if strings.TrimSpace(config.CIRegistrationURL) != "" && strings.TrimSpace(config.CIDeregistrationURL) == "" {
		return errors.New("ci_deregistration_url is required when ci_registration_url is configured")
	}
	if config.hasCIRegistrationEndpoint() && strings.TrimSpace(config.RegistrationToken) == "" {
		return errors.New("registration_token is required when CI registration endpoints are configured")
	}
	if strings.TrimSpace(config.RegistrationToken) != "" {
		if err := validateHeaderValue(config.RegistrationToken, "registration_token"); err != nil {
			return err
		}
	}
	if (strings.TrimSpace(config.CIAccessClientID) == "") != (strings.TrimSpace(config.CIAccessClientSecret) == "") {
		return errors.New("ci_access_client_id and ci_access_client_secret must be provided together")
	}
	if strings.TrimSpace(config.CIAccessClientID) != "" {
		if err := validateHeaderValue(config.CIAccessClientID, "ci_access_client_id"); err != nil {
			return err
		}
		if err := validateHeaderValue(config.CIAccessClientSecret, "ci_access_client_secret"); err != nil {
			return err
		}
	}
	if (strings.TrimSpace(config.RunnerAccessClientID) == "") != (strings.TrimSpace(config.RunnerAccessClientSecret) == "") {
		return errors.New("runner_access_client_id and runner_access_client_secret must be provided together")
	}
	if strings.TrimSpace(config.RunnerAccessClientID) != "" {
		if err := validateHeaderValue(config.RunnerAccessClientID, "runner_access_client_id"); err != nil {
			return err
		}
		if err := validateHeaderValue(config.RunnerAccessClientSecret, "runner_access_client_secret"); err != nil {
			return err
		}
	}
	for _, origin := range config.AllowedReportOrigins {
		if err := validateHTTPOrigin(origin, "allowed_report_origins"); err != nil {
			return err
		}
	}
	if config.HeartbeatSeconds < 0 {
		return errors.New("heartbeat_seconds must not be negative")
	}

	seen := map[string]bool{}
	for _, job := range config.Jobs {
		if err := requestmeta.ValidateJobID(job.ID); err != nil {
			return err
		}
		if seen[job.ID] {
			return fmt.Errorf("duplicate job id %q", job.ID)
		}
		seen[job.ID] = true

		if !job.Checkout {
			if strings.TrimSpace(job.WorkingDirectory) == "" {
				return fmt.Errorf("job %q working_directory is required", job.ID)
			}
			if !filepath.IsAbs(job.WorkingDirectory) {
				return fmt.Errorf("job %q working_directory must be an absolute path", job.ID)
			}
		}
		if job.Checkout && len(job.AllowedRepositories) == 0 {
			return fmt.Errorf("job %q allowed_repositories is required when checkout is enabled", job.ID)
		}
		for _, repository := range job.AllowedRepositories {
			if strings.TrimSpace(repository) == "" {
				return fmt.Errorf("job %q allowed_repositories must not include empty values", job.ID)
			}
			if err := requestmeta.ValidateRepoURL(repository); err != nil {
				return fmt.Errorf("job %q allowed_repositories entry %q is invalid: %w", job.ID, repository, err)
			}
		}
		if strings.TrimSpace(job.CheckoutAuthorizationHeader) != "" {
			if !job.Checkout {
				return fmt.Errorf("job %q checkout_authorization_header requires checkout to be enabled", job.ID)
			}
			if err := headerutil.ValidateLine(job.CheckoutAuthorizationHeader, fmt.Sprintf("job %q checkout_authorization_header", job.ID)); err != nil {
				return err
			}
		}
		if strings.TrimSpace(job.Command) == "" {
			return fmt.Errorf("job %q command is required", job.ID)
		}
		if !filepath.IsAbs(job.Command) {
			return fmt.Errorf("job %q command must be an absolute executable path", job.ID)
		}
		if strings.ContainsAny(job.Command, " \t\n\r") {
			return fmt.Errorf("job %q command must not contain whitespace; put arguments in arguments", job.ID)
		}
		if strings.ContainsAny(job.Command, ";&|`$<>") {
			return fmt.Errorf("job %q command must be an executable path, not shell text", job.ID)
		}
		if isShellExecutable(job.Command) {
			return fmt.Errorf("job %q command must not invoke a shell directly", job.ID)
		}
		if job.TimeoutSeconds <= 0 {
			return fmt.Errorf("job %q timeout_seconds must be positive", job.ID)
		}
		for key := range job.Environment {
			if !validEnvironmentKey(key) {
				return fmt.Errorf("job %q environment key %q is invalid", job.ID, key)
			}
			if reservedEnvironmentKey(key) {
				return fmt.Errorf("job %q environment key %q uses reserved TRANSWARP_ prefix", job.ID, key)
			}
		}
		for _, key := range job.RedactedEnvironmentKeys {
			if !validEnvironmentKey(key) {
				return fmt.Errorf("job %q redacted_environment_keys entry %q is invalid", job.ID, key)
			}
			if reservedEnvironmentKey(key) {
				return fmt.Errorf("job %q redacted_environment_keys entry %q uses reserved TRANSWARP_ prefix", job.ID, key)
			}
		}
	}

	switch config.Tunnel.Mode {
	case "", "off", "quick", "named":
	default:
		return fmt.Errorf("unsupported tunnel mode %q", config.Tunnel.Mode)
	}
	if config.Tunnel.Mode == "named" && strings.TrimSpace(config.Tunnel.Token) == "" {
		return errors.New("tunnel token is required when tunnel mode is named")
	}
	if config.Tunnel.Mode == "named" && strings.TrimSpace(config.Tunnel.PublicURL) == "" {
		return errors.New("tunnel public_url is required when tunnel mode is named")
	}
	if config.Tunnel.Mode == "named" && strings.TrimSpace(config.Tunnel.PublicURL) != "" {
		if err := validateNamedTunnelPublicURL(config.Tunnel.PublicURL); err != nil {
			return err
		}
	}

	return nil
}

func (config Config) hasCIRegistrationEndpoint() bool {
	return strings.TrimSpace(config.CIRegistrationURL) != "" ||
		strings.TrimSpace(config.CIHeartbeatURL) != "" ||
		strings.TrimSpace(config.CIDeregistrationURL) != ""
}

func validEnvironmentKey(key string) bool {
	if key == "" {
		return false
	}
	for index, char := range key {
		if char == '_' || (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
			continue
		}
		if index > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}

func validateHeaderValue(value string, field string) error {
	return headerutil.ValidateValue(value, field)
}

func reservedEnvironmentKey(key string) bool {
	return strings.HasPrefix(key, "TRANSWARP_")
}

func isShellExecutable(path string) bool {
	switch filepath.Base(path) {
	case "sh", "bash", "zsh", "fish", "csh", "tcsh":
		return true
	default:
		return false
	}
}

func validateHTTPURL(rawURL string, field string) error {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", field, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s must include a host", field)
	}
	if parsed.Scheme == "http" && !endpoint.IsLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("%s must use https unless it targets local loopback", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must not include query or fragment", field)
	}
	return nil
}

func validateHTTPOrigin(rawURL string, field string) error {
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("%s must not include empty values", field)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", field, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s must include a host", field)
	}
	if parsed.Scheme == "http" && !endpoint.IsLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("%s must use https unless it targets local loopback", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s entries must not include credentials", field)
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s entries must be origins like https://ci.example.com", field)
	}
	return nil
}

func validateNamedTunnelPublicURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("tunnel public_url is invalid: %w", err)
	}
	if parsed.Scheme != "https" {
		return errors.New("tunnel public_url must use https")
	}
	if parsed.Host == "" {
		return errors.New("tunnel public_url must include a host")
	}
	if parsed.User != nil {
		return errors.New("tunnel public_url must not include credentials")
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("tunnel public_url must be a base URL like https://transwarp.example.com")
	}
	return nil
}

func (config Config) Job(id string) (JobConfig, bool) {
	for _, job := range config.Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return JobConfig{}, false
}

func (config Config) ValidateReportURL(reportURL string) error {
	if strings.TrimSpace(reportURL) == "" {
		return nil
	}

	report, err := url.Parse(reportURL)
	if err != nil {
		return fmt.Errorf("report_url is invalid: %w", err)
	}
	if report.Scheme != "http" && report.Scheme != "https" {
		return fmt.Errorf("report_url must use http or https")
	}
	if report.Host == "" {
		return fmt.Errorf("report_url must include a host")
	}
	if report.Scheme == "http" && !endpoint.IsLoopbackHost(report.Hostname()) {
		return fmt.Errorf("report_url must use https unless it targets local loopback")
	}
	if report.User != nil {
		return fmt.Errorf("report_url must not include credentials")
	}
	if report.RawQuery != "" || report.Fragment != "" {
		return fmt.Errorf("report_url must not include query or fragment")
	}

	for _, allowed := range config.allowedReportOrigins() {
		if sameURLOrigin(report, allowed) {
			return nil
		}
	}

	return fmt.Errorf("report_url origin %q is not allowed", report.Scheme+"://"+report.Host)
}

func (config Config) allowedReportOrigins() []string {
	origins := []string{}
	for _, allowed := range []string{config.CIRegistrationURL, config.CIHeartbeatURL, config.CIDeregistrationURL} {
		if strings.TrimSpace(allowed) != "" {
			origins = append(origins, allowed)
		}
	}
	origins = append(origins, config.AllowedReportOrigins...)
	return origins
}

func sameURLOrigin(report *url.URL, rawAllowed string) bool {
	if strings.TrimSpace(rawAllowed) == "" {
		return false
	}
	allowed, err := url.Parse(rawAllowed)
	if err != nil {
		return false
	}
	return strings.EqualFold(report.Scheme, allowed.Scheme) && strings.EqualFold(report.Host, allowed.Host)
}

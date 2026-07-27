package diagnose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charliewilco/transwarp/internal/endpoint"
	"github.com/charliewilco/transwarp/internal/headerutil"
	"github.com/charliewilco/transwarp/internal/requestmeta"
	"github.com/charliewilco/transwarp/internal/tunnelnet"
)

type Request struct {
	BaseURL            string
	CoordinatorURL     string
	Token              string
	CoordinatorToken   string
	AccessClientID     string
	AccessClientSecret string
	MachineID          string
	JobID              string
	MinCPUCount        int
	MinMemoryBytes     uint64
	MinXcodeVersion    string
	AllowHTTP          bool
	Timeout            time.Duration
	LookupHost         func(context.Context, string) ([]string, error)
}

type StatusResponse struct {
	MachineID        string             `json:"machine_id"`
	MachineName      string             `json:"machine_name"`
	TunnelMode       string             `json:"tunnel_mode"`
	Tunnel           TunnelStatus       `json:"tunnel"`
	Registration     RegistrationStatus `json:"registration"`
	Capabilities     Capabilities       `json:"capabilities"`
	PublicURL        string             `json:"public_url"`
	AcceptingBuilds  *bool              `json:"accepting_builds,omitempty"`
	ActiveBuilds     int                `json:"active_builds"`
	QueuedBuilds     int                `json:"queued_builds"`
	QueuedBuildLimit int                `json:"queued_build_limit"`
	Jobs             []string           `json:"jobs"`
}

type RegistrationStatus struct {
	Configured     bool       `json:"configured"`
	State          string     `json:"state"`
	LastAction     string     `json:"last_action"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	LastError      string     `json:"last_error"`
}

type TunnelStatus struct {
	Mode           string `json:"mode"`
	State          string `json:"state"`
	PublicURL      string `json:"public_url"`
	Connected      bool   `json:"connected"`
	Ready          bool   `json:"ready"`
	Error          string `json:"error"`
	ReadinessError string `json:"readiness_error"`
}

type CoordinatorTarget struct {
	MachineID        string       `json:"machine_id"`
	MachineName      string       `json:"machine_name"`
	Listen           string       `json:"listen"`
	PublicURL        string       `json:"public_url,omitempty"`
	Capabilities     Capabilities `json:"capabilities"`
	AcceptingBuilds  *bool        `json:"accepting_builds,omitempty"`
	ActiveBuilds     int          `json:"active_builds"`
	QueuedBuilds     int          `json:"queued_builds"`
	QueuedBuildLimit int          `json:"queued_build_limit,omitempty"`
	Jobs             []string     `json:"jobs"`
	LeaseExpiresAt   time.Time    `json:"lease_expires_at"`
}

type Capabilities struct {
	OS           string `json:"os"`
	OSVersion    string `json:"os_version,omitempty"`
	Architecture string `json:"architecture"`
	CPUBrand     string `json:"cpu_brand,omitempty"`
	CPUCount     int    `json:"cpu_count,omitempty"`
	MemoryBytes  uint64 `json:"memory_bytes,omitempty"`
	XcodeVersion string `json:"xcode_version,omitempty"`
	DeveloperDir string `json:"developer_dir,omitempty"`
}

func Run(ctx context.Context, client *http.Client, request Request, output io.Writer) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if client == nil {
		client = tunnelnet.NoRedirectHTTPClient()
	}
	if request.LookupHost == nil {
		request.LookupHost = tunnelnet.LookupHost
	}
	if request.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	if strings.TrimSpace(request.CoordinatorURL) != "" {
		return runCoordinator(ctx, client, request, output)
	}

	baseURL, err := url.Parse(request.BaseURL)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	if err := resolveBaseURL(ctx, request, baseURL, output); err != nil {
		return err
	}

	if err := checkHealth(ctx, client, request); err != nil {
		return err
	}
	fmt.Fprintln(output, "[ok] public health endpoint reachable")

	status, err := checkStatus(ctx, client, request)
	if err != nil {
		return err
	}
	if err := reportAndValidateRunnerStatus(request, baseURL, status, output); err != nil {
		return err
	}

	fmt.Fprintln(output, "diagnosis passed")
	return nil
}

func (request Request) Validate() error {
	if (strings.TrimSpace(request.AccessClientID) == "") != (strings.TrimSpace(request.AccessClientSecret) == "") {
		return errors.New("Cloudflare Access client ID and secret must be provided together")
	}
	if strings.TrimSpace(request.AccessClientID) != "" {
		if err := headerutil.ValidateValue(request.AccessClientID, "Cloudflare Access client ID"); err != nil {
			return err
		}
		if err := headerutil.ValidateValue(request.AccessClientSecret, "Cloudflare Access client secret"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(request.Token) != "" {
		if err := headerutil.ValidateValue(request.Token, "token"); err != nil {
			return err
		}
	}
	if request.MinCPUCount < 0 {
		return errors.New("min CPU count must not be negative")
	}
	if strings.TrimSpace(request.MachineID) != "" {
		if err := requestmeta.ValidateMachineID(request.MachineID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(request.JobID) != "" {
		if err := requestmeta.ValidateJobID(request.JobID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(request.CoordinatorURL) != "" {
		return request.validateCoordinator()
	}
	return request.validateRunner()
}

func (request Request) validateRunner() error {
	if strings.TrimSpace(request.BaseURL) == "" {
		return errors.New("url is required")
	}
	if strings.TrimSpace(request.Token) == "" {
		return errors.New("token is required")
	}
	return validateDiagnosticURL("url", request.BaseURL, request.AllowHTTP)
}

func (request Request) validateCoordinator() error {
	if strings.TrimSpace(request.CoordinatorToken) == "" {
		return errors.New("coordinator token is required")
	}
	if err := headerutil.ValidateValue(request.CoordinatorToken, "coordinator token"); err != nil {
		return err
	}
	return validateDiagnosticURL("coordinator URL", request.CoordinatorURL, request.AllowHTTP)
}

func validateDiagnosticURL(label string, value string, allowHTTP bool) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", label, err)
	}
	if parsed.Scheme != "https" && !(allowHTTP && parsed.Scheme == "http") {
		return fmt.Errorf("%s must use https; pass -allow-http only for local diagnostics", label)
	}
	return endpoint.ValidateBaseURL(value, label)
}

func runCoordinator(ctx context.Context, client *http.Client, request Request, output io.Writer) error {
	baseURL, err := url.Parse(request.CoordinatorURL)
	if err != nil {
		return fmt.Errorf("parse coordinator URL: %w", err)
	}
	if err := resolveBaseURL(ctx, request, baseURL, output); err != nil {
		return err
	}

	if err := checkCoordinatorHealth(ctx, client, request); err != nil {
		return err
	}
	fmt.Fprintln(output, "[ok] coordinator health endpoint reachable")

	targets, err := checkCoordinatorTargets(ctx, client, request)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "[ok] coordinator targets reachable (%d active target%s)\n", len(targets), plural(len(targets)))

	target, err := selectCoordinatorTarget(targets, request)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "[ok] target %s (%s) is available\n", target.MachineName, target.MachineID)
	fmt.Fprintf(output, "[ok] target load %s\n", loadSummary(target.ActiveBuilds, target.QueuedBuilds, target.QueuedBuildLimit))
	if err := reportAndValidateCapabilities("target", target.Capabilities, output); err != nil {
		return err
	}
	if strings.TrimSpace(request.JobID) != "" {
		fmt.Fprintf(output, "[ok] target advertises job %q\n", request.JobID)
	}
	if err := validateCoordinatorTargetPublicURL(target, request); err != nil {
		return err
	}
	if target.PublicURL != "" {
		fmt.Fprintf(output, "[ok] target public_url=%s\n", target.PublicURL)
	}
	if err := validateCoordinatorTargetLease(target, output); err != nil {
		return err
	}
	if strings.TrimSpace(request.Token) != "" {
		if err := probeCoordinatorTargetRunner(ctx, client, request, target, output); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(output, "[info] runner token not provided; skipping selected runner public_url probe")
	}

	fmt.Fprintln(output, "diagnosis passed")
	return nil
}

func resolveBaseURL(ctx context.Context, request Request, baseURL *url.URL, output io.Writer) error {
	host := baseURL.Hostname()
	addresses, err := request.LookupHost(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	fmt.Fprintf(output, "[ok] resolved %s (%d address%s)\n", host, len(addresses), plural(len(addresses)))
	return nil
}

func checkCoordinatorHealth(ctx context.Context, client *http.Client, request Request) error {
	httpRequest, err := request.newCoordinatorRequest(ctx, http.MethodGet, "/health", false)
	if err != nil {
		return err
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("coordinator health request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("coordinator health returned %s", response.Status)
	}
	return nil
}

func checkCoordinatorTargets(ctx context.Context, client *http.Client, request Request) ([]CoordinatorTarget, error) {
	httpRequest, err := request.newCoordinatorRequest(ctx, http.MethodGet, "/transwarp/targets", true)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("coordinator targets request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return nil, fmt.Errorf("coordinator targets returned %s: %s", response.Status, strings.TrimSpace(string(data)))
	}

	var targets []CoordinatorTarget
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("decode coordinator targets: %w", err)
	}
	return targets, nil
}

func selectCoordinatorTarget(targets []CoordinatorTarget, request Request) (CoordinatorTarget, error) {
	for _, target := range targets {
		if request.MachineID != "" && target.MachineID != request.MachineID {
			continue
		}
		if request.JobID != "" && !slices.Contains(target.Jobs, request.JobID) {
			continue
		}
		if targetConstraintError(target.Capabilities, request) != nil {
			continue
		}
		if !acceptingBuilds(target.AcceptingBuilds) {
			if request.MachineID != "" {
				return CoordinatorTarget{}, fmt.Errorf("target %q is unavailable: target is paused", request.MachineID)
			}
			continue
		}
		if err := targetLoadError(target.QueuedBuilds, target.QueuedBuildLimit); err != nil {
			if request.MachineID != "" {
				return CoordinatorTarget{}, fmt.Errorf("target %q is unavailable: %w", request.MachineID, err)
			}
			continue
		}
		return target, nil
	}
	if request.MachineID != "" && request.JobID != "" {
		return CoordinatorTarget{}, fmt.Errorf("target %q is unavailable or does not advertise job %q", request.MachineID, request.JobID)
	}
	if request.MachineID != "" {
		return CoordinatorTarget{}, fmt.Errorf("target %q is unavailable", request.MachineID)
	}
	if request.JobID != "" {
		return CoordinatorTarget{}, fmt.Errorf("no active target advertises job %q and satisfies constraints", request.JobID)
	}
	return CoordinatorTarget{}, errors.New("no active targets satisfy constraints")
}

func validateCoordinatorTargetPublicURL(target CoordinatorTarget, request Request) error {
	if strings.TrimSpace(target.PublicURL) == "" {
		if request.AllowHTTP {
			return nil
		}
		return fmt.Errorf("target %q is registered without public_url; runner tunnel is not ready", target.MachineID)
	}
	parsed, err := url.Parse(target.PublicURL)
	if err != nil {
		return fmt.Errorf("target public_url is invalid: %w", err)
	}
	if parsed.Scheme != "https" && !(request.AllowHTTP && parsed.Scheme == "http") {
		return fmt.Errorf("target public_url must use https; pass -allow-http only for local diagnostics")
	}
	return endpoint.ValidateBaseURL(target.PublicURL, "target public_url")
}

func probeCoordinatorTargetRunner(ctx context.Context, client *http.Client, request Request, target CoordinatorTarget, output io.Writer) error {
	if strings.TrimSpace(target.PublicURL) == "" {
		return fmt.Errorf("target %q has no public_url to probe", target.MachineID)
	}
	baseURL, err := url.Parse(target.PublicURL)
	if err != nil {
		return fmt.Errorf("parse target public_url: %w", err)
	}
	runnerRequest := request
	runnerRequest.BaseURL = target.PublicURL
	runnerRequest.CoordinatorURL = ""
	if err := resolveBaseURL(ctx, runnerRequest, baseURL, output); err != nil {
		return err
	}
	if err := checkHealth(ctx, client, runnerRequest); err != nil {
		return fmt.Errorf("selected runner health check failed: %w", err)
	}
	fmt.Fprintln(output, "[ok] selected runner health reachable through public_url")

	status, err := checkStatus(ctx, client, runnerRequest)
	if err != nil {
		return fmt.Errorf("selected runner status check failed: %w", err)
	}
	if strings.TrimSpace(status.MachineID) != "" && status.MachineID != target.MachineID {
		return fmt.Errorf("selected runner machine_id %q does not match coordinator target %q", status.MachineID, target.MachineID)
	}
	return reportAndValidateRunnerStatus(runnerRequest, baseURL, status, output)
}

func checkHealth(ctx context.Context, client *http.Client, request Request) error {
	httpRequest, err := request.newRequest(ctx, http.MethodGet, "/health", false)
	if err != nil {
		return err
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("health request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("health returned %s", response.Status)
	}
	return nil
}

func checkStatus(ctx context.Context, client *http.Client, request Request) (StatusResponse, error) {
	httpRequest, err := request.newRequest(ctx, http.MethodGet, "/status", true)
	if err != nil {
		return StatusResponse{}, err
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return StatusResponse{}, fmt.Errorf("status request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return StatusResponse{}, fmt.Errorf("status returned %s: %s", response.Status, strings.TrimSpace(string(data)))
	}

	var status StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return StatusResponse{}, fmt.Errorf("decode status: %w", err)
	}
	if strings.TrimSpace(status.MachineID) == "" {
		return StatusResponse{}, errors.New("status response is missing machine_id")
	}
	return status, nil
}

func reportAndValidateRunnerStatus(request Request, baseURL *url.URL, status StatusResponse, output io.Writer) error {
	fmt.Fprintf(output, "[ok] authenticated status reachable for %s (%s)\n", status.MachineName, status.MachineID)
	if strings.TrimSpace(request.MachineID) != "" && status.MachineID != request.MachineID {
		return fmt.Errorf("runner machine_id %q does not match requested machine_id %q", status.MachineID, request.MachineID)
	}
	fmt.Fprintf(output, "[ok] runner load %s\n", loadSummary(status.ActiveBuilds, status.QueuedBuilds, status.QueuedBuildLimit))
	if err := reportAndValidateCapabilities("runner", status.Capabilities, output); err != nil {
		return err
	}
	if err := targetConstraintError(status.Capabilities, request); err != nil {
		return fmt.Errorf("runner does not satisfy constraints: %w", err)
	}
	if err := targetLoadError(status.QueuedBuilds, status.QueuedBuildLimit); err != nil {
		return fmt.Errorf("runner is unavailable: %w", err)
	}
	if !acceptingBuilds(status.AcceptingBuilds) {
		return errors.New("runner is unavailable: runner is paused")
	}
	fmt.Fprintf(output, "[ok] tunnel mode=%s state=%s connected=%t ready=%t\n", status.Tunnel.Mode, status.Tunnel.State, status.Tunnel.Connected, status.Tunnel.Ready)
	if status.Registration.Configured {
		fmt.Fprintf(output, "[ok] registration state=%s action=%s\n", status.Registration.State, firstNonEmpty(status.Registration.LastAction, "none"))
		if status.Registration.LeaseExpiresAt != nil {
			fmt.Fprintf(output, "[ok] registration lease_expires_at=%s\n", status.Registration.LeaseExpiresAt.UTC().Format(time.RFC3339))
		}
		if status.Registration.LastError != "" {
			fmt.Fprintf(output, "[info] registration last_error=%s\n", status.Registration.LastError)
		}
	}
	if publicURL := firstNonEmpty(status.PublicURL, status.Tunnel.PublicURL); publicURL != "" {
		fmt.Fprintf(output, "[ok] runner public_url=%s\n", publicURL)
	}
	if status.Tunnel.ReadinessError != "" {
		fmt.Fprintf(output, "[info] tunnel readiness_error=%s\n", status.Tunnel.ReadinessError)
	}

	if status.Tunnel.Error != "" {
		return fmt.Errorf("runner reports tunnel error: %s (%s)", status.Tunnel.Error, tunnelDescription(status))
	}
	if status.Registration.Configured && (status.Registration.State == "failed" || status.Registration.State == "heartbeat_failed") {
		return fmt.Errorf("runner reports registration %s: %s", status.Registration.State, status.Registration.LastError)
	}
	if err := validateRunnerRegistrationLease(status); err != nil {
		return err
	}
	if status.Tunnel.Mode != "" && status.Tunnel.Mode != "off" {
		if !status.Tunnel.Connected {
			return fmt.Errorf("runner reports tunnel is not connected (%s)", tunnelDescription(status))
		}
		if !status.Tunnel.Ready {
			return fmt.Errorf("runner reports tunnel is connected but public endpoint is not ready (%s)", tunnelDescription(status))
		}
		if strings.TrimSpace(firstNonEmpty(status.PublicURL, status.Tunnel.PublicURL)) == "" {
			return fmt.Errorf("runner reports tunnel is connected but public_url is empty (%s)", tunnelDescription(status))
		}
	}
	if err := validatePublicURLHost(request, baseURL, status); err != nil {
		return err
	}
	if strings.TrimSpace(request.JobID) != "" {
		if !slices.Contains(status.Jobs, request.JobID) {
			return fmt.Errorf("job %q is not advertised by runner", request.JobID)
		}
		fmt.Fprintf(output, "[ok] job %q is advertised\n", request.JobID)
	}
	return nil
}

func targetLoadError(queuedBuilds int, queuedBuildLimit int) error {
	if queuedBuildLimit > 0 && queuedBuilds >= queuedBuildLimit {
		return fmt.Errorf("queue is full (%d/%d queued)", queuedBuilds, queuedBuildLimit)
	}
	return nil
}

func acceptingBuilds(value *bool) bool {
	return value == nil || *value
}

func loadSummary(activeBuilds int, queuedBuilds int, queuedBuildLimit int) string {
	if queuedBuildLimit > 0 {
		return fmt.Sprintf("active=%d queued=%d/%d", activeBuilds, queuedBuilds, queuedBuildLimit)
	}
	return fmt.Sprintf("active=%d queued=%d", activeBuilds, queuedBuilds)
}

func validateCoordinatorTargetLease(target CoordinatorTarget, output io.Writer) error {
	if target.LeaseExpiresAt.IsZero() {
		return fmt.Errorf("target %q is registered without lease_expires_at", target.MachineID)
	}
	if !target.LeaseExpiresAt.After(time.Now()) {
		return fmt.Errorf("target %q registration lease expired at %s", target.MachineID, target.LeaseExpiresAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(output, "[ok] target lease_expires_at=%s\n", target.LeaseExpiresAt.UTC().Format(time.RFC3339))
	return nil
}

func validateRunnerRegistrationLease(status StatusResponse) error {
	if !status.Registration.Configured || status.Registration.State != "registered" {
		return nil
	}
	if status.Registration.LeaseExpiresAt == nil {
		return fmt.Errorf("runner reports registration registered without lease_expires_at")
	}
	if !status.Registration.LeaseExpiresAt.After(time.Now()) {
		return fmt.Errorf("runner reports registration lease expired at %s", status.Registration.LeaseExpiresAt.UTC().Format(time.RFC3339))
	}
	return nil
}

func targetConstraintError(capabilities Capabilities, request Request) error {
	failures := []string{}
	if request.MinCPUCount > 0 && capabilities.CPUCount < request.MinCPUCount {
		failures = append(failures, fmt.Sprintf("cpu_count %d is below minimum %d", capabilities.CPUCount, request.MinCPUCount))
	}
	if request.MinMemoryBytes > 0 && capabilities.MemoryBytes < request.MinMemoryBytes {
		failures = append(failures, fmt.Sprintf("memory_bytes %d is below minimum %d", capabilities.MemoryBytes, request.MinMemoryBytes))
	}
	if strings.TrimSpace(request.MinXcodeVersion) != "" {
		ok, err := xcodeVersionAtLeast(capabilities.XcodeVersion, request.MinXcodeVersion)
		if err != nil {
			failures = append(failures, err.Error())
		} else if !ok {
			failures = append(failures, fmt.Sprintf("xcode_version %q is below minimum %q", capabilities.XcodeVersion, request.MinXcodeVersion))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

var dottedVersionPattern = regexp.MustCompile(`\d+(?:\.\d+)*`)

func xcodeVersionAtLeast(actual string, minimum string) (bool, error) {
	actualParts, err := parseDottedVersion(actual)
	if err != nil {
		return false, fmt.Errorf("xcode_version %q is unavailable or invalid", actual)
	}
	minimumParts, err := parseDottedVersion(minimum)
	if err != nil {
		return false, fmt.Errorf("minimum xcode version %q is invalid", minimum)
	}
	length := len(actualParts)
	if len(minimumParts) > length {
		length = len(minimumParts)
	}
	for index := 0; index < length; index++ {
		actualPart := versionPart(actualParts, index)
		minimumPart := versionPart(minimumParts, index)
		if actualPart > minimumPart {
			return true, nil
		}
		if actualPart < minimumPart {
			return false, nil
		}
	}
	return true, nil
}

func parseDottedVersion(value string) ([]int, error) {
	raw := dottedVersionPattern.FindString(value)
	if raw == "" {
		return nil, errors.New("missing dotted version")
	}
	fields := strings.Split(raw, ".")
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		part, err := strconv.Atoi(field)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func versionPart(parts []int, index int) int {
	if index >= len(parts) {
		return 0
	}
	return parts[index]
}

func reportAndValidateCapabilities(label string, capabilities Capabilities, output io.Writer) error {
	if capabilities.Empty() {
		return fmt.Errorf("%s capabilities are required; Transwarp targets modern Apple Silicon Macs", label)
	}
	fmt.Fprintf(output, "[ok] %s capabilities os=%s arch=%s", label, firstNonEmpty(capabilities.OS, "unknown"), firstNonEmpty(capabilities.Architecture, "unknown"))
	if capabilities.OSVersion != "" {
		fmt.Fprintf(output, " os_version=%s", capabilities.OSVersion)
	}
	if capabilities.XcodeVersion != "" {
		fmt.Fprintf(output, " xcode=%s", capabilities.XcodeVersion)
	}
	if capabilities.CPUBrand != "" {
		fmt.Fprintf(output, " cpu=%s", capabilities.CPUBrand)
	}
	if capabilities.CPUCount > 0 {
		fmt.Fprintf(output, " cores=%d", capabilities.CPUCount)
	}
	fmt.Fprintln(output)

	if capabilities.OS == "" {
		return fmt.Errorf("%s OS is required; Transwarp targets modern macOS desktops", label)
	}
	if !strings.EqualFold(capabilities.OS, "macOS") && !strings.EqualFold(capabilities.OS, "darwin") {
		return fmt.Errorf("%s OS %q is unsupported; Transwarp targets modern macOS desktops", label, capabilities.OS)
	}
	if capabilities.Architecture == "" {
		return fmt.Errorf("%s architecture is required; Transwarp targets Apple Silicon Macs", label)
	}
	if capabilities.Architecture != "arm64" {
		return fmt.Errorf("%s architecture %q is unsupported; Transwarp targets Apple Silicon Macs", label, capabilities.Architecture)
	}
	if capabilities.OSVersion == "" {
		return fmt.Errorf("%s macOS version is required; Transwarp requires macOS %d or newer", label, minimumSupportedMacOSMajor)
	}
	major, ok := macOSMajorVersion(capabilities.OSVersion)
	if !ok {
		return fmt.Errorf("%s macOS version %q could not be parsed; Transwarp requires macOS %d or newer", label, capabilities.OSVersion, minimumSupportedMacOSMajor)
	}
	if major < minimumSupportedMacOSMajor {
		return fmt.Errorf("%s macOS version %q is unsupported; Transwarp requires macOS %d or newer", label, capabilities.OSVersion, minimumSupportedMacOSMajor)
	}
	return nil
}

const minimumSupportedMacOSMajor = 14

func macOSMajorVersion(version string) (int, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return 0, false
	}
	majorText, _, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return 0, false
	}
	return major, true
}

func (capabilities Capabilities) Empty() bool {
	return strings.TrimSpace(capabilities.OS) == "" &&
		strings.TrimSpace(capabilities.OSVersion) == "" &&
		strings.TrimSpace(capabilities.Architecture) == "" &&
		strings.TrimSpace(capabilities.CPUBrand) == "" &&
		capabilities.CPUCount == 0 &&
		capabilities.MemoryBytes == 0 &&
		strings.TrimSpace(capabilities.XcodeVersion) == "" &&
		strings.TrimSpace(capabilities.DeveloperDir) == ""
}

func validatePublicURLHost(request Request, baseURL *url.URL, status StatusResponse) error {
	publicURL := firstNonEmpty(status.PublicURL, status.Tunnel.PublicURL)
	if strings.TrimSpace(publicURL) == "" {
		return nil
	}
	if err := validateDiagnosticURL("runner public_url", publicURL, request.AllowHTTP); err != nil {
		return err
	}
	if request.AllowHTTP {
		return nil
	}
	parsed, err := url.Parse(publicURL)
	if err != nil {
		return fmt.Errorf("runner public_url is invalid: %w", err)
	}
	if !strings.EqualFold(parsed.Hostname(), baseURL.Hostname()) {
		return fmt.Errorf("runner public_url host %q does not match diagnostic URL host %q", parsed.Hostname(), baseURL.Hostname())
	}
	return nil
}

func (request Request) newRequest(ctx context.Context, method string, path string, authorize bool) (*http.Request, error) {
	endpoint := strings.TrimRight(request.BaseURL, "/") + path
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if authorize {
		httpRequest.Header.Set("Authorization", "Bearer "+request.Token)
	}
	if request.AccessClientID != "" {
		httpRequest.Header.Set("CF-Access-Client-Id", request.AccessClientID)
		httpRequest.Header.Set("CF-Access-Client-Secret", request.AccessClientSecret)
	}
	return httpRequest, nil
}

func (request Request) newCoordinatorRequest(ctx context.Context, method string, path string, authorize bool) (*http.Request, error) {
	endpoint := strings.TrimRight(request.CoordinatorURL, "/") + path
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if authorize {
		httpRequest.Header.Set("Authorization", "Bearer "+request.CoordinatorToken)
	}
	if request.AccessClientID != "" {
		httpRequest.Header.Set("CF-Access-Client-Id", request.AccessClientID)
		httpRequest.Header.Set("CF-Access-Client-Secret", request.AccessClientSecret)
	}
	return httpRequest, nil
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "es"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func tunnelDescription(status StatusResponse) string {
	publicURL := firstNonEmpty(status.PublicURL, status.Tunnel.PublicURL)
	if publicURL == "" {
		publicURL = "<empty>"
	}
	description := fmt.Sprintf("mode=%s state=%s connected=%t ready=%t public_url=%q", status.Tunnel.Mode, status.Tunnel.State, status.Tunnel.Connected, status.Tunnel.Ready, publicURL)
	if status.Tunnel.ReadinessError != "" {
		description += fmt.Sprintf(" readiness_error=%q", status.Tunnel.ReadinessError)
	}
	return description
}

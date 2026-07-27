package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/charliewilco/transwarp/internal/agent"
	"github.com/charliewilco/transwarp/internal/configwriter"
)

type repeatedStrings []string

func (values *repeatedStrings) String() string {
	return strings.Join(*values, ",")
}

func (values *repeatedStrings) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type repeatedJobs []agent.JobConfig

func (jobs *repeatedJobs) String() string {
	return fmt.Sprintf("%d jobs", len(*jobs))
}

func (jobs *repeatedJobs) Set(value string) error {
	job, err := parseJobSpec(value)
	if err != nil {
		return err
	}
	*jobs = append(*jobs, job)
	return nil
}

func parseJobSpec(value string) (agent.JobConfig, error) {
	values, err := url.ParseQuery(value)
	if err != nil {
		return agent.JobConfig{}, fmt.Errorf("parse job: %w", err)
	}
	allowedKeys := map[string]bool{
		"id":                            true,
		"label":                         true,
		"working_directory":             true,
		"checkout":                      true,
		"allowed_repository":            true,
		"checkout_authorization_header": true,
		"command":                       true,
		"arg":                           true,
		"env":                           true,
		"redacted_environment_key":      true,
		"timeout_seconds":               true,
	}
	for key := range values {
		if !allowedKeys[key] {
			return agent.JobConfig{}, fmt.Errorf("parse job: unknown field %q", key)
		}
	}

	id, err := singleQueryValue(values, "id")
	if err != nil {
		return agent.JobConfig{}, err
	}
	label, err := singleQueryValue(values, "label")
	if err != nil {
		return agent.JobConfig{}, err
	}
	workingDirectory, err := singleQueryValue(values, "working_directory")
	if err != nil {
		return agent.JobConfig{}, err
	}
	checkoutValue, err := singleQueryValue(values, "checkout")
	if err != nil {
		return agent.JobConfig{}, err
	}
	checkoutAuthorizationHeader, err := singleQueryValue(values, "checkout_authorization_header")
	if err != nil {
		return agent.JobConfig{}, err
	}
	command, err := singleQueryValue(values, "command")
	if err != nil {
		return agent.JobConfig{}, err
	}
	timeoutValue, err := singleQueryValue(values, "timeout_seconds")
	if err != nil {
		return agent.JobConfig{}, err
	}

	job := agent.JobConfig{
		ID:                          id,
		Label:                       label,
		WorkingDirectory:            workingDirectory,
		Checkout:                    false,
		AllowedRepositories:         values["allowed_repository"],
		CheckoutAuthorizationHeader: checkoutAuthorizationHeader,
		Command:                     command,
		Arguments:                   values["arg"],
		Environment:                 map[string]string{},
		RedactedEnvironmentKeys:     values["redacted_environment_key"],
		TimeoutSeconds:              300,
	}
	if checkoutValue != "" {
		checkout, err := strconv.ParseBool(checkoutValue)
		if err != nil {
			return agent.JobConfig{}, fmt.Errorf("parse job checkout: %w", err)
		}
		job.Checkout = checkout
	}
	if timeoutValue != "" {
		timeout, err := strconv.Atoi(timeoutValue)
		if err != nil {
			return agent.JobConfig{}, fmt.Errorf("parse job timeout_seconds: %w", err)
		}
		job.TimeoutSeconds = timeout
	}
	for _, entry := range values["env"] {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return agent.JobConfig{}, fmt.Errorf("parse job env: expected KEY=VALUE, got %q", entry)
		}
		job.Environment[key] = value
	}
	return job, nil
}

func encodeJobSpec(job agent.JobConfig) string {
	values := url.Values{}
	values.Set("id", job.ID)
	values.Set("label", job.Label)
	values.Set("working_directory", job.WorkingDirectory)
	if job.Checkout {
		values.Set("checkout", strconv.FormatBool(job.Checkout))
	}
	for _, repository := range job.AllowedRepositories {
		values.Add("allowed_repository", repository)
	}
	values.Set("checkout_authorization_header", job.CheckoutAuthorizationHeader)
	values.Set("command", job.Command)
	for _, argument := range job.Arguments {
		values.Add("arg", argument)
	}
	for key, value := range job.Environment {
		values.Add("env", key+"="+value)
	}
	for _, key := range job.RedactedEnvironmentKeys {
		values.Add("redacted_environment_key", key)
	}
	values.Set("timeout_seconds", strconv.Itoa(job.TimeoutSeconds))
	return values.Encode()
}

func singleQueryValue(values url.Values, key string) (string, error) {
	entries := values[key]
	if len(entries) > 1 {
		return "", fmt.Errorf("parse job: field %q may only be provided once", key)
	}
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0], nil
}

func main() {
	var jobArgs repeatedStrings
	var jobs repeatedJobs
	printJobSpec := flag.Bool("print-job-spec", false, "print a URL-encoded -job spec from single-job flags")
	output := flag.String("output", "", "path to write agent JSON config")
	listen := flag.String("listen", "", "loopback listen address")
	machineID := flag.String("machine-id", "", "machine identifier")
	machineName := flag.String("machine-name", "", "machine display name")
	sharedToken := flag.String("shared-token", envAny("TRANSWARP_SHARED_TOKEN", "TRANSWARP_TOKEN"), "runner shared token")
	workspaceRoot := flag.String("workspace-root", "", "workspace root")
	preventSleep := flag.Bool("prevent-sleep", false, "prevent system sleep while running")
	heartbeatSeconds := flag.Int("heartbeat-seconds", 30, "heartbeat interval in seconds")
	ciRegistrationURL := flag.String("ci-registration-url", "", "CI registration endpoint")
	ciHeartbeatURL := flag.String("ci-heartbeat-url", "", "CI heartbeat endpoint")
	ciDeregistrationURL := flag.String("ci-deregistration-url", "", "CI deregistration endpoint")
	registrationToken := flag.String("registration-token", envAny("TRANSWARP_REGISTRATION_TOKEN"), "CI registration bearer token")
	ciAccessClientID := flag.String("ci-access-client-id", envAny("TRANSWARP_CI_ACCESS_CLIENT_ID"), "Cloudflare Access client ID for CI endpoints")
	ciAccessClientSecret := flag.String("ci-access-client-secret", envAny("TRANSWARP_CI_ACCESS_CLIENT_SECRET"), "Cloudflare Access client secret for CI endpoints")
	runnerAccessClientID := flag.String("runner-access-client-id", envAny("TRANSWARP_RUNNER_ACCESS_CLIENT_ID"), "Cloudflare Access client ID for runner endpoint")
	runnerAccessClientSecret := flag.String("runner-access-client-secret", envAny("TRANSWARP_RUNNER_ACCESS_CLIENT_SECRET"), "Cloudflare Access client secret for runner endpoint")
	tunnelMode := flag.String("tunnel-mode", "off", "tunnel mode: off, quick, or named")
	cloudflaredPath := flag.String("cloudflared-path", "", "cloudflared executable path or @bundle/cloudflared")
	tunnelToken := flag.String("tunnel-token", envAny("TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN", "TRANSWARP_TUNNEL_TOKEN"), "Cloudflare Tunnel token")
	tunnelName := flag.String("tunnel-name", "", "Cloudflare Tunnel name")
	publicURL := flag.String("public-url", "", "public runner URL")
	jobID := flag.String("job-id", "", "job identifier")
	jobLabel := flag.String("job-label", "", "job display label")
	jobWorkingDirectory := flag.String("job-working-directory", "", "job working directory")
	jobCommand := flag.String("job-command", "", "absolute job executable path")
	jobTimeoutSeconds := flag.Int("job-timeout-seconds", 300, "job timeout in seconds")
	flag.Var(&jobArgs, "job-arg", "job argument, repeatable")
	flag.Var(&jobs, "job", "job query spec, repeatable; fields include id, label, working_directory, command, arg, timeout_seconds")
	flag.Parse()

	singleJob := agent.JobConfig{
		ID:                      *jobID,
		Label:                   *jobLabel,
		WorkingDirectory:        *jobWorkingDirectory,
		Checkout:                false,
		AllowedRepositories:     []string{},
		Command:                 *jobCommand,
		Arguments:               []string(jobArgs),
		Environment:             map[string]string{},
		RedactedEnvironmentKeys: []string{},
		TimeoutSeconds:          *jobTimeoutSeconds,
	}
	if *printJobSpec {
		if len(jobs) > 0 {
			fmt.Fprintln(os.Stderr, "-print-job-spec cannot be combined with -job")
			os.Exit(2)
		}
		fmt.Println(encodeJobSpec(singleJob))
		return
	}

	config := agent.Config{
		ListenAddress:            *listen,
		MachineID:                *machineID,
		MachineName:              *machineName,
		SharedToken:              *sharedToken,
		WorkspaceRoot:            *workspaceRoot,
		PreventSleep:             *preventSleep,
		RedactedValues:           []string{},
		CIRegistrationURL:        *ciRegistrationURL,
		CIHeartbeatURL:           *ciHeartbeatURL,
		CIDeregistrationURL:      *ciDeregistrationURL,
		RegistrationToken:        *registrationToken,
		CIAccessClientID:         *ciAccessClientID,
		CIAccessClientSecret:     *ciAccessClientSecret,
		RunnerAccessClientID:     *runnerAccessClientID,
		RunnerAccessClientSecret: *runnerAccessClientSecret,
		AllowedReportOrigins:     []string{},
		HeartbeatSeconds:         *heartbeatSeconds,
		Tunnel: agent.TunnelConfig{
			Mode:            *tunnelMode,
			CloudflaredPath: *cloudflaredPath,
			Token:           *tunnelToken,
			Name:            *tunnelName,
			PublicURL:       *publicURL,
		},
	}
	if len(jobs) > 0 {
		if singleJobFlagsUsed(*jobID, *jobLabel, *jobWorkingDirectory, *jobCommand, *jobTimeoutSeconds, jobArgs) {
			fmt.Fprintln(os.Stderr, "-job cannot be combined with single-job flags")
			os.Exit(2)
		}
		config.Jobs = []agent.JobConfig(jobs)
	} else {
		config.Jobs = []agent.JobConfig{singleJob}
	}
	if err := configwriter.WriteAgentConfig(*output, config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func singleJobFlagsUsed(jobID, jobLabel, jobWorkingDirectory, jobCommand string, timeoutSeconds int, jobArgs repeatedStrings) bool {
	return jobID != "" ||
		jobLabel != "" ||
		jobWorkingDirectory != "" ||
		jobCommand != "" ||
		timeoutSeconds != 300 ||
		len(jobArgs) > 0
}

func envAny(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

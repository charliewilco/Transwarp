package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charliewilco/transwarp/internal/clienv"
	"github.com/charliewilco/transwarp/internal/dispatch"
)

func main() {
	request := dispatch.Request{}
	coordinatorURL := os.Getenv("TRANSWARP_COORDINATOR_URL")
	coordinatorToken := os.Getenv("TRANSWARP_COORDINATOR_TOKEN")
	machineID := os.Getenv("TRANSWARP_MACHINE_ID")
	minCPUCount, err := clienv.NonNegativeInt("TRANSWARP_MIN_CPU_COUNT", 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	minMemoryBytes, err := clienv.Uint64("TRANSWARP_MIN_MEMORY_BYTES", 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	minXcodeVersion := os.Getenv("TRANSWARP_MIN_XCODE_VERSION")
	requirePublicURL, err := clienv.Bool("TRANSWARP_REQUIRE_PUBLIC_URL", false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	timeout := flag.Duration("timeout", 0, "overall dispatch timeout, for example 30m")
	resultLookup := flag.Bool("result", false, "fetch a recorded coordinator result for -request-id")

	flag.StringVar(&request.BaseURL, "url", os.Getenv("TRANSWARP_URL"), "Transwarp public or local base URL")
	flag.StringVar(&request.Token, "token", os.Getenv("TRANSWARP_TOKEN"), "Transwarp bearer token")
	flag.StringVar(&coordinatorURL, "coordinator-url", coordinatorURL, "optional Transwarp coordinator base URL")
	flag.StringVar(&coordinatorToken, "coordinator-token", coordinatorToken, "Transwarp coordinator bearer token")
	flag.StringVar(&machineID, "machine-id", machineID, "optional registered machine ID when dispatching through the coordinator")
	flag.IntVar(&minCPUCount, "min-cpu-count", minCPUCount, "optional minimum target CPU core count for coordinator dispatch")
	flag.Uint64Var(&minMemoryBytes, "min-memory-bytes", minMemoryBytes, "optional minimum target memory bytes for coordinator dispatch")
	flag.StringVar(&minXcodeVersion, "min-xcode-version", minXcodeVersion, "optional minimum target Xcode version for coordinator dispatch")
	flag.BoolVar(&requirePublicURL, "require-public-url", requirePublicURL, "require coordinator dispatch to select a target with a public runner URL")
	flag.StringVar(&request.AccessClientID, "access-client-id", os.Getenv("TRANSWARP_ACCESS_CLIENT_ID"), "optional Cloudflare Access service token client ID")
	flag.StringVar(&request.AccessClientSecret, "access-client-secret", os.Getenv("TRANSWARP_ACCESS_CLIENT_SECRET"), "optional Cloudflare Access service token client secret")
	flag.StringVar(&request.JobID, "job", os.Getenv("TRANSWARP_JOB"), "configured Transwarp job ID")
	flag.StringVar(&request.RequestID, "request-id", os.Getenv("TRANSWARP_REQUEST_ID"), "stable CI request ID")
	flag.StringVar(&request.RepoURL, "repo-url", os.Getenv("TRANSWARP_REPO_URL"), "repository URL to build")
	flag.StringVar(&request.Ref, "ref", os.Getenv("TRANSWARP_REF"), "ref to check out")
	flag.StringVar(&request.Commit, "commit", os.Getenv("TRANSWARP_COMMIT"), "commit to check out")
	flag.StringVar(&request.ReportURL, "report-url", os.Getenv("TRANSWARP_REPORT_URL"), "optional CI result callback URL")
	flag.StringVar(&request.ReportToken, "report-token", os.Getenv("TRANSWARP_REPORT_TOKEN"), "optional CI result callback bearer token")
	flag.StringVar(&request.BuildID, "build-id", os.Getenv("TRANSWARP_BUILD_ID"), "existing Transwarp build ID to tail or cancel")
	flag.IntVar(&request.AfterSequence, "after", 0, "first log sequence to read after when tailing an existing build")
	flag.BoolVar(&request.Cancel, "cancel", false, "cancel the build identified by -build-id")
	flag.Parse()

	request.Timeout = time.Duration(*timeout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result := dispatch.RunResult{RequestID: request.RequestID, BuildID: request.BuildID}
	var resultStatusErr error
	coordinatorRequest := dispatch.CoordinatorRequest{
		BaseURL:            coordinatorURL,
		Token:              coordinatorToken,
		AccessClientID:     request.AccessClientID,
		AccessClientSecret: request.AccessClientSecret,
		MachineID:          machineID,
		JobID:              request.JobID,
		RequestID:          request.RequestID,
		RepoURL:            request.RepoURL,
		Ref:                request.Ref,
		Commit:             request.Commit,
		MinCPUCount:        minCPUCount,
		MinMemoryBytes:     minMemoryBytes,
		MinXcodeVersion:    minXcodeVersion,
		RequirePublicURL:   requirePublicURL,
		Timeout:            request.Timeout,
		Cancel:             request.Cancel,
	}
	if *resultLookup {
		if request.Cancel {
			err = fmt.Errorf("-result cannot be combined with -cancel")
		} else if coordinatorURL == "" {
			err = fmt.Errorf("-result requires -coordinator-url")
		} else {
			var coordinatorResult dispatch.CoordinatorBuildResult
			coordinatorResult, err = dispatch.GetCoordinatorResult(ctx, nil, coordinatorRequest)
			if err == nil {
				result = coordinatorResult.RunResult()
				resultStatusErr = coordinatorResult.StatusError()
			}
		}
	} else if coordinatorURL != "" {
		result, err = dispatch.RunCoordinatorWithResult(ctx, nil, coordinatorRequest, os.Stdout)
	} else {
		result, err = dispatch.RunWithResult(ctx, nil, request, os.Stdout)
	}
	if outputErr := writeGitHubOutputs(os.Getenv("GITHUB_OUTPUT"), result); outputErr != nil {
		if err == nil {
			err = outputErr
		} else {
			fmt.Fprintf(os.Stderr, "failed to write GitHub outputs: %v\n", outputErr)
		}
	}
	if shouldWriteResultSummary(result) {
		writeResultSummary(os.Stdout, result)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if resultStatusErr != nil {
		fmt.Fprintln(os.Stderr, resultStatusErr)
		os.Exit(1)
	}
}

func shouldWriteResultSummary(result dispatch.RunResult) bool {
	return result.BuildID != "" ||
		result.MachineID != "" ||
		result.PublicURL != "" ||
		result.RepoURL != "" ||
		result.Ref != "" ||
		result.Commit != "" ||
		result.Status != "" ||
		result.Error != ""
}

func writeGitHubOutputs(path string, result dispatch.RunResult) error {
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	if result.RequestID != "" {
		if err := writeGitHubOutputValue(file, "request-id", result.RequestID); err != nil {
			return err
		}
	}
	if result.BuildID != "" {
		if err := writeGitHubOutputValue(file, "build-id", result.BuildID); err != nil {
			return err
		}
	}
	if result.JobID != "" {
		if err := writeGitHubOutputValue(file, "job-id", result.JobID); err != nil {
			return err
		}
	}
	if result.MachineID != "" {
		if err := writeGitHubOutputValue(file, "machine-id", result.MachineID); err != nil {
			return err
		}
	}
	if result.PublicURL != "" {
		if err := writeGitHubOutputValue(file, "public-url", result.PublicURL); err != nil {
			return err
		}
	}
	if result.RepoURL != "" {
		if err := writeGitHubOutputValue(file, "repo-url", result.RepoURL); err != nil {
			return err
		}
	}
	if result.Ref != "" {
		if err := writeGitHubOutputValue(file, "ref", result.Ref); err != nil {
			return err
		}
	}
	if result.Commit != "" {
		if err := writeGitHubOutputValue(file, "commit", result.Commit); err != nil {
			return err
		}
	}
	if result.Status != "" {
		if err := writeGitHubOutputValue(file, "status", result.Status); err != nil {
			return err
		}
		if err := writeGitHubOutputValue(file, "exit-code", fmt.Sprintf("%d", result.ExitCode)); err != nil {
			return err
		}
	}
	if result.Error != "" {
		if err := writeGitHubOutputValue(file, "error", result.Error); err != nil {
			return err
		}
	}
	return nil
}

func writeResultSummary(output io.Writer, result dispatch.RunResult) {
	if result.RequestID != "" {
		fmt.Fprintf(output, "[result] request_id %s\n", result.RequestID)
	}
	if result.BuildID != "" {
		fmt.Fprintf(output, "[result] build_id %s\n", result.BuildID)
	}
	if result.JobID != "" {
		fmt.Fprintf(output, "[result] job_id %s\n", result.JobID)
	}
	if result.MachineID != "" {
		fmt.Fprintf(output, "[result] machine_id %s\n", result.MachineID)
	}
	if result.PublicURL != "" {
		fmt.Fprintf(output, "[result] public_url %s\n", result.PublicURL)
	}
	if result.RepoURL != "" {
		fmt.Fprintf(output, "[result] repo_url %s\n", result.RepoURL)
	}
	if result.Ref != "" {
		fmt.Fprintf(output, "[result] ref %s\n", result.Ref)
	}
	if result.Commit != "" {
		fmt.Fprintf(output, "[result] commit %s\n", result.Commit)
	}
	if result.Status != "" {
		fmt.Fprintf(output, "[result] status %s\n", result.Status)
		fmt.Fprintf(output, "[result] exit_code %d\n", result.ExitCode)
	}
	if result.Error != "" {
		fmt.Fprintf(output, "[result] error %s\n", result.Error)
	}
}

func writeGitHubOutputValue(writer io.Writer, name string, value string) error {
	delimiter := "TRANSWARP_OUTPUT"
	wrapped := "\n" + value + "\n"
	for strings.Contains(wrapped, "\n"+delimiter+"\n") {
		delimiter += "_END"
	}
	_, err := fmt.Fprintf(writer, "%s<<%s\n%s\n%s\n", name, delimiter, value, delimiter)
	return err
}

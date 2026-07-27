package dispatch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/charliewilco/transwarp/internal/endpoint"
	"github.com/charliewilco/transwarp/internal/headerutil"
	"github.com/charliewilco/transwarp/internal/requestmeta"
	"github.com/charliewilco/transwarp/internal/tunnelnet"
)

type Request struct {
	BaseURL            string
	Token              string
	AccessClientID     string
	AccessClientSecret string
	JobID              string
	RequestID          string
	RepoURL            string
	Ref                string
	Commit             string
	ReportURL          string
	ReportToken        string
	BuildID            string
	AfterSequence      int
	Cancel             bool
	Timeout            time.Duration
}

type CoordinatorRequest struct {
	BaseURL            string
	Token              string
	AccessClientID     string
	AccessClientSecret string
	MachineID          string
	JobID              string
	RequestID          string
	RepoURL            string
	Ref                string
	Commit             string
	MinCPUCount        int
	MinMemoryBytes     uint64
	MinXcodeVersion    string
	Timeout            time.Duration
	Cancel             bool
}

type BuildStartResponse struct {
	BuildID string `json:"build_id"`
	Status  string `json:"status"`
}

type RunResult struct {
	RequestID string
	BuildID   string
	JobID     string
	MachineID string
	PublicURL string
}

type coordinatorStreamResult struct {
	BuildID   string
	JobID     string
	MachineID string
	PublicURL string
}

var dottedVersionPattern = regexp.MustCompile(`\d+(?:\.\d+)*`)
var dispatchRetryDelays = []time.Duration{
	250 * time.Millisecond,
	750 * time.Millisecond,
	1500 * time.Millisecond,
}

type StartError struct {
	StatusCode int
	Status     string
	Body       string
}

func (err *StartError) Error() string {
	return fmt.Sprintf("dispatch failed with %s: %s", err.Status, err.Body)
}

type Event struct {
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	BuildID   string    `json:"build_id,omitempty"`
	JobID     string    `json:"job_id,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	MachineID string    `json:"machine_id,omitempty"`
	PublicURL string    `json:"public_url,omitempty"`
	Sequence  int       `json:"sequence,omitempty"`
	Time      time.Time `json:"time"`
}

func Run(ctx context.Context, client *http.Client, request Request, output io.Writer) error {
	_, err := RunWithResult(ctx, client, request, output)
	return err
}

func RunWithResult(ctx context.Context, client *http.Client, request Request, output io.Writer) (RunResult, error) {
	result := RunResult{RequestID: request.RequestID, BuildID: request.BuildID, JobID: request.JobID}
	if request.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	if request.Cancel {
		return result, Cancel(ctx, client, request)
	}
	if request.BuildID != "" && request.JobID == "" {
		return result, Tail(ctx, client, request, output)
	}
	started, err := Start(ctx, client, request)
	if err != nil {
		return result, err
	}

	request.BuildID = started.BuildID
	result.BuildID = started.BuildID
	return result, Tail(ctx, client, request, output)
}

func Start(ctx context.Context, client *http.Client, request Request) (BuildStartResponse, error) {
	if err := request.ValidateStart(); err != nil {
		return BuildStartResponse{}, err
	}
	if client == nil {
		client = tunnelnet.NoRedirectHTTPClient()
	}

	body, err := json.Marshal(map[string]string{
		"job_id":       request.JobID,
		"request_id":   request.RequestID,
		"repo_url":     request.RepoURL,
		"ref":          request.Ref,
		"commit":       request.Commit,
		"report_url":   request.ReportURL,
		"report_token": request.ReportToken,
	})
	if err != nil {
		return BuildStartResponse{}, err
	}

	response, err := doDispatchRequest(ctx, client, request.BaseURL, func(ctx context.Context) (*http.Request, error) {
		httpRequest, err := request.newRequest(ctx, http.MethodPost, "/v1/builds", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		return httpRequest, nil
	})
	if err != nil {
		return BuildStartResponse{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return BuildStartResponse{}, &StartError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Body:       strings.TrimSpace(string(data)),
		}
	}

	var started BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		return BuildStartResponse{}, err
	}
	if err := requestmeta.ValidateBuildID(started.BuildID); err != nil {
		if strings.TrimSpace(started.BuildID) == "" {
			return BuildStartResponse{}, errors.New("dispatch response did not include build_id")
		}
		return BuildStartResponse{}, err
	}
	return started, nil
}

func RunCoordinator(ctx context.Context, client *http.Client, request CoordinatorRequest, output io.Writer) error {
	_, err := RunCoordinatorWithResult(ctx, client, request, output)
	return err
}

func RunCoordinatorWithResult(ctx context.Context, client *http.Client, request CoordinatorRequest, output io.Writer) (RunResult, error) {
	result := RunResult{RequestID: request.RequestID, JobID: request.JobID}
	if request.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	if request.Cancel {
		return result, CancelCoordinator(ctx, client, request)
	}
	if err := request.Validate(); err != nil {
		return result, err
	}
	if client == nil {
		client = tunnelnet.NoRedirectHTTPClient()
	}

	body, err := json.Marshal(struct {
		MachineID       string `json:"machine_id,omitempty"`
		JobID           string `json:"job_id"`
		RequestID       string `json:"request_id"`
		RepoURL         string `json:"repo_url,omitempty"`
		Ref             string `json:"ref,omitempty"`
		Commit          string `json:"commit,omitempty"`
		MinCPUCount     int    `json:"min_cpu_count,omitempty"`
		MinMemoryBytes  uint64 `json:"min_memory_bytes,omitempty"`
		MinXcodeVersion string `json:"min_xcode_version,omitempty"`
	}{
		MachineID:       request.MachineID,
		JobID:           request.JobID,
		RequestID:       request.RequestID,
		RepoURL:         request.RepoURL,
		Ref:             request.Ref,
		Commit:          request.Commit,
		MinCPUCount:     request.MinCPUCount,
		MinMemoryBytes:  request.MinMemoryBytes,
		MinXcodeVersion: request.MinXcodeVersion,
	})
	if err != nil {
		return result, err
	}

	response, err := doDispatchRequest(ctx, client, request.BaseURL, func(ctx context.Context) (*http.Request, error) {
		httpRequest, err := request.newRequest(ctx, http.MethodPost, "/transwarp/dispatch", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		return httpRequest, nil
	})
	if err != nil {
		return result, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return result, fmt.Errorf("coordinator dispatch failed with %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	streamResult, err := readCoordinatorStream(response.Body, output, request)
	if streamResult.BuildID != "" {
		result.BuildID = streamResult.BuildID
	}
	if streamResult.JobID != "" {
		result.JobID = streamResult.JobID
	}
	if streamResult.MachineID != "" {
		result.MachineID = streamResult.MachineID
	}
	if streamResult.PublicURL != "" {
		result.PublicURL = streamResult.PublicURL
	}
	if err != nil {
		if ctx.Err() != nil {
			_ = CancelCoordinator(context.Background(), client, request)
			return result, ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || streamContextEnded(err) {
			_ = CancelCoordinator(context.Background(), client, request)
		}
		return result, err
	}
	return result, nil
}

func streamContextEnded(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "context canceled") || strings.Contains(message, "context deadline exceeded")
}

func Tail(ctx context.Context, client *http.Client, request Request, output io.Writer) error {
	if err := request.ValidateBuildID(); err != nil {
		return err
	}
	if client == nil {
		client = tunnelnet.NoRedirectHTTPClient()
	}

	after := request.AfterSequence
	for {
		path := fmt.Sprintf("/v1/builds/%s/logs?after=%d&follow=true", url.PathEscape(request.BuildID), after)
		response, err := doDispatchRequest(ctx, client, request.BaseURL, func(ctx context.Context) (*http.Request, error) {
			return request.newRequest(ctx, http.MethodGet, path, nil)
		})
		if err != nil {
			if ctx.Err() != nil {
				_ = Cancel(context.Background(), client, request)
				return ctx.Err()
			}
			return err
		}

		status, lastSequence, err := readEvents(response.Body, output)
		response.Body.Close()
		if lastSequence > after {
			after = lastSequence
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("tail failed with %s", response.Status)
		}
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			_ = Cancel(context.Background(), client, request)
			return ctx.Err()
		}
		if !errors.Is(err, errMissingTerminalStatus) {
			return err
		}
		if status != "" {
			return statusError(status)
		}
	}
}

func Cancel(ctx context.Context, client *http.Client, request Request) error {
	if err := request.ValidateBuildID(); err != nil {
		return err
	}
	if client == nil {
		client = tunnelnet.NoRedirectHTTPClient()
	}

	response, err := doDispatchRequest(ctx, client, request.BaseURL, func(ctx context.Context) (*http.Request, error) {
		return request.newRequest(ctx, http.MethodPost, fmt.Sprintf("/v1/builds/%s/cancel", url.PathEscape(request.BuildID)), nil)
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("cancel failed with %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

func CancelCoordinator(ctx context.Context, client *http.Client, request CoordinatorRequest) error {
	if err := request.ValidateCancel(); err != nil {
		return err
	}
	if client == nil {
		client = tunnelnet.NoRedirectHTTPClient()
	}

	response, err := doDispatchRequest(ctx, client, request.BaseURL, func(ctx context.Context) (*http.Request, error) {
		return request.newRequest(ctx, http.MethodPost, fmt.Sprintf("/transwarp/dispatches/%s/cancel", url.PathEscape(request.RequestID)), nil)
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("coordinator cancel failed with %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

func (request Request) ValidateStart() error {
	if strings.TrimSpace(request.BaseURL) == "" {
		return errors.New("base URL is required")
	}
	if err := endpoint.ValidateBaseURL(request.BaseURL, "base URL"); err != nil {
		return err
	}
	if strings.TrimSpace(request.Token) == "" {
		return errors.New("token is required")
	}
	if err := headerutil.ValidateValue(request.Token, "token"); err != nil {
		return err
	}
	if err := requestmeta.ValidateJobID(request.JobID); err != nil {
		return err
	}
	if strings.TrimSpace(request.RequestID) == "" {
		return errors.New("request ID is required")
	}
	if err := requestmeta.Validate(request.RequestID, request.RepoURL, request.Ref, request.Commit); err != nil {
		return err
	}
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
	if (strings.TrimSpace(request.ReportURL) == "") != (strings.TrimSpace(request.ReportToken) == "") {
		return errors.New("report URL and report token must be provided together")
	}
	if strings.TrimSpace(request.ReportToken) != "" {
		if err := headerutil.ValidateValue(request.ReportToken, "report token"); err != nil {
			return err
		}
	}
	if err := validateReportURL(request.ReportURL); err != nil {
		return err
	}
	return nil
}

func validateReportURL(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	if strings.Contains(rawURL, "#") {
		return errors.New("report URL must not include query or fragment")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("report URL is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("report URL must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("report URL must include a host")
	}
	if parsed.Scheme == "http" && !endpoint.IsLoopbackHost(parsed.Hostname()) {
		return errors.New("report URL must use https unless it targets local loopback")
	}
	if parsed.User != nil {
		return errors.New("report URL must not include credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("report URL must not include query or fragment")
	}
	return nil
}

func (request Request) ValidateBuildID() error {
	if strings.TrimSpace(request.BaseURL) == "" {
		return errors.New("base URL is required")
	}
	if err := endpoint.ValidateBaseURL(request.BaseURL, "base URL"); err != nil {
		return err
	}
	if strings.TrimSpace(request.Token) == "" {
		return errors.New("token is required")
	}
	if err := headerutil.ValidateValue(request.Token, "token"); err != nil {
		return err
	}
	if err := requestmeta.ValidateBuildID(request.BuildID); err != nil {
		return err
	}
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
	return nil
}

func (request Request) newRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error) {
	endpoint := strings.TrimRight(request.BaseURL, "/") + path
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+request.Token)
	if request.AccessClientID != "" {
		httpRequest.Header.Set("CF-Access-Client-Id", request.AccessClientID)
		httpRequest.Header.Set("CF-Access-Client-Secret", request.AccessClientSecret)
	}
	return httpRequest, nil
}

func (request CoordinatorRequest) Validate() error {
	if strings.TrimSpace(request.BaseURL) == "" {
		return errors.New("coordinator URL is required")
	}
	if err := endpoint.ValidateBaseURL(request.BaseURL, "coordinator URL"); err != nil {
		return err
	}
	if strings.TrimSpace(request.Token) == "" {
		return errors.New("coordinator token is required")
	}
	if err := headerutil.ValidateValue(request.Token, "coordinator token"); err != nil {
		return err
	}
	if err := requestmeta.ValidateJobID(request.JobID); err != nil {
		return err
	}
	if strings.TrimSpace(request.RequestID) == "" {
		return errors.New("request ID is required")
	}
	if request.MinCPUCount < 0 {
		return errors.New("min CPU count must not be negative")
	}
	if strings.TrimSpace(request.MinXcodeVersion) != "" && !dottedVersionPattern.MatchString(request.MinXcodeVersion) {
		return fmt.Errorf("min Xcode version %q is invalid", request.MinXcodeVersion)
	}
	if strings.TrimSpace(request.MachineID) != "" {
		if err := requestmeta.ValidateMachineID(request.MachineID); err != nil {
			return err
		}
	}
	if err := requestmeta.Validate(request.RequestID, request.RepoURL, request.Ref, request.Commit); err != nil {
		return err
	}
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
	return nil
}

func (request CoordinatorRequest) ValidateCancel() error {
	if strings.TrimSpace(request.BaseURL) == "" {
		return errors.New("coordinator URL is required")
	}
	if err := endpoint.ValidateBaseURL(request.BaseURL, "coordinator URL"); err != nil {
		return err
	}
	if strings.TrimSpace(request.Token) == "" {
		return errors.New("coordinator token is required")
	}
	if err := headerutil.ValidateValue(request.Token, "coordinator token"); err != nil {
		return err
	}
	if strings.TrimSpace(request.RequestID) == "" {
		return errors.New("request ID is required")
	}
	if err := requestmeta.ValidateRequestID(request.RequestID); err != nil {
		return err
	}
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
	return nil
}

func (request CoordinatorRequest) newRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error) {
	endpoint := strings.TrimRight(request.BaseURL, "/") + path
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+request.Token)
	if request.AccessClientID != "" {
		httpRequest.Header.Set("CF-Access-Client-Id", request.AccessClientID)
		httpRequest.Header.Set("CF-Access-Client-Secret", request.AccessClientSecret)
	}
	return httpRequest, nil
}

func doDispatchRequest(ctx context.Context, client *http.Client, baseURL string, makeRequest func(context.Context) (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= len(dispatchRetryDelays); attempt++ {
		httpRequest, err := makeRequest(ctx)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(httpRequest)
		if err == nil {
			if attempt == len(dispatchRetryDelays) || !shouldRetryDispatchResponse(baseURL, response.StatusCode) {
				return response, nil
			}
			response.Body.Close()
			if err := sleepDispatchRetry(ctx, dispatchRetryDelays[attempt]); err != nil {
				return nil, err
			}
			continue
		}
		lastErr = err
		if ctx.Err() != nil || attempt == len(dispatchRetryDelays) || !shouldRetryDispatchAttempt(baseURL, err) {
			return nil, err
		}
		if err := sleepDispatchRetry(ctx, dispatchRetryDelays[attempt]); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func shouldRetryDispatchAttempt(baseURL string, err error) bool {
	if !isPublicHTTPSBaseURL(baseURL) {
		return false
	}
	return shouldRetryDispatchError(err)
}

func shouldRetryDispatchResponse(baseURL string, statusCode int) bool {
	return isPublicHTTPSBaseURL(baseURL) && shouldRetryDispatchStatus(statusCode)
}

func shouldRetryDispatchStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func shouldRetryDispatchError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func isPublicHTTPSBaseURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && parsed.Hostname() != "" && !endpoint.IsLoopbackHost(parsed.Hostname())
}

func sleepDispatchRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var errMissingTerminalStatus = errors.New("build stream ended without terminal status")

func readEvents(reader io.Reader, output io.Writer) (string, int, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	terminalStatus := ""
	reportFailure := ""
	lastSequence := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			fmt.Fprintln(output, string(line))
			continue
		}

		writeEvent(output, event)
		if event.Sequence > lastSequence {
			lastSequence = event.Sequence
		}
		if event.Kind == "build" {
			switch {
			case event.Message == "passed":
				terminalStatus = "passed"
			case event.Message == "canceled":
				terminalStatus = "canceled"
			case strings.HasPrefix(event.Message, "failed"):
				terminalStatus = event.Message
			}
		}
		if event.Kind == "error" {
			if message, ok := strings.CutPrefix(event.Message, "report failed: "); ok {
				reportFailure = message
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return terminalStatus, lastSequence, err
	}

	if reportFailure != "" {
		return terminalStatus, lastSequence, fmt.Errorf("report failed: %s", reportFailure)
	}
	if terminalStatus == "" {
		return "", lastSequence, errMissingTerminalStatus
	}
	if terminalStatus != "passed" {
		return terminalStatus, lastSequence, statusError(terminalStatus)
	}
	return terminalStatus, lastSequence, nil
}

func statusError(status string) error {
	return errors.New(status)
}

func writeEvent(output io.Writer, event Event) {
	switch event.Kind {
	case "log":
		fmt.Fprintln(output, event.Message)
	case "build", "error":
		fmt.Fprintf(output, "[%s] %s\n", event.Kind, event.Message)
	default:
		fmt.Fprintf(output, "[%s] %s\n", event.Kind, event.Message)
	}
}

func readCoordinatorStream(reader io.Reader, output io.Writer, request CoordinatorRequest) (coordinatorStreamResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	result := coordinatorStreamResult{}
	var dispatchFailure string
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(output, line)
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err == nil &&
			event.Kind == "coordinator" &&
			event.Message == "accepted runner build" &&
			event.BuildID != "" {
			if err := validateAcceptedBuildEvent(event, request); err != nil {
				return result, err
			}
			result.BuildID = event.BuildID
			result.JobID = event.JobID
			result.MachineID = event.MachineID
			result.PublicURL = event.PublicURL
		}
		if message, ok := strings.CutPrefix(line, "dispatch failed: "); ok {
			dispatchFailure = message
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if dispatchFailure != "" {
		return result, errors.New(dispatchFailure)
	}
	if result.BuildID == "" {
		return result, errors.New("coordinator stream ended without accepted runner build metadata")
	}
	return result, nil
}

func validateAcceptedBuildEvent(event Event, request CoordinatorRequest) error {
	if err := requestmeta.ValidateBuildID(event.BuildID); err != nil {
		return err
	}
	if strings.TrimSpace(event.JobID) == "" {
		return errors.New("coordinator accepted-build metadata missing job_id")
	}
	if err := requestmeta.ValidateJobID(event.JobID); err != nil {
		return err
	}
	if event.JobID != request.JobID {
		return fmt.Errorf("coordinator accepted-build metadata job_id %q does not match requested job_id %q", event.JobID, request.JobID)
	}
	if strings.TrimSpace(event.RequestID) == "" {
		return errors.New("coordinator accepted-build metadata missing request_id")
	}
	if err := requestmeta.ValidateRequestID(event.RequestID); err != nil {
		return err
	}
	if event.RequestID != request.RequestID {
		return fmt.Errorf("coordinator accepted-build metadata request_id %q does not match requested request_id %q", event.RequestID, request.RequestID)
	}
	if strings.TrimSpace(event.MachineID) == "" {
		return errors.New("coordinator accepted-build metadata missing machine_id")
	}
	if err := requestmeta.ValidateMachineID(event.MachineID); err != nil {
		return err
	}
	if request.MachineID != "" && event.MachineID != request.MachineID {
		return fmt.Errorf("coordinator accepted-build metadata machine_id %q does not match requested machine_id %q", event.MachineID, request.MachineID)
	}
	if strings.TrimSpace(event.PublicURL) == "" {
		return errors.New("coordinator accepted-build metadata missing public_url")
	}
	if err := endpoint.ValidateBaseURL(event.PublicURL, "public_url"); err != nil {
		return err
	}
	return nil
}

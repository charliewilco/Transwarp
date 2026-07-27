package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charliewilco/transwarp/internal/endpoint"
	"github.com/charliewilco/transwarp/internal/requestmeta"
)

type BuildRequest struct {
	JobID       string `json:"job_id"`
	RequestID   string `json:"request_id"`
	RepoURL     string `json:"repo_url,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Commit      string `json:"commit,omitempty"`
	ReportURL   string `json:"report_url,omitempty"`
	ReportToken string `json:"report_token,omitempty"`
}

type LedgerRecord struct {
	BuildID      string    `json:"build_id"`
	JobID        string    `json:"job_id"`
	RequestID    string    `json:"request_id"`
	RepoURL      string    `json:"repo_url,omitempty"`
	Ref          string    `json:"ref,omitempty"`
	Commit       string    `json:"commit,omitempty"`
	Workspace    string    `json:"workspace,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	ExitCode     int       `json:"exit_code"`
	Error        string    `json:"error,omitempty"`
	ReportStatus string    `json:"report_status,omitempty"`
	ReportError  string    `json:"report_error,omitempty"`
}

type BuildReport struct {
	BuildID    string    `json:"build_id"`
	JobID      string    `json:"job_id"`
	RequestID  string    `json:"request_id"`
	MachineID  string    `json:"machine_id"`
	Machine    string    `json:"machine"`
	RepoURL    string    `json:"repo_url,omitempty"`
	Ref        string    `json:"ref,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	ExitCode   int       `json:"exit_code"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	PublicURL  string    `json:"public_url,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

type BuildStartResponse struct {
	BuildID   string `json:"build_id"`
	Status    string `json:"status"`
	LogsURL   string `json:"logs_url"`
	CancelURL string `json:"cancel_url"`
}

type BuildStatusResponse struct {
	BuildID      string             `json:"build_id"`
	JobID        string             `json:"job_id"`
	RequestID    string             `json:"request_id,omitempty"`
	Status       string             `json:"status"`
	CreatedAt    time.Time          `json:"created_at"`
	ReportStatus string             `json:"report_status,omitempty"`
	ReportError  string             `json:"report_error,omitempty"`
	Result       *BuildStatusResult `json:"result,omitempty"`
}

type BuildStatusResult struct {
	BuildID   string    `json:"build_id"`
	JobID     string    `json:"job_id"`
	RequestID string    `json:"request_id,omitempty"`
	RepoURL   string    `json:"repo_url,omitempty"`
	Ref       string    `json:"ref,omitempty"`
	Commit    string    `json:"commit,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	ExitCode  int       `json:"exit_code"`
	Error     string    `json:"error,omitempty"`
}

var reportRetryDelays = []time.Duration{1 * time.Second, 3 * time.Second, 5 * time.Second}

var maxRetainedBuildEvents = 5000

var buildStreamKeepaliveInterval = 30 * time.Second

var maxQueuedBuilds = 25

var errNotAcceptingBuilds = errors.New("runner is paused and not accepting new builds")

type BuildRun struct {
	ID        string
	Request   BuildRequest
	Job       JobConfig
	Stream    *buildStream
	CreatedAt time.Time

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	closed chan struct{}

	mu      sync.Mutex
	record  LedgerRecord
	started bool

	reportStatus string
	reportError  string
}

func (agent *Agent) startBuild(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(response, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var buildRequest BuildRequest
	if err := decodeBuildRequest(http.MaxBytesReader(response, request.Body, 64*1024), &buildRequest); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid build request"})
		return
	}
	if err := buildRequest.Validate(); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := agent.config.ValidateReportURL(buildRequest.ReportURL); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	job, ok := agent.config.Job(buildRequest.JobID)
	if !ok {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "unknown job_id"})
		return
	}
	if err := validateBuildRequestForJob(buildRequest, job); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	run, err := agent.startBuildRun(buildRequest, job)
	if err != nil {
		if errors.Is(err, errNotAcceptingBuilds) {
			writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(response, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	response.Header().Set("Transwarp-Build-ID", run.ID)
	response.Header().Set("Location", "/v1/builds/"+run.ID)
	writeJSON(response, http.StatusAccepted, run.StartResponse())
}

func decodeBuildRequest(reader io.Reader, buildRequest *BuildRequest) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(buildRequest); err != nil {
		return err
	}

	var trailing struct{}
	if err := decoder.Decode(&trailing); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("build request must contain one JSON object")
}

func (agent *Agent) buildStatus(response http.ResponseWriter, request *http.Request) {
	buildID := request.PathValue("buildID")
	if err := requestmeta.ValidateBuildID(buildID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	run, ok := agent.build(buildID)
	if !ok {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "unknown build_id"})
		return
	}

	writeJSON(response, http.StatusOK, run.Status())
}

func (agent *Agent) tailBuildLogs(response http.ResponseWriter, request *http.Request) {
	buildID := request.PathValue("buildID")
	if err := requestmeta.ValidateBuildID(buildID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	run, ok := agent.build(buildID)
	if !ok {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "unknown build_id"})
		return
	}

	after := 0
	if rawAfter := request.URL.Query().Get("after"); rawAfter != "" {
		parsed, err := strconv.Atoi(rawAfter)
		if err != nil || parsed < 0 {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "after must be a non-negative sequence"})
			return
		}
		after = parsed
	}

	response.Header().Set("Content-Type", "application/x-ndjson")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Accel-Buffering", "no")
	response.Header().Set("Transwarp-Build-ID", run.ID)
	response.WriteHeader(http.StatusOK)

	flusher, _ := response.(http.Flusher)
	follow := request.URL.Query().Get("follow") != "false"
	run.Stream.writeTo(response, flusher, after, follow, run.done, request.Context())
}

func (agent *Agent) cancelBuild(response http.ResponseWriter, request *http.Request) {
	buildID := request.PathValue("buildID")
	if err := requestmeta.ValidateBuildID(buildID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	run, ok := agent.build(buildID)
	if !ok {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "unknown build_id"})
		return
	}

	if run.Done() {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "build already finished"})
		return
	}
	if run.HasTerminalRecord() {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "build already finished; result reporting is still in progress"})
		return
	}

	agent.cancelBuildRun(run, "cancel requested")
	writeJSON(response, http.StatusAccepted, map[string]string{"build_id": run.ID, "status": "canceling"})
}

func (request BuildRequest) Validate() error {
	if err := requestmeta.ValidateJobID(request.JobID); err != nil {
		return err
	}
	if err := requestmeta.Validate(request.RequestID, request.RepoURL, request.Ref, request.Commit); err != nil {
		return err
	}
	if (strings.TrimSpace(request.ReportURL) == "") != (strings.TrimSpace(request.ReportToken) == "") {
		return errors.New("report_url and report_token must be provided together")
	}
	if strings.TrimSpace(request.ReportToken) != "" {
		if err := validateHeaderValue(request.ReportToken, "report_token"); err != nil {
			return err
		}
	}
	if request.ReportURL != "" {
		if strings.Contains(request.ReportURL, "#") {
			return errors.New("report_url must not include query or fragment")
		}
		parsed, err := url.ParseRequestURI(request.ReportURL)
		if err != nil {
			return fmt.Errorf("report_url is invalid: %w", err)
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return errors.New("report_url must use http or https")
		}
		if parsed.Host == "" {
			return errors.New("report_url must include a host")
		}
		if parsed.Scheme == "http" && !endpoint.IsLoopbackHost(parsed.Hostname()) {
			return errors.New("report_url must use https unless it targets local loopback")
		}
		if parsed.User != nil {
			return errors.New("report_url must not include credentials")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("report_url must not include query or fragment")
		}
	}
	return nil
}

func validateBuildRequestForJob(request BuildRequest, job JobConfig) error {
	if !job.Checkout {
		if strings.TrimSpace(request.RepoURL) != "" {
			return fmt.Errorf("repo_url is only accepted for checkout jobs; job %q has checkout disabled", job.ID)
		}
		if strings.TrimSpace(request.Ref) != "" {
			return fmt.Errorf("ref is only accepted for checkout jobs; job %q has checkout disabled", job.ID)
		}
		if strings.TrimSpace(request.Commit) != "" {
			return fmt.Errorf("commit is only accepted for checkout jobs; job %q has checkout disabled", job.ID)
		}
		return nil
	}
	if strings.TrimSpace(request.RepoURL) == "" {
		return errors.New("repo_url is required for checkout jobs")
	}
	if !repositoryAllowed(request.RepoURL, job.AllowedRepositories) {
		return fmt.Errorf("repo_url %q is not allowed for job %q", request.RepoURL, job.ID)
	}
	if strings.TrimSpace(request.Ref) == "" && strings.TrimSpace(request.Commit) == "" {
		return errors.New("ref or commit is required for checkout jobs")
	}
	return nil
}

func (agent *Agent) startBuildRun(buildRequest BuildRequest, job JobConfig) (*BuildRun, error) {
	agent.buildsMu.Lock()
	defer agent.buildsMu.Unlock()

	if existing := agent.buildWithRequestIDLocked(buildRequest.RequestID); existing != nil {
		if !sameBuildRequest(existing.Request, buildRequest) {
			return nil, errors.New("request_id already used with a different build payload")
		}
		return existing, nil
	}
	if !agent.acceptingNewBuildsLocked() {
		return nil, errNotAcceptingBuilds
	}
	if agent.activeBuildID != "" && maxQueuedBuilds >= 0 && len(agent.queuedBuildIDs) >= maxQueuedBuilds {
		return nil, fmt.Errorf("build queue is full (%d queued)", len(agent.queuedBuildIDs))
	}

	ctx, cancel := context.WithCancel(context.Background())
	buildID := randomBuildID()
	streamRedactor := reportRequestRedactor(agent.redactor, buildRequest)
	run := &BuildRun{
		ID:        buildID,
		Request:   buildRequest,
		Job:       job,
		Stream:    newBuildStream(buildID, job.ID, streamRedactor).mirroringToRunnerLog(),
		CreatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		closed:    make(chan struct{}),
	}
	if buildRequest.ReportURL != "" {
		run.reportStatus = "pending"
	}
	agent.builds[run.ID] = run
	if agent.activeBuildID == "" {
		agent.startBuildRunLocked(run)
	} else {
		agent.queuedBuildIDs = append(agent.queuedBuildIDs, run.ID)
		run.Stream.write(Event{Kind: "build", Message: "queued", JobID: job.ID})
	}
	agent.requestRegistrationRefresh()
	return run, nil
}

func (agent *Agent) startBuildRunLocked(run *BuildRun) {
	agent.activeBuildID = run.ID
	run.markStarted()
	go agent.finishBuildRun(run)
}

func (agent *Agent) finishBuildRun(run *BuildRun) {
	defer run.cancel()
	defer close(run.closed)

	record := agent.runJob(run.ctx, run.ID, run.Request, run.Job, run.Stream)
	if run.Request.ReportURL != "" {
		record.ReportStatus = "pending"
	}
	run.setRecord(record)
	agent.writeLedger(record)

	if run.Request.ReportURL != "" {
		if err := agent.reportBuild(run.Request, record); err != nil {
			message := reportFailureMessage(run, err)
			run.setReportStatus("failed", message)
			record.ReportStatus = "failed"
			record.ReportError = message
			agent.writeLedger(record)
			run.Stream.write(Event{Kind: "error", Message: "report failed: " + message, JobID: run.Job.ID})
		} else {
			run.setReportStatus("reported", "")
			record.ReportStatus = "reported"
			record.ReportError = ""
			agent.writeLedger(record)
			run.Stream.write(Event{Kind: "build", Message: "reported result", JobID: run.Job.ID})
		}
	}
	agent.completeBuild(run.ID)
	close(run.done)
}

func (agent *Agent) runJob(parent context.Context, buildID string, buildRequest BuildRequest, job JobConfig, stream *buildStream) LedgerRecord {
	started := time.Now()
	record := LedgerRecord{
		BuildID:   buildID,
		JobID:     job.ID,
		RequestID: buildRequest.RequestID,
		RepoURL:   buildRequest.RepoURL,
		Ref:       buildRequest.Ref,
		Commit:    buildRequest.Commit,
		StartedAt: started,
		ExitCode:  -1,
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(job.TimeoutSeconds)*time.Second)
	defer cancel()

	stream.write(Event{Kind: "build", Message: fmt.Sprintf("starting %s", labelFor(job)), JobID: job.ID})

	workspace, err := agent.prepareWorkspace(ctx, buildRequest, job, stream)
	if err != nil {
		return finishWithError(record, stream, err)
	}
	defer workspace.Cleanup()
	record.Workspace = workspace.Path

	command := exec.CommandContext(ctx, job.Command, job.Arguments...)
	command.Dir = workspace.Path
	command.Env = buildEnvironment(job.Environment, buildRequest, workspace.Path)
	configureBuildCommand(command)

	stdout, err := command.StdoutPipe()
	if err != nil {
		return finishWithError(record, stream, err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return finishWithError(record, stream, err)
	}

	if err := command.Start(); err != nil {
		return finishWithError(record, stream, err)
	}
	stopProcessGuard := watchBuildProcess(ctx, command.Process.Pid)
	defer stopProcessGuard()
	stopPowerAssertion := startPowerAssertion(ctx, command.Process.Pid, agent.config.PreventSleep, stream)
	defer stopPowerAssertion()

	var scanWG sync.WaitGroup
	scanWG.Add(2)
	go scanLines(&scanWG, stdout, stream)
	go scanLines(&scanWG, stderr, stream)
	scanWG.Wait()

	err = command.Wait()
	record.EndedAt = time.Now()
	if err != nil {
		record.ExitCode = exitCode(err)
		record.Error = err.Error()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			record.Error = "build timed out"
		} else if errors.Is(ctx.Err(), context.Canceled) {
			record.Error = "build canceled"
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			stream.write(Event{Kind: "build", Message: "canceled", JobID: job.ID})
		} else {
			stream.write(Event{Kind: "build", Message: fmt.Sprintf("failed with exit code %d", record.ExitCode), JobID: job.ID})
		}
		return record
	}

	record.ExitCode = 0
	stream.write(Event{Kind: "build", Message: "passed", JobID: job.ID})
	return record
}

func scanLines(wg *sync.WaitGroup, reader io.Reader, stream *buildStream) {
	defer wg.Done()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		stream.write(Event{Kind: "log", Message: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		stream.write(Event{Kind: "error", Message: "log stream failed: " + err.Error()})
	}
}

func buildEnvironment(local map[string]string, request BuildRequest, workspace string) []string {
	environment := map[string]string{
		"HOME":   os.Getenv("HOME"),
		"PATH":   "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin",
		"TMPDIR": os.TempDir(),
	}
	for key, value := range local {
		if strings.TrimSpace(key) == "" || reservedEnvironmentKey(key) {
			continue
		}
		environment[key] = value
	}
	if workspace != "" {
		environment["TRANSWARP_WORKSPACE"] = workspace
	}
	if request.RepoURL != "" {
		environment["TRANSWARP_REPO_URL"] = request.RepoURL
	}
	if request.Ref != "" {
		environment["TRANSWARP_REF"] = request.Ref
	}
	if request.Commit != "" {
		environment["TRANSWARP_COMMIT"] = request.Commit
	}

	values := make([]string, 0, len(environment))
	for key, value := range environment {
		values = append(values, key+"="+value)
	}
	return values
}

func finishWithError(record LedgerRecord, stream *buildStream, err error) LedgerRecord {
	record.EndedAt = time.Now()
	record.ExitCode = -1
	record.Error = stream.redactor.Redact(err.Error())
	if errors.Is(err, context.Canceled) {
		record.Error = "build canceled"
	}
	stream.write(Event{Kind: "error", Message: err.Error(), JobID: record.JobID})
	if record.Error == "build canceled" {
		stream.write(Event{Kind: "build", Message: "canceled", JobID: record.JobID})
	} else {
		stream.write(Event{Kind: "build", Message: "failed with exit code -1", JobID: record.JobID})
	}
	return record
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func labelFor(job JobConfig) string {
	if job.Label != "" {
		return job.Label
	}
	return job.ID
}

func (agent *Agent) writeLedger(record LedgerRecord) {
	path, err := ledgerPath()
	if err != nil {
		Emit(Event{Kind: "error", Message: "ledger unavailable: " + err.Error()})
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		Emit(Event{Kind: "error", Message: "ledger unavailable: " + err.Error()})
		return
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		Emit(Event{Kind: "error", Message: "ledger unavailable: " + err.Error()})
		return
	}
	defer file.Close()

	_ = json.NewEncoder(file).Encode(record)
}

func ledgerPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "Transwarp", "jobs.jsonl"), nil
}

func (agent *Agent) reportBuild(request BuildRequest, record LedgerRecord) error {
	if request.ReportURL == "" {
		return nil
	}

	status := "failed"
	if record.ExitCode == 0 && record.Error == "" {
		status = "passed"
	} else if record.Error == "build canceled" {
		status = "canceled"
	}

	report := BuildReport{
		BuildID:    record.BuildID,
		JobID:      record.JobID,
		RequestID:  record.RequestID,
		MachineID:  agent.config.MachineID,
		Machine:    agent.config.MachineName,
		RepoURL:    record.RepoURL,
		Ref:        record.Ref,
		Commit:     record.Commit,
		StartedAt:  record.StartedAt,
		EndedAt:    record.EndedAt,
		ExitCode:   record.ExitCode,
		Status:     status,
		Error:      record.Error,
		PublicURL:  agent.publicURL(),
		DurationMS: record.EndedAt.Sub(record.StartedAt).Milliseconds(),
	}

	data, err := json.Marshal(report)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= len(reportRetryDelays); attempt++ {
		if attempt > 0 {
			time.Sleep(reportRetryDelays[attempt-1])
		}

		retry, err := agent.postBuildReport(request, data)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry {
			break
		}
	}

	return lastErr
}

func (agent *Agent) postBuildReport(request BuildRequest, data []byte) (bool, error) {
	reportCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(reportCtx, http.MethodPost, request.ReportURL, bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if request.ReportToken != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+request.ReportToken)
	}
	if agent.config.CIAccessClientID != "" {
		httpRequest.Header.Set("CF-Access-Client-Id", agent.config.CIAccessClientID)
		httpRequest.Header.Set("CF-Access-Client-Secret", agent.config.CIAccessClientSecret)
	}

	response, err := newCIHTTPClient(15 * time.Second).Do(httpRequest)
	if err != nil {
		return true, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		retry := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		body := strings.TrimSpace(string(data))
		if body != "" {
			return retry, fmt.Errorf("report endpoint returned %s: %s", response.Status, body)
		}
		return retry, fmt.Errorf("report endpoint returned %s", response.Status)
	}

	return false, nil
}

type buildStream struct {
	jobID    string
	buildID  string
	redactor Redactor
	mu       sync.Mutex
	sequence int
	events   []Event
	mirror   bool
}

func newBuildStream(buildID string, jobID string, redactor Redactor) *buildStream {
	return &buildStream{buildID: buildID, jobID: jobID, redactor: redactor}
}

func (stream *buildStream) mirroringToRunnerLog() *buildStream {
	stream.mirror = true
	return stream
}

func (stream *buildStream) write(event Event) {
	stream.mu.Lock()

	stream.sequence++
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	if event.JobID == "" {
		event.JobID = stream.jobID
	}
	if event.BuildID == "" {
		event.BuildID = stream.buildID
	}
	event.Sequence = stream.sequence
	event.Message = stream.redactor.Redact(event.Message)
	stream.events = append(stream.events, event)
	if maxRetainedBuildEvents > 0 && len(stream.events) > maxRetainedBuildEvents {
		stream.events = stream.events[len(stream.events)-maxRetainedBuildEvents:]
	}
	mirror := stream.mirror
	stream.mu.Unlock()

	if mirror {
		Emit(event)
	}
}

func (stream *buildStream) writeTo(writer io.Writer, flusher http.Flusher, after int, follow bool, done <-chan struct{}, ctx context.Context) {
	next := after
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var keepalive <-chan time.Time
	var keepaliveTicker *time.Ticker
	if follow && buildStreamKeepaliveInterval > 0 {
		keepaliveTicker = time.NewTicker(buildStreamKeepaliveInterval)
		keepalive = keepaliveTicker.C
		defer keepaliveTicker.Stop()
	}

	for {
		wrote := false
		for _, event := range stream.eventsAfter(next) {
			if writeEvent(writer, flusher, event) {
				next = event.Sequence
				wrote = true
			} else {
				return
			}
		}
		if !follow {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-done:
			for _, event := range stream.eventsAfter(next) {
				if !writeEvent(writer, flusher, event) {
					return
				}
				next = event.Sequence
			}
			return
		case <-keepalive:
			if !writeEvent(writer, flusher, stream.keepaliveEvent()) {
				return
			}
		case <-ticker.C:
			if wrote {
				continue
			}
		}
	}
}

func (stream *buildStream) keepaliveEvent() Event {
	return Event{
		Kind:    "info",
		Message: "build stream keepalive",
		BuildID: stream.buildID,
		JobID:   stream.jobID,
		Time:    time.Now(),
	}
}

func (stream *buildStream) eventsAfter(after int) []Event {
	stream.mu.Lock()
	defer stream.mu.Unlock()

	events := []Event{}
	if len(stream.events) > 0 && after < stream.events[0].Sequence-1 {
		events = append(events, Event{
			Kind:     "info",
			Message:  fmt.Sprintf("retained log history truncated before sequence %d", stream.events[0].Sequence),
			BuildID:  stream.buildID,
			JobID:    stream.jobID,
			Sequence: stream.events[0].Sequence - 1,
			Time:     stream.events[0].Time,
		})
	}
	for _, event := range stream.events {
		if event.Sequence > after {
			events = append(events, event)
		}
	}
	return events
}

func writeEvent(writer io.Writer, flusher http.Flusher, event Event) bool {
	data, err := json.Marshal(event)
	if err != nil {
		return true
	}
	if _, err := fmt.Fprintln(writer, string(data)); err != nil {
		return false
	}
	if flusher != nil {
		flusher.Flush()
	}
	return true
}

func terminalStatus(message string) bool {
	return message == "passed" || message == "canceled" || strings.HasPrefix(message, "failed")
}

func (agent *Agent) build(buildID string) (*BuildRun, bool) {
	agent.buildsMu.Lock()
	defer agent.buildsMu.Unlock()

	run, ok := agent.builds[buildID]
	return run, ok
}

func (agent *Agent) buildWithRequestIDLocked(requestID string) *BuildRun {
	for _, run := range agent.builds {
		if run.Request.RequestID == requestID {
			return run
		}
	}
	return nil
}

func sameBuildRequest(left BuildRequest, right BuildRequest) bool {
	return left.JobID == right.JobID &&
		left.RequestID == right.RequestID &&
		left.RepoURL == right.RepoURL &&
		left.Ref == right.Ref &&
		left.Commit == right.Commit &&
		left.ReportURL == right.ReportURL &&
		left.ReportToken == right.ReportToken
}

func (agent *Agent) recentBuilds(limit int) []BuildStatusResponse {
	agent.buildsMu.Lock()
	runs := make([]*BuildRun, 0, len(agent.builds))
	for _, run := range agent.builds {
		runs = append(runs, run)
	}
	agent.buildsMu.Unlock()

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	statuses := make([]BuildStatusResponse, 0, len(runs))
	for _, run := range runs {
		statuses = append(statuses, run.Status())
	}
	if len(statuses) > 0 {
		return statuses
	}
	return ledgerRecentBuilds(limit)
}

func ledgerRecentBuilds(limit int) []BuildStatusResponse {
	if limit == 0 {
		return nil
	}
	path, err := ledgerPath()
	if err != nil {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	recordsByBuildID := map[string]LedgerRecord{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record LedgerRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if !validLedgerStatusRecord(record) {
			continue
		}
		recordsByBuildID[record.BuildID] = record
	}
	records := make([]LedgerRecord, 0, len(recordsByBuildID))
	for _, record := range recordsByBuildID {
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool {
		left := ledgerRecordSortTime(records[i])
		right := ledgerRecordSortTime(records[j])
		if left.Equal(right) {
			return records[i].BuildID < records[j].BuildID
		}
		return left.After(right)
	})
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}

	statuses := make([]BuildStatusResponse, 0, len(records))
	for _, record := range records {
		statuses = append(statuses, buildStatusFromLedger(record))
	}
	return statuses
}

func validLedgerStatusRecord(record LedgerRecord) bool {
	if err := requestmeta.ValidateBuildID(record.BuildID); err != nil {
		return false
	}
	if err := requestmeta.ValidateJobID(record.JobID); err != nil {
		return false
	}
	if err := requestmeta.ValidateRequestID(record.RequestID); err != nil {
		return false
	}
	if record.StartedAt.IsZero() || record.EndedAt.IsZero() {
		return false
	}
	return validLedgerReportStatus(record.ReportStatus, record.ReportError)
}

func validLedgerReportStatus(status string, reportError string) bool {
	switch status {
	case "", "pending", "reported":
		return reportError == ""
	case "failed":
		return true
	default:
		return false
	}
}

func buildStatusFromLedger(record LedgerRecord) BuildStatusResponse {
	return BuildStatusResponse{
		BuildID:      record.BuildID,
		JobID:        record.JobID,
		RequestID:    record.RequestID,
		Status:       statusForRecord(record),
		CreatedAt:    record.StartedAt,
		ReportStatus: record.ReportStatus,
		ReportError:  record.ReportError,
		Result:       buildStatusResult(record),
	}
}

func ledgerRecordSortTime(record LedgerRecord) time.Time {
	if !record.EndedAt.IsZero() {
		return record.EndedAt
	}
	return record.StartedAt
}

func (agent *Agent) completeBuild(buildID string) {
	agent.buildsMu.Lock()
	defer agent.buildsMu.Unlock()

	if agent.activeBuildID == buildID {
		agent.activeBuildID = ""
	}
	for len(agent.queuedBuildIDs) > 0 && agent.activeBuildID == "" {
		nextID := agent.queuedBuildIDs[0]
		agent.queuedBuildIDs = agent.queuedBuildIDs[1:]
		next, ok := agent.builds[nextID]
		if !ok || next.Done() {
			continue
		}
		agent.startBuildRunLocked(next)
		break
	}
	if len(agent.builds) <= 50 {
		agent.requestRegistrationRefresh()
		return
	}
	for id, run := range agent.builds {
		if id != agent.activeBuildID && run.Done() {
			delete(agent.builds, id)
			if len(agent.builds) <= 50 {
				agent.requestRegistrationRefresh()
				return
			}
		}
	}
	agent.requestRegistrationRefresh()
}

func (agent *Agent) cancelBuildRun(run *BuildRun, message string) {
	var queued bool

	agent.buildsMu.Lock()
	for index, buildID := range agent.queuedBuildIDs {
		if buildID == run.ID {
			agent.queuedBuildIDs = append(agent.queuedBuildIDs[:index], agent.queuedBuildIDs[index+1:]...)
			queued = true
			break
		}
	}
	agent.buildsMu.Unlock()
	if queued {
		agent.requestRegistrationRefresh()
	}

	run.Stream.write(Event{Kind: "build", Message: message, JobID: run.Job.ID})
	if queued {
		run.cancel()
		record := canceledQueuedRecord(run)
		if run.Request.ReportURL != "" {
			record.ReportStatus = "pending"
		}
		run.setRecord(record)
		agent.writeLedger(record)
		run.Stream.write(Event{Kind: "build", Message: "canceled", JobID: run.Job.ID})
		go func() {
			if err := agent.reportBuild(run.Request, record); err != nil {
				message := reportFailureMessage(run, err)
				run.setReportStatus("failed", message)
				record.ReportStatus = "failed"
				record.ReportError = message
				agent.writeLedger(record)
				run.Stream.write(Event{Kind: "error", Message: "report failed: " + message, JobID: run.Job.ID})
			} else if run.Request.ReportURL != "" {
				run.setReportStatus("reported", "")
				record.ReportStatus = "reported"
				record.ReportError = ""
				agent.writeLedger(record)
				run.Stream.write(Event{Kind: "build", Message: "reported result", JobID: run.Job.ID})
			}
			close(run.done)
			close(run.closed)
		}()
		return
	}

	run.cancel()
}

func canceledQueuedRecord(run *BuildRun) LedgerRecord {
	now := time.Now()
	return LedgerRecord{
		BuildID:   run.ID,
		JobID:     run.Job.ID,
		RequestID: run.Request.RequestID,
		RepoURL:   run.Request.RepoURL,
		Ref:       run.Request.Ref,
		Commit:    run.Request.Commit,
		StartedAt: now,
		EndedAt:   now,
		ExitCode:  -1,
		Error:     "build canceled",
	}
}

func (run *BuildRun) Done() bool {
	select {
	case <-run.done:
		return true
	default:
		return false
	}
}

func (run *BuildRun) Closed() bool {
	select {
	case <-run.closed:
		return true
	default:
		return false
	}
}

func (run *BuildRun) Started() bool {
	run.mu.Lock()
	defer run.mu.Unlock()

	return run.started
}

func (run *BuildRun) HasTerminalRecord() bool {
	run.mu.Lock()
	defer run.mu.Unlock()

	return !run.record.EndedAt.IsZero()
}

func (run *BuildRun) markStarted() {
	run.mu.Lock()
	defer run.mu.Unlock()

	run.started = true
}

func (run *BuildRun) setRecord(record LedgerRecord) {
	run.mu.Lock()
	defer run.mu.Unlock()
	run.record = record
}

func (run *BuildRun) setReportStatus(status string, err string) {
	run.mu.Lock()
	defer run.mu.Unlock()
	run.reportStatus = status
	run.reportError = err
}

func reportFailureMessage(run *BuildRun, err error) string {
	return reportRequestRedactor(run.Stream.redactor, run.Request).Redact(err.Error())
}

func reportRequestRedactor(redactor Redactor, request BuildRequest) Redactor {
	values := []string{request.ReportToken}
	if parsed, err := url.Parse(request.ReportURL); err == nil {
		if parsed.User != nil {
			values = append(values, parsed.User.Username())
			if password, ok := parsed.User.Password(); ok {
				values = append(values, password)
			}
		}
		for _, queryValues := range parsed.Query() {
			values = append(values, queryValues...)
		}
	}
	return redactor.WithValues(values...)
}

func (run *BuildRun) Status() BuildStatusResponse {
	run.mu.Lock()
	record := run.record
	started := run.started
	reportStatus := run.reportStatus
	reportError := run.reportError
	run.mu.Unlock()

	status := "running"
	var result *BuildStatusResult
	if run.Done() || !record.EndedAt.IsZero() {
		status = statusForRecord(record)
		result = buildStatusResult(record)
	} else if !started {
		status = "queued"
	}

	return BuildStatusResponse{
		BuildID:      run.ID,
		JobID:        run.Job.ID,
		RequestID:    run.Request.RequestID,
		Status:       status,
		CreatedAt:    run.CreatedAt,
		ReportStatus: reportStatus,
		ReportError:  reportError,
		Result:       result,
	}
}

func statusForRecord(record LedgerRecord) string {
	if record.ExitCode == 0 && record.Error == "" {
		return "passed"
	}
	if record.Error == "build canceled" {
		return "canceled"
	}
	return "failed"
}

func buildStatusResult(record LedgerRecord) *BuildStatusResult {
	return &BuildStatusResult{
		BuildID:   record.BuildID,
		JobID:     record.JobID,
		RequestID: record.RequestID,
		RepoURL:   record.RepoURL,
		Ref:       record.Ref,
		Commit:    record.Commit,
		StartedAt: record.StartedAt,
		EndedAt:   record.EndedAt,
		ExitCode:  record.ExitCode,
		Error:     record.Error,
	}
}

func (run *BuildRun) StartResponse() BuildStartResponse {
	return BuildStartResponse{
		BuildID:   run.ID,
		Status:    run.Status().Status,
		LogsURL:   "/v1/builds/" + run.ID + "/logs?after=0&follow=true",
		CancelURL: "/v1/builds/" + run.ID + "/cancel",
	}
}

func randomBuildID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("build-%d", time.Now().UnixNano())
	}
	return "build-" + fmt.Sprintf("%x", bytes)
}

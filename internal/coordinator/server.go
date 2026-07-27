package coordinator

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charliewilco/transwarp/internal/dispatch"
	"github.com/charliewilco/transwarp/internal/endpoint"
	"github.com/charliewilco/transwarp/internal/headerutil"
	"github.com/charliewilco/transwarp/internal/requestmeta"
	"github.com/charliewilco/transwarp/internal/tunnelnet"
)

type Server struct {
	token              string
	targetToken        string
	transwarpToken     string
	accessClientID     string
	accessClientSecret string
	publicURL          string
	httpClient         *http.Client
	statePath          string
	resultWaitTimeout  time.Duration

	mu                 sync.Mutex
	targets            map[string]Target
	results            map[string]BuildResult
	active             map[string]activeDispatch
	targetReservations map[string]int
}

type Options struct {
	Token              string
	TargetToken        string
	TranswarpToken     string
	AccessClientID     string
	AccessClientSecret string
	PublicURL          string
	StatePath          string
	HTTPClient         *http.Client
	ResultWaitTimeout  time.Duration
}

const (
	defaultResultWaitTimeout   = 10 * time.Second
	resultWaitPollInterval     = 50 * time.Millisecond
	resultDurationToleranceMS  = 100
	acceptedBuildWaitTimeout   = 2 * time.Second
	defaultTargetLease         = 90 * time.Second
	maxTargetLease             = 5 * time.Minute
	minimumSupportedMacOSMajor = 14
)

type Target struct {
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

type BuildResult struct {
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

type DispatchRequest struct {
	MachineID       string `json:"machine_id"`
	JobID           string `json:"job_id"`
	RequestID       string `json:"request_id"`
	RepoURL         string `json:"repo_url,omitempty"`
	Ref             string `json:"ref,omitempty"`
	Commit          string `json:"commit,omitempty"`
	MinCPUCount     int    `json:"min_cpu_count,omitempty"`
	MinMemoryBytes  uint64 `json:"min_memory_bytes,omitempty"`
	MinXcodeVersion string `json:"min_xcode_version,omitempty"`
}

type activeDispatch struct {
	Request         DispatchRequest `json:"request"`
	TargetMachineID string          `json:"target_machine_id"`
	RunnerBaseURL   string          `json:"runner_base_url"`
	RunnerBuildID   string          `json:"runner_build_id"`
}

func (active *activeDispatch) UnmarshalJSON(data []byte) error {
	type activeDispatchJSON struct {
		Request               DispatchRequest `json:"request"`
		TargetMachineID       string          `json:"target_machine_id"`
		RunnerBaseURL         string          `json:"runner_base_url"`
		RunnerBuildID         string          `json:"runner_build_id"`
		LegacyRequest         DispatchRequest `json:"Request"`
		LegacyTargetMachineID string          `json:"TargetMachineID"`
		LegacyRunnerBaseURL   string          `json:"RunnerBaseURL"`
		LegacyRunnerBuildID   string          `json:"RunnerBuildID"`
	}

	var payload activeDispatchJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.Request.RequestID == "" {
		payload.Request = payload.LegacyRequest
	}
	if payload.TargetMachineID == "" {
		payload.TargetMachineID = payload.LegacyTargetMachineID
	}
	if payload.RunnerBaseURL == "" {
		payload.RunnerBaseURL = payload.LegacyRunnerBaseURL
	}
	if payload.RunnerBuildID == "" {
		payload.RunnerBuildID = payload.LegacyRunnerBuildID
	}

	*active = activeDispatch{
		Request:         payload.Request,
		TargetMachineID: payload.TargetMachineID,
		RunnerBaseURL:   payload.RunnerBaseURL,
		RunnerBuildID:   payload.RunnerBuildID,
	}
	return nil
}

func New(options Options) (*Server, error) {
	options.Token = strings.TrimSpace(options.Token)
	options.TargetToken = strings.TrimSpace(options.TargetToken)
	options.TranswarpToken = strings.TrimSpace(options.TranswarpToken)
	options.PublicURL = strings.TrimSpace(options.PublicURL)
	if options.Token == "" {
		return nil, errors.New("coordinator token is required")
	}
	if err := headerutil.ValidateValue(options.Token, "coordinator token"); err != nil {
		return nil, err
	}
	if options.TargetToken == "" {
		options.TargetToken = options.Token
	}
	if err := headerutil.ValidateValue(options.TargetToken, "coordinator target token"); err != nil {
		return nil, err
	}
	if options.TranswarpToken == "" {
		return nil, errors.New("transwarp runner token is required")
	}
	if err := headerutil.ValidateValue(options.TranswarpToken, "transwarp runner token"); err != nil {
		return nil, err
	}
	if (strings.TrimSpace(options.AccessClientID) == "") != (strings.TrimSpace(options.AccessClientSecret) == "") {
		return nil, errors.New("Cloudflare Access client ID and secret must be provided together")
	}
	if strings.TrimSpace(options.AccessClientID) != "" {
		if err := headerutil.ValidateValue(options.AccessClientID, "Cloudflare Access client ID"); err != nil {
			return nil, err
		}
		if err := headerutil.ValidateValue(options.AccessClientSecret, "Cloudflare Access client secret"); err != nil {
			return nil, err
		}
	}
	if options.PublicURL == "" {
		return nil, errors.New("coordinator public URL is required")
	}
	if err := endpoint.ValidateBaseURL(options.PublicURL, "coordinator public URL"); err != nil {
		return nil, err
	}
	if options.HTTPClient == nil {
		options.HTTPClient = tunnelnet.NoRedirectHTTPClient()
	}
	if options.ResultWaitTimeout == 0 {
		options.ResultWaitTimeout = defaultResultWaitTimeout
	}

	server := &Server{
		token:              options.Token,
		targetToken:        options.TargetToken,
		transwarpToken:     options.TranswarpToken,
		accessClientID:     options.AccessClientID,
		accessClientSecret: options.AccessClientSecret,
		publicURL:          options.PublicURL,
		httpClient:         options.HTTPClient,
		statePath:          options.StatePath,
		resultWaitTimeout:  options.ResultWaitTimeout,
		targets:            map[string]Target{},
		results:            map[string]BuildResult{},
		active:             map[string]activeDispatch{},
		targetReservations: map[string]int{},
	}
	if err := server.loadState(); err != nil {
		return nil, err
	}
	return server, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.health)
	mux.HandleFunc("POST /transwarp/register", server.targetAuth(server.register))
	mux.HandleFunc("POST /transwarp/heartbeat", server.targetAuth(server.register))
	mux.HandleFunc("POST /transwarp/deregister", server.targetAuth(server.deregister))
	mux.HandleFunc("GET /transwarp/targets", server.auth(server.listTargets))
	mux.HandleFunc("POST /transwarp/dispatch", server.auth(server.dispatchBuild))
	mux.HandleFunc("POST /transwarp/dispatches/{requestID}/cancel", server.auth(server.cancelDispatch))
	mux.HandleFunc("POST /transwarp/result", server.targetAuth(server.recordResult))
	mux.HandleFunc("GET /transwarp/results", server.auth(server.listResults))
	mux.HandleFunc("GET /transwarp/results/{requestID}", server.auth(server.getResult))
	return mux
}

func (server *Server) health(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) register(response http.ResponseWriter, request *http.Request) {
	var target Target
	if err := decodeJSONBody(http.MaxBytesReader(response, request.Body, 64*1024), &target); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid target payload"})
		return
	}
	if err := requestmeta.ValidateMachineID(target.MachineID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := targetEligibilityError(target); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	target = normalizeTargetLease(target, time.Now())

	server.mu.Lock()
	server.targets[target.MachineID] = target
	err := server.saveStateLocked()
	server.mu.Unlock()
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "persist target registration"})
		return
	}

	writeJSON(response, http.StatusAccepted, target)
}

func (server *Server) deregister(response http.ResponseWriter, request *http.Request) {
	var target Target
	if err := decodeJSONBody(http.MaxBytesReader(response, request.Body, 64*1024), &target); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid target payload"})
		return
	}
	if err := requestmeta.ValidateMachineID(target.MachineID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	server.mu.Lock()
	delete(server.targets, target.MachineID)
	server.releaseUnacceptedDispatchesForTargetLocked(target.MachineID)
	err := server.saveStateLocked()
	server.mu.Unlock()
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "persist target deregistration"})
		return
	}

	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) listTargets(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, server.eligibleTargets())
}

func (server *Server) dispatchBuild(response http.ResponseWriter, request *http.Request) {
	var dispatchRequest DispatchRequest
	if err := decodeJSONBody(http.MaxBytesReader(response, request.Body, 64*1024), &dispatchRequest); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid dispatch payload"})
		return
	}
	if err := validateDispatchRequest(dispatchRequest); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	active, reconnect, ok := server.beginDispatch(response, dispatchRequest)
	if !ok {
		return
	}
	if reconnect {
		server.reconnectActiveDispatch(request.Context(), dispatchRequest, active, response)
		return
	}
	defer server.endDispatch(dispatchRequest.RequestID)

	targets, ok := server.dispatchTargets(dispatchRequest, response)
	if !ok {
		return
	}

	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	flusher, _ := response.(http.Flusher)
	writer := flushWriter{writer: response, flusher: flusher}

	var lastErr error
	for index, target := range targets {
		reservedTarget, err := server.reserveActiveDispatchTarget(dispatchRequest.RequestID, target.MachineID)
		if err != nil {
			lastErr = err
			if shouldTryNextReservationTarget(dispatchRequest, index, len(targets)) {
				fmt.Fprintf(writer, "[coordinator] target %s unavailable before build start: %v\n", target.MachineID, err)
				continue
			}
			fmt.Fprintf(writer, "dispatch failed: %v\n", err)
			return
		}
		target = reservedTarget
		err = server.dispatchToTarget(request.Context(), dispatchRequest, target, writer)
		if err != nil && !runnerTerminalFailure(err) {
			lastErr = err
			if dispatchContextEnded(request.Context(), err) {
				if server.activeRunnerBuildStarted(dispatchRequest.RequestID) {
					_, _ = server.waitForResult(context.Background(), dispatchRequest.RequestID)
				}
				fmt.Fprintf(writer, "dispatch failed: %v\n", err)
				return
			}
			if shouldTryNextTarget(dispatchRequest, err, index, len(targets)) {
				fmt.Fprintf(writer, "[coordinator] target %s rejected build start: %v\n", target.MachineID, err)
				server.releaseActiveDispatchTarget(dispatchRequest.RequestID, target.MachineID)
				continue
			}
			fmt.Fprintf(writer, "dispatch failed: %v\n", err)
			return
		}

		result, err := server.waitForResult(request.Context(), dispatchRequest.RequestID)
		if err != nil {
			fmt.Fprintf(writer, "dispatch failed: %v\n", err)
			return
		}
		fmt.Fprintf(writer, "[result] recorded %s\n", result.Status)
		if result.Status != "passed" {
			message := result.Status
			if result.Error != "" {
				message = result.Error
			}
			fmt.Fprintf(writer, "dispatch failed: %s\n", message)
			return
		}
		return
	}

	if lastErr != nil {
		fmt.Fprintf(writer, "dispatch failed: no target accepted build start: %v\n", lastErr)
	}
}

func (server *Server) reconnectActiveDispatch(ctx context.Context, request DispatchRequest, active activeDispatch, response http.ResponseWriter) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	flusher, _ := response.(http.Flusher)
	writer := flushWriter{writer: response, flusher: flusher}

	_ = json.NewEncoder(writer).Encode(dispatch.Event{
		Kind:      "coordinator",
		Message:   "accepted runner build",
		BuildID:   active.RunnerBuildID,
		JobID:     request.JobID,
		RequestID: request.RequestID,
		MachineID: active.TargetMachineID,
		PublicURL: active.RunnerBaseURL,
		Time:      time.Now().UTC(),
	})

	err := dispatch.Tail(ctx, server.httpClient, dispatch.Request{
		BaseURL:            active.RunnerBaseURL,
		Token:              server.transwarpToken,
		AccessClientID:     server.accessClientID,
		AccessClientSecret: server.accessClientSecret,
		BuildID:            active.RunnerBuildID,
	}, writer)
	if err != nil && !runnerTerminalFailure(err) {
		fmt.Fprintf(writer, "dispatch failed: %v\n", err)
		return
	}

	result, err := server.waitForResult(ctx, request.RequestID)
	if err != nil {
		fmt.Fprintf(writer, "dispatch failed: %v\n", err)
		return
	}
	fmt.Fprintf(writer, "[result] recorded %s\n", result.Status)
	if result.Status != "passed" {
		message := result.Status
		if result.Error != "" {
			message = result.Error
		}
		fmt.Fprintf(writer, "dispatch failed: %s\n", message)
	}
}

func (server *Server) dispatchToTarget(ctx context.Context, request DispatchRequest, target Target, writer io.Writer) error {
	baseURL := target.PublicURL
	if baseURL == "" {
		baseURL = "http://" + target.Listen
	}

	runnerRequest := dispatch.Request{
		BaseURL:            baseURL,
		Token:              server.transwarpToken,
		AccessClientID:     server.accessClientID,
		AccessClientSecret: server.accessClientSecret,
		JobID:              request.JobID,
		RequestID:          request.RequestID,
		RepoURL:            request.RepoURL,
		Ref:                request.Ref,
		Commit:             request.Commit,
		ReportURL:          strings.TrimRight(server.publicURL, "/") + "/transwarp/result",
		ReportToken:        server.targetToken,
	}
	started, err := dispatch.Start(ctx, server.httpClient, runnerRequest)
	if err != nil {
		return err
	}
	runnerRequest.BuildID = started.BuildID
	if err := server.markActiveDispatchBuild(request.RequestID, target.MachineID, baseURL, started.BuildID); err != nil {
		_ = dispatch.Cancel(context.Background(), server.httpClient, runnerRequest)
		return fmt.Errorf("persist accepted runner build: %w", err)
	}
	_ = json.NewEncoder(writer).Encode(dispatch.Event{
		Kind:      "coordinator",
		Message:   "accepted runner build",
		BuildID:   started.BuildID,
		JobID:     request.JobID,
		RequestID: request.RequestID,
		MachineID: target.MachineID,
		PublicURL: baseURL,
		Time:      time.Now().UTC(),
	})
	return dispatch.Tail(ctx, server.httpClient, runnerRequest, writer)
}

func (server *Server) cancelDispatch(response http.ResponseWriter, request *http.Request) {
	requestID := strings.TrimSpace(request.PathValue("requestID"))
	if requestID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "request_id is required"})
		return
	}
	if err := requestmeta.ValidateRequestID(requestID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	server.mu.Lock()
	active, ok := server.active[requestID]
	server.mu.Unlock()
	if !ok {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "dispatch is not active"})
		return
	}
	if active.RunnerBaseURL == "" || active.RunnerBuildID == "" {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "dispatch has not started a runner build"})
		return
	}

	err := dispatch.Cancel(request.Context(), server.httpClient, dispatch.Request{
		BaseURL:            active.RunnerBaseURL,
		Token:              server.transwarpToken,
		AccessClientID:     server.accessClientID,
		AccessClientSecret: server.accessClientSecret,
		BuildID:            active.RunnerBuildID,
	})
	if err != nil {
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(response, http.StatusAccepted, map[string]string{
		"request_id": requestID,
		"machine_id": active.TargetMachineID,
		"build_id":   active.RunnerBuildID,
		"status":     "canceling",
	})
}

func shouldTryNextTarget(request DispatchRequest, err error, index int, count int) bool {
	if strings.TrimSpace(request.MachineID) != "" || index >= count-1 {
		return false
	}
	var startError *dispatch.StartError
	return errors.As(err, &startError)
}

func shouldTryNextReservationTarget(request DispatchRequest, index int, count int) bool {
	return strings.TrimSpace(request.MachineID) == "" && index < count-1
}

func (server *Server) beginDispatch(response http.ResponseWriter, request DispatchRequest) (activeDispatch, bool, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	if result, ok := server.results[request.RequestID]; ok {
		if !sameDispatchResult(request, result) {
			writeJSON(response, http.StatusConflict, map[string]string{"error": "request_id already has a different recorded result"})
			return activeDispatch{}, false, false
		}
		writeRecordedResult(response, result)
		return activeDispatch{}, false, false
	}
	if active, ok := server.active[request.RequestID]; ok {
		if !sameDispatchRequest(active.Request, request) {
			writeJSON(response, http.StatusConflict, map[string]string{"error": "request_id is already dispatching with a different payload"})
			return activeDispatch{}, false, false
		}
		if active.RunnerBaseURL != "" && active.RunnerBuildID != "" {
			return active, true, true
		}
		writeJSON(response, http.StatusConflict, map[string]string{"error": "request_id is already dispatching"})
		return activeDispatch{}, false, false
	}

	server.active[request.RequestID] = activeDispatch{Request: request}
	return activeDispatch{}, false, true
}

func (server *Server) endDispatch(requestID string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.releaseActiveDispatchLocked(requestID)
}

func (server *Server) reserveActiveDispatchTarget(requestID string, machineID string) (Target, error) {
	server.mu.Lock()
	defer server.mu.Unlock()

	active, ok := server.active[requestID]
	if !ok {
		return Target{}, errors.New("dispatch is no longer active")
	}
	if active.TargetMachineID == machineID {
		target, ok := server.targetLocked(machineID)
		if !ok {
			return Target{}, errors.New("target is unavailable")
		}
		return server.targetWithReservationsLocked(target), nil
	}
	server.releaseTargetReservationLocked(active.TargetMachineID)

	target, ok := server.targetLocked(machineID)
	if !ok {
		active.TargetMachineID = ""
		active.RunnerBaseURL = ""
		active.RunnerBuildID = ""
		server.active[requestID] = active
		return Target{}, errors.New("target is unavailable")
	}
	reservedLoadTarget := server.targetWithReservationsLocked(target)
	if err := targetEligibilityError(reservedLoadTarget); err != nil {
		active.TargetMachineID = ""
		active.RunnerBaseURL = ""
		active.RunnerBuildID = ""
		server.active[requestID] = active
		return Target{}, fmt.Errorf("target is unsupported: %w", err)
	}
	if err := targetLoadError(reservedLoadTarget); err != nil {
		active.TargetMachineID = ""
		active.RunnerBaseURL = ""
		active.RunnerBuildID = ""
		server.active[requestID] = active
		return Target{}, err
	}
	server.targetReservations[machineID]++
	active.TargetMachineID = machineID
	active.RunnerBaseURL = ""
	active.RunnerBuildID = ""
	server.active[requestID] = active
	return server.targetWithReservationsLocked(target), nil
}

func (server *Server) markActiveDispatchBuild(requestID string, machineID string, runnerBaseURL string, runnerBuildID string) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	active, ok := server.active[requestID]
	if !ok || active.TargetMachineID != machineID {
		return nil
	}
	active.RunnerBaseURL = runnerBaseURL
	active.RunnerBuildID = runnerBuildID
	server.active[requestID] = active
	return server.saveStateLocked()
}

func (server *Server) releaseActiveDispatchTarget(requestID string, machineID string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	active, ok := server.active[requestID]
	if !ok || active.TargetMachineID != machineID {
		return
	}
	server.releaseTargetReservationLocked(machineID)
	active.TargetMachineID = ""
	active.RunnerBaseURL = ""
	active.RunnerBuildID = ""
	server.active[requestID] = active
}

func (server *Server) releaseActiveDispatchLocked(requestID string) {
	active, ok := server.active[requestID]
	if !ok {
		return
	}
	server.releaseTargetReservationLocked(active.TargetMachineID)
	delete(server.active, requestID)
}

func (server *Server) releaseUnacceptedDispatchesForTargetLocked(machineID string) {
	if machineID == "" {
		return
	}
	for requestID, active := range server.active {
		if active.TargetMachineID != machineID {
			continue
		}
		if strings.TrimSpace(active.RunnerBaseURL) != "" && strings.TrimSpace(active.RunnerBuildID) != "" {
			continue
		}
		server.releaseTargetReservationLocked(machineID)
		delete(server.active, requestID)
	}
}

func (server *Server) releaseTargetReservationLocked(machineID string) {
	if machineID == "" {
		return
	}
	count := server.targetReservations[machineID]
	if count <= 1 {
		delete(server.targetReservations, machineID)
		return
	}
	server.targetReservations[machineID] = count - 1
}

func writeRecordedResult(response http.ResponseWriter, result BuildResult) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(dispatch.Event{
		Kind:      "coordinator",
		Message:   "accepted runner build",
		BuildID:   result.BuildID,
		JobID:     result.JobID,
		RequestID: result.RequestID,
		MachineID: result.MachineID,
		PublicURL: result.PublicURL,
		Time:      time.Now().UTC(),
	})
	fmt.Fprintf(response, "[result] recorded %s\n", result.Status)
	if result.Status != "passed" {
		message := result.Status
		if result.Error != "" {
			message = result.Error
		}
		fmt.Fprintf(response, "dispatch failed: %s\n", message)
		return
	}
}

func runnerTerminalFailure(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return message == "canceled" || strings.HasPrefix(message, "failed")
}

func dispatchContextEnded(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "context canceled") || strings.Contains(message, "context deadline exceeded")
}

func (server *Server) activeRunnerBuildStarted(requestID string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()

	active, ok := server.active[requestID]
	return ok && active.RunnerBuildID != ""
}

func (server *Server) waitForAcceptedRunnerBuild(ctx context.Context, requestID string, machineID string, buildID string) bool {
	timeout := time.NewTimer(acceptedBuildWaitTimeout)
	defer timeout.Stop()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		server.mu.Lock()
		active, ok := server.active[requestID]
		if !ok {
			server.mu.Unlock()
			return false
		}
		if active.TargetMachineID != machineID {
			server.mu.Unlock()
			return false
		}
		if active.RunnerBuildID == buildID {
			server.mu.Unlock()
			return true
		}
		if active.RunnerBuildID != "" {
			server.mu.Unlock()
			return false
		}
		server.mu.Unlock()

		select {
		case <-ctx.Done():
			return false
		case <-timeout.C:
			return false
		case <-ticker.C:
		}
	}
}

func (server *Server) recordResult(response http.ResponseWriter, request *http.Request) {
	var result BuildResult
	if err := decodeJSONBody(http.MaxBytesReader(response, request.Body, 64*1024), &result); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid result payload"})
		return
	}
	if err := requestmeta.ValidateRequestID(result.RequestID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := requestmeta.ValidateBuildID(result.BuildID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := requestmeta.ValidateJobID(result.JobID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := requestmeta.ValidateMachineID(result.MachineID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !validBuildResultStatus(result.Status) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "status must be passed, failed, or canceled"})
		return
	}
	if err := validateBuildResultConsistency(result); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

retry:
	server.mu.Lock()
	if existing, ok := server.results[result.RequestID]; ok {
		if !sameBuildResult(existing, result) {
			server.mu.Unlock()
			writeJSON(response, http.StatusConflict, map[string]string{"error": "request_id already has a different recorded result"})
			return
		}
		server.mu.Unlock()
		writeJSON(response, http.StatusAccepted, existing)
		return
	}
	active, ok := server.active[result.RequestID]
	if !ok {
		server.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"error": "result does not match an active dispatch"})
		return
	}
	if !sameDispatchResult(active.Request, result) {
		server.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"error": "result does not match active dispatch"})
		return
	}
	if active.TargetMachineID != "" && result.MachineID != active.TargetMachineID {
		server.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"error": "result does not match selected target"})
		return
	}
	if active.RunnerBuildID == "" {
		if active.TargetMachineID != "" && result.MachineID == active.TargetMachineID {
			server.mu.Unlock()
			if server.waitForAcceptedRunnerBuild(request.Context(), result.RequestID, result.MachineID, result.BuildID) {
				goto retry
			}
			writeJSON(response, http.StatusConflict, map[string]string{"error": "result cannot be recorded before a runner build is accepted"})
			return
		}
		server.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"error": "result cannot be recorded before a runner build is accepted"})
		return
	}
	if result.BuildID != active.RunnerBuildID {
		server.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"error": "result does not match selected runner build"})
		return
	}
	if selectedRunnerRequiresPublicURL(active.RunnerBaseURL) && strings.TrimSpace(result.PublicURL) == "" {
		server.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"error": "result public_url is required for selected public runner"})
		return
	}
	if strings.TrimSpace(result.PublicURL) != "" && active.RunnerBaseURL != "" && normalizeBaseURL(result.PublicURL) != normalizeBaseURL(active.RunnerBaseURL) {
		server.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"error": "result public_url does not match selected runner"})
		return
	}
	server.results[result.RequestID] = result
	server.releaseActiveDispatchLocked(result.RequestID)
	err := server.saveStateLocked()
	server.mu.Unlock()
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "persist build result"})
		return
	}

	writeJSON(response, http.StatusAccepted, result)
}

func (server *Server) listResults(response http.ResponseWriter, request *http.Request) {
	server.mu.Lock()
	results := make([]BuildResult, 0, len(server.results))
	for _, result := range server.results {
		results = append(results, result)
	}
	server.mu.Unlock()

	sort.Slice(results, func(i, j int) bool {
		return results[i].StartedAt.Before(results[j].StartedAt)
	})
	writeJSON(response, http.StatusOK, results)
}

func (server *Server) getResult(response http.ResponseWriter, request *http.Request) {
	requestID := strings.TrimSpace(request.PathValue("requestID"))
	if requestID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "request_id is required"})
		return
	}
	if err := requestmeta.ValidateRequestID(requestID); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	result, ok := server.result(requestID)
	if !ok {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "unknown request_id"})
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func decodeJSONBody(reader io.Reader, value any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}

	var trailing struct{}
	if err := decoder.Decode(&trailing); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("payload must contain one JSON object")
}

func (server *Server) waitForResult(ctx context.Context, requestID string) (BuildResult, error) {
	if result, ok := server.result(requestID); ok {
		return result, nil
	}

	timer := time.NewTimer(server.resultWaitTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(resultWaitPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return BuildResult{}, ctx.Err()
		case <-timer.C:
			return BuildResult{}, fmt.Errorf("result callback for request %s was not recorded within %s", requestID, server.resultWaitTimeout)
		case <-ticker.C:
			if result, ok := server.result(requestID); ok {
				return result, nil
			}
		}
	}
}

func (server *Server) result(requestID string) (BuildResult, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()

	result, ok := server.results[requestID]
	return result, ok
}

func (server *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		token, ok := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(server.token)) != 1 {
			writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(response, request)
	}
}

func (server *Server) targetAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		token, ok := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(server.targetToken)) != 1 {
			writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(response, request)
	}
}

func (server *Server) activeTargets() []Target {
	now := time.Now()

	server.mu.Lock()
	changed := false
	targets := []Target{}
	for id, target := range server.targets {
		if !target.LeaseExpiresAt.IsZero() && target.LeaseExpiresAt.Before(now) {
			delete(server.targets, id)
			changed = true
			continue
		}
		targets = append(targets, server.targetWithReservationsLocked(target))
	}
	if changed {
		_ = server.saveStateLocked()
	}
	server.mu.Unlock()

	sort.Slice(targets, func(i, j int) bool {
		return targets[i].MachineID < targets[j].MachineID
	})
	return targets
}

func (server *Server) target(machineID string) (Target, bool) {
	for _, target := range server.activeTargets() {
		if target.MachineID == machineID {
			return target, true
		}
	}
	return Target{}, false
}

func (server *Server) targetLocked(machineID string) (Target, bool) {
	target, ok := server.targets[machineID]
	if !ok {
		return Target{}, false
	}
	if !target.LeaseExpiresAt.IsZero() && target.LeaseExpiresAt.Before(time.Now()) {
		return Target{}, false
	}
	return target, true
}

func (server *Server) targetWithReservationsLocked(target Target) Target {
	target.QueuedBuilds += server.targetReservations[target.MachineID]
	return target
}

func (server *Server) eligibleTargets() []Target {
	targets := []Target{}
	for _, target := range server.activeTargets() {
		if targetEligibilityError(target) == nil && targetLoadError(target) == nil {
			targets = append(targets, target)
		}
	}
	sortTargetsByLoad(targets)
	return targets
}

func (server *Server) dispatchTargets(request DispatchRequest, response http.ResponseWriter) ([]Target, bool) {
	if strings.TrimSpace(request.MachineID) != "" {
		target, ok := server.target(request.MachineID)
		if !ok {
			writeJSON(response, http.StatusNotFound, map[string]string{"error": "target is unavailable"})
			return nil, false
		}
		if !targetSupportsJob(target, request.JobID) {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "target does not advertise job"})
			return nil, false
		}
		if err := targetEligibilityError(target); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "target is unsupported: " + err.Error()})
			return nil, false
		}
		if err := targetConstraintError(target, request); err != nil {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "target does not satisfy constraints: " + err.Error()})
			return nil, false
		}
		if err := targetLoadError(target); err != nil {
			writeJSON(response, http.StatusConflict, map[string]string{"error": err.Error()})
			return nil, false
		}
		return []Target{target}, true
	}

	targets := []Target{}
	for _, target := range server.eligibleTargets() {
		if targetSupportsJob(target, request.JobID) && targetConstraintError(target, request) == nil && targetLoadError(target) == nil {
			targets = append(targets, target)
		}
	}
	if len(targets) > 0 {
		sortTargetsByLoad(targets)
		return targets, true
	}
	writeJSON(response, http.StatusNotFound, map[string]string{"error": "no eligible active target advertises job and satisfies constraints"})
	return nil, false
}

func sortTargetsByLoad(targets []Target) {
	sort.SliceStable(targets, func(i, j int) bool {
		left := targets[i]
		right := targets[j]
		if left.ActiveBuilds != right.ActiveBuilds {
			return left.ActiveBuilds < right.ActiveBuilds
		}
		if left.QueuedBuilds != right.QueuedBuilds {
			return left.QueuedBuilds < right.QueuedBuilds
		}
		return left.MachineID < right.MachineID
	})
}

func targetSupportsJob(target Target, jobID string) bool {
	for _, candidate := range target.Jobs {
		if candidate == jobID {
			return true
		}
	}
	return false
}

func targetLoadError(target Target) error {
	if !targetAcceptingBuilds(target) {
		return errors.New("target is paused")
	}
	if target.QueuedBuildLimit > 0 && target.QueuedBuilds >= target.QueuedBuildLimit {
		return fmt.Errorf("target queue is full (%d/%d queued)", target.QueuedBuilds, target.QueuedBuildLimit)
	}
	return nil
}

func targetAcceptingBuilds(target Target) bool {
	return target.AcceptingBuilds == nil || *target.AcceptingBuilds
}

func targetEligibilityError(target Target) error {
	if strings.TrimSpace(target.PublicURL) == "" && strings.TrimSpace(target.Listen) == "" {
		return errors.New("target must include public_url or loopback listen address")
	}
	if strings.TrimSpace(target.Listen) != "" && !targetListenIsLoopback(target.Listen) {
		return fmt.Errorf("target listen %q must be loopback", target.Listen)
	}
	if strings.TrimSpace(target.PublicURL) != "" {
		if err := endpoint.ValidateBaseURL(target.PublicURL, "target public_url"); err != nil {
			return err
		}
	}
	for _, jobID := range target.Jobs {
		if err := requestmeta.ValidateJobID(jobID); err != nil {
			return fmt.Errorf("target jobs entry %q is invalid: %w", jobID, err)
		}
	}
	if target.ActiveBuilds < 0 {
		return fmt.Errorf("target active_builds %d is invalid; active_builds must be non-negative", target.ActiveBuilds)
	}
	if target.QueuedBuilds < 0 {
		return fmt.Errorf("target queued_builds %d is invalid; queued_builds must be non-negative", target.QueuedBuilds)
	}
	if target.QueuedBuildLimit < 0 {
		return fmt.Errorf("target queued_build_limit %d is invalid; queued_build_limit must be non-negative", target.QueuedBuildLimit)
	}
	capabilities := target.Capabilities
	if capabilities.Empty() {
		return errors.New("target capabilities are required; Transwarp targets modern Apple Silicon Macs")
	}
	if capabilities.OS == "" {
		return errors.New("target OS is required; Transwarp targets modern macOS desktops")
	}
	if !strings.EqualFold(capabilities.OS, "macOS") && !strings.EqualFold(capabilities.OS, "darwin") {
		return fmt.Errorf("target OS %q is unsupported; Transwarp targets modern macOS desktops", capabilities.OS)
	}
	if capabilities.Architecture == "" {
		return errors.New("target architecture is required; Transwarp targets Apple Silicon Macs")
	}
	if capabilities.Architecture != "arm64" {
		return fmt.Errorf("target architecture %q is unsupported; Transwarp targets Apple Silicon Macs", capabilities.Architecture)
	}
	if capabilities.OSVersion == "" {
		return fmt.Errorf("target macOS version is required; Transwarp requires macOS %d or newer", minimumSupportedMacOSMajor)
	}
	major, ok := macOSMajorVersion(capabilities.OSVersion)
	if !ok {
		return fmt.Errorf("target macOS version %q could not be parsed; Transwarp requires macOS %d or newer", capabilities.OSVersion, minimumSupportedMacOSMajor)
	}
	if major < minimumSupportedMacOSMajor {
		return fmt.Errorf("target macOS version %q is unsupported; Transwarp requires macOS %d or newer", capabilities.OSVersion, minimumSupportedMacOSMajor)
	}
	return nil
}

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

func targetListenIsLoopback(listen string) bool {
	return strings.HasPrefix(listen, "127.0.0.1:") || strings.HasPrefix(listen, "localhost:")
}

func normalizeTargetLease(target Target, now time.Time) Target {
	if target.LeaseExpiresAt.IsZero() {
		target.LeaseExpiresAt = now.Add(defaultTargetLease)
		return target
	}
	maxLeaseExpiresAt := now.Add(maxTargetLease)
	if target.LeaseExpiresAt.After(maxLeaseExpiresAt) {
		target.LeaseExpiresAt = maxLeaseExpiresAt
	}
	return target
}

func targetConstraintError(target Target, request DispatchRequest) error {
	capabilities := target.Capabilities
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

func sameDispatchRequest(left DispatchRequest, right DispatchRequest) bool {
	return left.MachineID == right.MachineID &&
		left.JobID == right.JobID &&
		left.RequestID == right.RequestID &&
		left.RepoURL == right.RepoURL &&
		left.Ref == right.Ref &&
		left.Commit == right.Commit &&
		left.MinCPUCount == right.MinCPUCount &&
		left.MinMemoryBytes == right.MinMemoryBytes &&
		left.MinXcodeVersion == right.MinXcodeVersion
}

func validateDispatchRequest(request DispatchRequest) error {
	if err := requestmeta.ValidateJobID(request.JobID); err != nil {
		return err
	}
	if strings.TrimSpace(request.RequestID) == "" {
		return errors.New("request_id is required")
	}
	if request.MinCPUCount < 0 {
		return errors.New("min_cpu_count must not be negative")
	}
	if strings.TrimSpace(request.MinXcodeVersion) != "" {
		if _, err := parseDottedVersion(request.MinXcodeVersion); err != nil {
			return fmt.Errorf("min_xcode_version %q is invalid", request.MinXcodeVersion)
		}
	}
	if strings.TrimSpace(request.MachineID) != "" {
		if err := requestmeta.ValidateMachineID(request.MachineID); err != nil {
			return err
		}
	}
	if err := requestmeta.Validate(request.RequestID, request.RepoURL, request.Ref, request.Commit); err != nil {
		return err
	}
	return nil
}

func sameDispatchResult(request DispatchRequest, result BuildResult) bool {
	if request.JobID != result.JobID ||
		request.RequestID != result.RequestID ||
		request.RepoURL != result.RepoURL ||
		request.Ref != result.Ref ||
		request.Commit != result.Commit {
		return false
	}
	if strings.TrimSpace(request.MachineID) != "" && request.MachineID != result.MachineID {
		return false
	}
	return true
}

func sameBuildResult(left BuildResult, right BuildResult) bool {
	return left.BuildID == right.BuildID &&
		left.JobID == right.JobID &&
		left.RequestID == right.RequestID &&
		left.MachineID == right.MachineID &&
		left.Machine == right.Machine &&
		left.RepoURL == right.RepoURL &&
		left.Ref == right.Ref &&
		left.Commit == right.Commit &&
		left.StartedAt.Equal(right.StartedAt) &&
		left.EndedAt.Equal(right.EndedAt) &&
		left.ExitCode == right.ExitCode &&
		left.Status == right.Status &&
		left.Error == right.Error &&
		left.PublicURL == right.PublicURL &&
		left.DurationMS == right.DurationMS
}

func normalizeBaseURL(rawURL string) string {
	return strings.TrimRight(strings.TrimSpace(rawURL), "/")
}

func selectedRunnerRequiresPublicURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	return !endpoint.IsLoopbackHost(parsed.Hostname())
}

func validBuildResultStatus(status string) bool {
	switch status {
	case "passed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func validateBuildResultConsistency(result BuildResult) error {
	if strings.TrimSpace(result.PublicURL) != "" {
		if err := endpoint.ValidateBaseURL(result.PublicURL, "result public_url"); err != nil {
			return err
		}
	}
	if result.DurationMS < 0 {
		return errors.New("duration_ms must not be negative")
	}
	startedAtRecorded := !result.StartedAt.IsZero()
	endedAtRecorded := !result.EndedAt.IsZero()
	if startedAtRecorded != endedAtRecorded {
		return errors.New("started_at and ended_at must be recorded together")
	}
	if startedAtRecorded {
		if result.EndedAt.Before(result.StartedAt) {
			return errors.New("ended_at must not be before started_at")
		}
		expectedDurationMS := result.EndedAt.Sub(result.StartedAt).Milliseconds()
		if absInt64(result.DurationMS-expectedDurationMS) > resultDurationToleranceMS {
			return errors.New("duration_ms must match started_at and ended_at")
		}
	}

	switch result.Status {
	case "passed":
		if result.ExitCode != 0 || strings.TrimSpace(result.Error) != "" {
			return errors.New("passed results must have exit_code 0 and no error")
		}
	case "failed":
		if result.ExitCode == 0 && strings.TrimSpace(result.Error) == "" {
			return errors.New("failed results must include a nonzero exit_code or error")
		}
	case "canceled":
		if result.Error != "build canceled" {
			return errors.New(`canceled results must use error "build canceled"`)
		}
	}
	return nil
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

type stateFile struct {
	Targets map[string]Target         `json:"targets"`
	Results map[string]BuildResult    `json:"results"`
	Active  map[string]activeDispatch `json:"active,omitempty"`
}

func (server *Server) loadState() error {
	if server.statePath == "" {
		return nil
	}

	data, err := os.ReadFile(server.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load coordinator state: %w", err)
	}

	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode coordinator state: %w", err)
	}
	changed := false
	if state.Targets != nil {
		var targetsChanged bool
		server.targets, targetsChanged = normalizeLoadedTargets(state.Targets, time.Now())
		changed = changed || targetsChanged
	}
	if state.Results != nil {
		var resultsChanged bool
		server.results, resultsChanged = normalizeLoadedResults(state.Results)
		changed = changed || resultsChanged
	}
	if state.Active != nil {
		var activeChanged bool
		server.active, activeChanged = normalizeLoadedActiveDispatches(state.Active, server.results)
		changed = changed || activeChanged
	}
	if changed {
		if err := server.saveStateLocked(); err != nil {
			return fmt.Errorf("rewrite coordinator state: %w", err)
		}
	}
	return nil
}

func normalizeLoadedTargets(targets map[string]Target, now time.Time) (map[string]Target, bool) {
	normalized := map[string]Target{}
	changed := false
	for id, target := range targets {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(target.MachineID) == "" {
			changed = true
			continue
		}
		if id != target.MachineID {
			changed = true
			continue
		}
		if !target.LeaseExpiresAt.IsZero() && target.LeaseExpiresAt.Before(now) {
			changed = true
			continue
		}
		if err := targetEligibilityError(target); err != nil {
			changed = true
			continue
		}
		normalizedTarget := normalizeTargetLease(target, now)
		if !normalizedTarget.LeaseExpiresAt.Equal(target.LeaseExpiresAt) {
			changed = true
		}
		normalized[id] = normalizedTarget
	}
	return normalized, changed
}

func normalizeLoadedResults(results map[string]BuildResult) (map[string]BuildResult, bool) {
	normalized := map[string]BuildResult{}
	changed := false
	for id, result := range results {
		if id != result.RequestID {
			changed = true
			continue
		}
		if err := requestmeta.ValidateRequestID(result.RequestID); err != nil {
			changed = true
			continue
		}
		if err := requestmeta.ValidateBuildID(result.BuildID); err != nil {
			changed = true
			continue
		}
		if err := requestmeta.ValidateJobID(result.JobID); err != nil {
			changed = true
			continue
		}
		if err := requestmeta.ValidateMachineID(result.MachineID); err != nil {
			changed = true
			continue
		}
		if !validBuildResultStatus(result.Status) {
			changed = true
			continue
		}
		if err := validateBuildResultConsistency(result); err != nil {
			changed = true
			continue
		}
		normalized[id] = result
	}
	return normalized, changed
}

func normalizeLoadedActiveDispatches(activeDispatches map[string]activeDispatch, results map[string]BuildResult) (map[string]activeDispatch, bool) {
	normalized := map[string]activeDispatch{}
	changed := false
	for id, active := range activeDispatches {
		if id != active.Request.RequestID {
			changed = true
			continue
		}
		if _, resultRecorded := results[id]; resultRecorded {
			changed = true
			continue
		}
		if err := validateDispatchRequest(active.Request); err != nil {
			changed = true
			continue
		}
		if strings.TrimSpace(active.TargetMachineID) == "" || strings.TrimSpace(active.RunnerBaseURL) == "" || strings.TrimSpace(active.RunnerBuildID) == "" {
			changed = true
			continue
		}
		if err := requestmeta.ValidateMachineID(active.TargetMachineID); err != nil {
			changed = true
			continue
		}
		if strings.TrimSpace(active.Request.MachineID) != "" && active.Request.MachineID != active.TargetMachineID {
			changed = true
			continue
		}
		if err := endpoint.ValidateBaseURL(active.RunnerBaseURL, "active runner base URL"); err != nil {
			changed = true
			continue
		}
		if err := requestmeta.ValidateBuildID(active.RunnerBuildID); err != nil {
			changed = true
			continue
		}
		normalized[id] = active
	}
	return normalized, changed
}

func (server *Server) saveStateLocked() error {
	if server.statePath == "" {
		return nil
	}

	state := stateFile{
		Targets: server.targets,
		Results: server.results,
		Active:  persistedActiveDispatches(server.active),
	}
	data, err := json.MarshalIndent(state, "", "\t")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(server.statePath), 0o700); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(server.statePath), ".transwarp-state-*.tmp")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempName)
		}
	}()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tempName, server.statePath); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func persistedActiveDispatches(activeDispatches map[string]activeDispatch) map[string]activeDispatch {
	persisted := map[string]activeDispatch{}
	for requestID, active := range activeDispatches {
		if strings.TrimSpace(active.RunnerBaseURL) == "" || strings.TrimSpace(active.RunnerBuildID) == "" {
			continue
		}
		persisted[requestID] = active
	}
	if len(persisted) == 0 {
		return nil
	}
	return persisted
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

type flushWriter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
}

func (writer flushWriter) Write(data []byte) (int, error) {
	n, err := writer.writer.Write(data)
	if writer.flusher != nil {
		writer.flusher.Flush()
	}
	return n, err
}

func Serve(ctx context.Context, address string, server *Server) error {
	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errs <- err
		}
		close(errs)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errs:
		return err
	}
}

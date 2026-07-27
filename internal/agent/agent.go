package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Agent struct {
	config   Config
	server   *http.Server
	tunnel   *Tunnel
	redactor Redactor

	buildsMu       sync.Mutex
	builds         map[string]*BuildRun
	activeBuildID  string
	queuedBuildIDs []string

	registrationMu      sync.Mutex
	registrationStatus  RegistrationStatus
	registrationRefresh chan struct{}

	acceptingBuilds bool
}

var registrationTunnelReadyTimeout = 30 * time.Second
var registrationTunnelRetryDelay = 5 * time.Second
var buildShutdownTimeout = 15 * time.Second
var deregistrationShutdownTimeout = 10 * time.Second

func New(config Config) *Agent {
	config = ensureCapabilities(config)
	agent := &Agent{
		config:              config,
		redactor:            NewRedactor(config),
		builds:              map[string]*BuildRun{},
		registrationStatus:  initialRegistrationStatus(config),
		registrationRefresh: make(chan struct{}, 1),
		acceptingBuilds:     true,
	}
	agent.tunnel = NewTunnel(config)
	return agent
}

func (agent *Agent) Run(ctx context.Context) error {
	if err := ValidateSupportedHost(agent.config.Capabilities); err != nil {
		agent.updateRegistrationStatus("failed", "", nil, err)
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", agent.health)
	mux.HandleFunc("GET /status", agent.auth(agent.status))
	mux.HandleFunc("POST /v1/builds", agent.auth(agent.startBuild))
	mux.HandleFunc("GET /v1/builds/{buildID}", agent.auth(agent.buildStatus))
	mux.HandleFunc("GET /v1/builds/{buildID}/logs", agent.auth(agent.tailBuildLogs))
	mux.HandleFunc("POST /v1/builds/{buildID}/cancel", agent.auth(agent.cancelBuild))
	mux.HandleFunc("POST /v1/availability", agent.auth(agent.setAvailability))

	agent.server = &http.Server{
		Addr:              agent.config.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		Emit(Event{Kind: "info", Message: "listening on http://" + agent.config.ListenAddress})
		if err := agent.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
		close(serverErrors)
	}()

	tunnel := agent.tunnel
	if err := tunnel.Start(ctx); err != nil {
		agent.updateRegistrationStatus("failed", "", nil, err)
		agent.shutdownServer()
		return err
	}
	defer tunnel.Stop()
	tunnelErrors := tunnel.Done()
	if !tunnelRequiresProcess(agent.config.Tunnel.Mode) {
		tunnelErrors = nil
	}

	registrationConfig := agent.config
	if registrationConfig.CIRegistrationURL != "" {
		registrar, err := agent.registerWhenTunnelReadyOrServerFails(ctx, tunnel, registrationConfig, serverErrors, tunnelErrors)
		if err != nil {
			agent.shutdownBuilds(buildShutdownTimeout)
			agent.shutdownServer()
			if errors.Is(err, context.Canceled) {
				Emit(Event{Kind: "info", Message: "shutdown complete"})
				return nil
			}
			return err
		}
		heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			registrar.RunHeartbeat(heartbeatCtx)
		}()
		defer func() {
			stopHeartbeat()
			<-heartbeatDone
			deregisterCtx, cancel := context.WithTimeout(context.Background(), deregistrationShutdownTimeout)
			defer cancel()
			registrar.DeregisterWithRetry(deregisterCtx, time.Second)
		}()
	} else {
		agent.updateRegistrationStatus("disabled", "", nil, nil)
		Emit(Event{Kind: "registration", Message: "registration URL not configured"})
	}

	select {
	case <-ctx.Done():
		agent.shutdownBuilds(buildShutdownTimeout)
		agent.shutdownServer()
		Emit(Event{Kind: "info", Message: "shutdown complete"})
		return nil
	case err := <-tunnelErrors:
		err = tunnelStoppedError(err, "tunnel stopped unexpectedly")
		agent.updateRegistrationStatus("failed", "", nil, err)
		agent.shutdownBuilds(buildShutdownTimeout)
		agent.shutdownServer()
		return err
	case err := <-serverErrors:
		if err != nil {
			agent.shutdownBuilds(buildShutdownTimeout)
			return fmt.Errorf("server failed: %w", err)
		}
		return nil
	}
}

func (agent *Agent) registerWhenTunnelReadyOrServerFails(ctx context.Context, tunnel *Tunnel, registrationConfig Config, serverErrors <-chan error, tunnelErrors <-chan error) (*Registrar, error) {
	registrationCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type registrationResult struct {
		registrar *Registrar
		err       error
	}
	done := make(chan registrationResult, 1)
	go func() {
		registrar, err := agent.registerWhenTunnelReady(registrationCtx, tunnel, registrationConfig)
		done <- registrationResult{registrar: registrar, err: err}
	}()

	select {
	case result := <-done:
		return result.registrar, result.err
	case err, ok := <-serverErrors:
		if ok && err != nil {
			return nil, fmt.Errorf("server failed: %w", err)
		}
		return nil, errors.New("server stopped before registration")
	case err := <-tunnelErrors:
		return nil, tunnelStoppedError(err, "tunnel stopped before registration")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (agent *Agent) registerWhenTunnelReady(ctx context.Context, tunnel *Tunnel, registrationConfig Config) (*Registrar, error) {
	for {
		agent.updateRegistrationStatus("waiting_for_tunnel", "", nil, nil)
		if err := tunnel.WaitReady(ctx, registrationTunnelReadyTimeout); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			agent.updateRegistrationStatus("waiting_for_tunnel", "", nil, err)
			Emit(Event{Kind: "registration", Message: "registration waiting for tunnel readiness: " + err.Error()})
			timer := time.NewTimer(registrationTunnelRetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}

		registrationConfig.Tunnel.PublicURL = agent.publicURL()
		registrar := NewRegistrar(registrationConfig)
		registrar.onPost = agent.recordRegistrationPost
		registrar.load = agent.buildLoad
		registrar.refresh = agent.registrationRefresh
		agent.updateRegistrationStatus("registering", "", nil, nil)
		if err := registrar.RegisterWithRetry(ctx, 5*time.Second); err != nil {
			return nil, err
		}
		return registrar, nil
	}
}

func (agent *Agent) shutdownServer() {
	if agent.server == nil {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = agent.server.Shutdown(shutdownCtx)
}

func (agent *Agent) shutdownBuilds(timeout time.Duration) {
	runs := agent.openBuilds()
	if len(runs) == 0 {
		return
	}

	Emit(Event{Kind: "build", Message: fmt.Sprintf("canceling %d build%s before shutdown", len(runs), plural(len(runs)))})
	for _, run := range runs {
		if !run.Done() && !run.HasTerminalRecord() {
			agent.cancelBuildRun(run, "runner shutting down; canceling build")
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for _, run := range runs {
		if run.Closed() {
			continue
		}
		select {
		case <-run.closed:
		case <-timer.C:
			Emit(Event{Kind: "error", Message: "timed out waiting for builds to stop before shutdown"})
			return
		}
	}
}

func (agent *Agent) openBuilds() []*BuildRun {
	agent.buildsMu.Lock()
	defer agent.buildsMu.Unlock()

	runs := []*BuildRun{}
	for _, run := range agent.builds {
		if !run.Closed() {
			runs = append(runs, run)
		}
	}
	return runs
}

func (agent *Agent) publicURL() string {
	publicURL := agent.config.Tunnel.PublicURL
	if publicURL != "" {
		return publicURL
	}

	if agent.tunnel == nil {
		return ""
	}
	return agent.tunnel.Status().PublicURL
}

func (agent *Agent) activeBuildCount() int {
	agent.buildsMu.Lock()
	defer agent.buildsMu.Unlock()

	if agent.activeBuildID != "" {
		return 1
	}
	return 0
}

func (agent *Agent) queuedBuildCount() int {
	agent.buildsMu.Lock()
	defer agent.buildsMu.Unlock()

	return len(agent.queuedBuildIDs)
}

func (agent *Agent) queuedBuildLimit() int {
	return maxQueuedBuilds
}

func (agent *Agent) buildLoad() BuildLoad {
	agent.buildsMu.Lock()
	acceptingBuilds := agent.acceptingNewBuildsLocked()
	defer agent.buildsMu.Unlock()

	load := BuildLoad{
		AcceptingBuilds:  acceptingBuilds && agent.tunnelAvailableForRegistration(),
		QueuedBuilds:     len(agent.queuedBuildIDs),
		QueuedBuildLimit: agent.queuedBuildLimit(),
	}
	if agent.activeBuildID != "" {
		load.ActiveBuilds = 1
	}
	return load
}

func (agent *Agent) tunnelAvailableForRegistration() bool {
	if agent.tunnel == nil || !tunnelRequiresProcess(agent.config.Tunnel.Mode) {
		return true
	}
	return agent.tunnel.Status().Ready
}

func (agent *Agent) acceptingNewBuilds() bool {
	agent.buildsMu.Lock()
	defer agent.buildsMu.Unlock()
	return agent.acceptingNewBuildsLocked()
}

func (agent *Agent) acceptingNewBuildsLocked() bool {
	return agent.acceptingBuilds
}

func (agent *Agent) setAcceptingBuilds(accepting bool) {
	agent.buildsMu.Lock()
	agent.acceptingBuilds = accepting
	agent.buildsMu.Unlock()
	agent.requestRegistrationRefresh()
}

func (agent *Agent) requestRegistrationRefresh() {
	if agent.config.CIRegistrationURL == "" {
		return
	}
	select {
	case agent.registrationRefresh <- struct{}{}:
	default:
	}
}

func initialRegistrationStatus(config Config) RegistrationStatus {
	if config.CIRegistrationURL == "" {
		return RegistrationStatus{Configured: false, State: "disabled"}
	}
	return RegistrationStatus{Configured: true, State: "starting"}
}

func tunnelRequiresProcess(mode string) bool {
	return mode == "quick" || mode == "named"
}

func tunnelStoppedError(err error, fallback string) error {
	if err != nil {
		return err
	}
	return errors.New(fallback)
}

func (agent *Agent) updateRegistrationStatus(state string, action string, payload *RegistrationPayload, err error) {
	agent.registrationMu.Lock()
	defer agent.registrationMu.Unlock()

	status := agent.registrationStatus
	status.Configured = agent.config.CIRegistrationURL != ""
	status.State = state
	if action != "" {
		status.LastAction = action
	}
	if err != nil {
		status.LastError = agent.redactor.Redact(err.Error())
	} else if state != "heartbeat_failed" && state != "failed" {
		status.LastError = ""
	}
	if payload != nil {
		now := time.Now()
		status.LastSuccessAt = &now
		status.LeaseExpiresAt = &payload.LeaseExpiresAt
	}
	agent.registrationStatus = status
}

func (agent *Agent) recordRegistrationPost(action string, payload RegistrationPayload, err error) {
	if err != nil {
		state := "failed"
		if action == "heartbeat" {
			state = "heartbeat_failed"
		}
		agent.updateRegistrationStatus(state, action, nil, err)
		return
	}

	state := "registered"
	if action == "deregister" {
		state = "deregistered"
	}
	agent.updateRegistrationStatus(state, action, &payload, nil)
}

func (agent *Agent) registration() RegistrationStatus {
	agent.registrationMu.Lock()
	defer agent.registrationMu.Unlock()
	return agent.registrationStatus
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

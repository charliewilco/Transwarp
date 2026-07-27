package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type Registrar struct {
	config   Config
	client   *http.Client
	redactor Redactor
	onPost   func(action string, payload RegistrationPayload, err error)
	load     func() BuildLoad
	refresh  <-chan struct{}
}

type RegistrationPayload struct {
	MachineID        string       `json:"machine_id"`
	MachineName      string       `json:"machine_name"`
	Listen           string       `json:"listen"`
	PublicURL        string       `json:"public_url,omitempty"`
	Capabilities     Capabilities `json:"capabilities"`
	AcceptingBuilds  bool         `json:"accepting_builds"`
	ActiveBuilds     int          `json:"active_builds"`
	QueuedBuilds     int          `json:"queued_builds"`
	QueuedBuildLimit int          `json:"queued_build_limit"`
	Jobs             []string     `json:"jobs"`
	LeaseExpiresAt   time.Time    `json:"lease_expires_at"`
}

type BuildLoad struct {
	AcceptingBuilds  bool
	ActiveBuilds     int
	QueuedBuilds     int
	QueuedBuildLimit int
}

const minimumRegistrationLease = 30 * time.Second

type RegistrationStatus struct {
	Configured     bool       `json:"configured"`
	State          string     `json:"state"`
	LastAction     string     `json:"last_action,omitempty"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

func NewRegistrar(config Config) *Registrar {
	config = ensureCapabilities(config)
	return &Registrar{
		config:   config,
		client:   newCIHTTPClient(15 * time.Second),
		redactor: NewRedactor(config),
	}
}

func (registrar *Registrar) Configured() bool {
	return registrar.config.CIRegistrationURL != ""
}

func (registrar *Registrar) Register(ctx context.Context) error {
	if registrar.config.CIRegistrationURL == "" {
		return nil
	}

	payload, err := registrar.post(ctx, registrar.config.CIRegistrationURL)
	registrar.notify("register", payload, err)
	if err != nil {
		return err
	}

	Emit(Event{Kind: "registration", Message: "registered " + registrar.config.MachineName})
	return nil
}

func (registrar *Registrar) RegisterWithRetry(ctx context.Context, retryDelay time.Duration) error {
	if retryDelay <= 0 {
		retryDelay = 5 * time.Second
	}

	for {
		if err := registrar.Register(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			Emit(Event{Kind: "registration", Message: "registration failed: " + registrar.errorMessage(err)})
		} else {
			return nil
		}

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (registrar *Registrar) RunHeartbeat(ctx context.Context) {
	endpoint := registrar.config.CIHeartbeatURL
	if endpoint == "" {
		endpoint = registrar.config.CIRegistrationURL
	}
	if endpoint == "" {
		return
	}

	interval := registrar.config.HeartbeatSeconds
	if interval == 0 {
		interval = 30
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-registrar.refresh:
			registrar.postHeartbeat(ctx, endpoint)
		case <-ticker.C:
			registrar.postHeartbeat(ctx, endpoint)
		}
	}
}

func (registrar *Registrar) postHeartbeat(ctx context.Context, endpoint string) {
	payload, err := registrar.post(ctx, endpoint)
	registrar.notify("heartbeat", payload, err)
	if err != nil {
		Emit(Event{Kind: "registration", Message: "heartbeat failed: " + registrar.errorMessage(err)})
	} else {
		Emit(Event{Kind: "registration", Message: "heartbeat sent"})
	}
}

func (registrar *Registrar) Deregister(ctx context.Context) error {
	if registrar.config.CIDeregistrationURL == "" {
		return nil
	}

	payload, err := registrar.post(ctx, registrar.config.CIDeregistrationURL)
	registrar.notify("deregister", payload, err)
	if err != nil {
		Emit(Event{Kind: "registration", Message: "deregistration failed: " + registrar.errorMessage(err)})
		return err
	}

	Emit(Event{Kind: "registration", Message: "deregistered " + registrar.config.MachineName})
	return nil
}

func (registrar *Registrar) DeregisterWithRetry(ctx context.Context, retryDelay time.Duration) {
	if retryDelay <= 0 {
		retryDelay = time.Second
	}

	for {
		if err := registrar.Deregister(ctx); err == nil {
			return
		}
		if ctx.Err() != nil {
			return
		}

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (registrar *Registrar) notify(action string, payload RegistrationPayload, err error) {
	if registrar.onPost != nil {
		registrar.onPost(action, payload, err)
	}
}

func (registrar *Registrar) errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return registrar.redactor.Redact(err.Error())
}

func (registrar *Registrar) post(ctx context.Context, endpoint string) (RegistrationPayload, error) {
	jobs := make([]string, 0, len(registrar.config.Jobs))
	for _, job := range registrar.config.Jobs {
		jobs = append(jobs, job.ID)
	}
	load := registrar.currentLoad()

	payload := RegistrationPayload{
		MachineID:        registrar.config.MachineID,
		MachineName:      registrar.config.MachineName,
		Listen:           registrar.config.ListenAddress,
		PublicURL:        registrar.config.Tunnel.PublicURL,
		Capabilities:     registrar.config.Capabilities,
		AcceptingBuilds:  load.AcceptingBuilds,
		ActiveBuilds:     load.ActiveBuilds,
		QueuedBuilds:     load.QueuedBuilds,
		QueuedBuildLimit: load.QueuedBuildLimit,
		Jobs:             jobs,
		LeaseExpiresAt:   time.Now().Add(registrar.leaseDuration()),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return payload, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return payload, err
	}
	request.Header.Set("Content-Type", "application/json")
	if registrar.config.RegistrationToken != "" {
		request.Header.Set("Authorization", "Bearer "+registrar.config.RegistrationToken)
	}
	if registrar.config.CIAccessClientID != "" {
		request.Header.Set("CF-Access-Client-Id", registrar.config.CIAccessClientID)
		request.Header.Set("CF-Access-Client-Secret", registrar.config.CIAccessClientSecret)
	}

	response, err := registrar.client.Do(request)
	if err != nil {
		return payload, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return payload, &registrationError{status: response.Status, body: strings.TrimSpace(string(data))}
	}
	return payload, nil
}

func (registrar *Registrar) currentLoad() BuildLoad {
	if registrar.load == nil {
		return BuildLoad{AcceptingBuilds: true}
	}
	return registrar.load()
}

func (registrar *Registrar) leaseDuration() time.Duration {
	heartbeat := registrar.config.HeartbeatSeconds
	if heartbeat == 0 {
		heartbeat = 30
	}
	duration := time.Duration(heartbeat*3) * time.Second
	if duration < minimumRegistrationLease {
		return minimumRegistrationLease
	}
	return duration
}

type registrationError struct {
	status string
	body   string
}

func (err *registrationError) Error() string {
	message := "registration endpoint returned " + err.status
	if err.body != "" {
		message += ": " + err.body
	}
	return message
}

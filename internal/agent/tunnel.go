package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charliewilco/transwarp/internal/tunnelnet"
)

var tryCloudflareURLPattern = regexp.MustCompile(`https://[-a-zA-Z0-9.]+\.trycloudflare\.com`)
var lookupTunnelHost = tunnelnet.LookupHost
var tunnelReadinessRequiredSuccesses = 3

type Tunnel struct {
	config Config

	mu       sync.Mutex
	command  *exec.Cmd
	status   TunnelStatus
	done     chan error
	redactor Redactor
}

type TunnelStatus struct {
	Mode           string `json:"mode"`
	State          string `json:"state"`
	Origin         string `json:"origin,omitempty"`
	PublicURL      string `json:"public_url,omitempty"`
	Connected      bool   `json:"connected"`
	Ready          bool   `json:"ready"`
	PID            int    `json:"pid,omitempty"`
	Error          string `json:"error,omitempty"`
	ReadinessError string `json:"readiness_error,omitempty"`
}

func NewTunnel(config Config) *Tunnel {
	mode := config.Tunnel.Mode
	if mode == "" {
		mode = "off"
	}

	return &Tunnel{
		config:   config,
		done:     make(chan error, 1),
		redactor: NewRedactor(config),
		status: TunnelStatus{
			Mode:  mode,
			State: "stopped",
		},
	}
}

func (tunnel *Tunnel) Start(ctx context.Context) error {
	mode := tunnel.config.Tunnel.Mode
	if mode == "" || mode == "off" {
		tunnel.setStatus(TunnelStatus{Mode: "off", State: "disabled", PublicURL: tunnel.config.Tunnel.PublicURL})
		Emit(Event{Kind: "tunnel", Message: "tunnel disabled"})
		return nil
	}

	origin := "http://" + tunnel.config.ListenAddress
	tunnel.setStatus(TunnelStatus{Mode: mode, State: "starting", Origin: origin, PublicURL: tunnel.config.Tunnel.PublicURL})
	args := []string{"tunnel", "--no-autoupdate"}
	switch mode {
	case "quick":
		args = append(args, "--url", origin)
	case "named":
		args = append(args, "run")
	default:
		return fmt.Errorf("unsupported tunnel mode %q", mode)
	}

	executable, err := tunnel.cloudflaredExecutable()
	if err != nil {
		tunnel.setStatus(TunnelStatus{Mode: mode, State: "failed", Origin: origin, PublicURL: tunnel.config.Tunnel.PublicURL, Error: err.Error()})
		return err
	}

	command := exec.CommandContext(ctx, executable, args...)
	command.Env = cloudflaredEnvironment(tunnel.config.Tunnel)
	stdout, err := command.StdoutPipe()
	if err != nil {
		tunnel.setStatus(TunnelStatus{Mode: mode, State: "failed", Origin: origin, PublicURL: tunnel.config.Tunnel.PublicURL, Error: err.Error()})
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		tunnel.setStatus(TunnelStatus{Mode: mode, State: "failed", Origin: origin, PublicURL: tunnel.config.Tunnel.PublicURL, Error: err.Error()})
		return err
	}

	if err := command.Start(); err != nil {
		tunnel.setStatus(TunnelStatus{Mode: mode, State: "failed", Origin: origin, PublicURL: tunnel.config.Tunnel.PublicURL, Error: err.Error()})
		return fmt.Errorf("start cloudflared: %w", err)
	}
	tunnel.command = command
	tunnel.setStatus(TunnelStatus{Mode: mode, State: "running", Origin: origin, PublicURL: tunnel.config.Tunnel.PublicURL, PID: command.Process.Pid})
	Emit(Event{Kind: "tunnel", Message: "started cloudflared for " + origin})

	go tunnel.scan("tunnel", stdout)
	go tunnel.scan("tunnel", stderr)
	go tunnel.monitorReadiness(ctx)
	go func() {
		err := command.Wait()
		if err != nil && ctx.Err() == nil {
			tunnel.updateStatus(func(status *TunnelStatus) {
				status.State = "failed"
				status.Error = err.Error()
				status.Ready = false
			})
			Emit(Event{Kind: "tunnel", Message: "cloudflared exited: " + err.Error()})
		}
		if err == nil || ctx.Err() != nil {
			tunnel.updateStatus(func(status *TunnelStatus) {
				status.State = "stopped"
				status.Connected = false
				status.Ready = false
				status.PID = 0
			})
		}
		tunnel.notifyDone(unexpectedTunnelExitError(ctx, err))
	}()

	return nil
}

func (tunnel *Tunnel) Stop() {
	if tunnel.command == nil || tunnel.command.Process == nil {
		return
	}

	_ = tunnel.command.Process.Signal(defaultInterruptSignal())
	Emit(Event{Kind: "tunnel", Message: "stopped cloudflared"})
}

func (tunnel *Tunnel) Done() <-chan error {
	return tunnel.done
}

func (tunnel *Tunnel) notifyDone(err error) {
	select {
	case tunnel.done <- err:
	default:
	}
	close(tunnel.done)
}

func unexpectedTunnelExitError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cloudflared exited: %w", err)
	}
	return errors.New("cloudflared exited unexpectedly")
}

func (tunnel *Tunnel) scan(kind string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if publicURL := tryCloudflareURLPattern.FindString(line); publicURL != "" {
			tunnel.updateStatus(func(status *TunnelStatus) {
				if status.Mode == "quick" {
					status.PublicURL = publicURL
					status.Ready = false
				}
			})
		}
		if strings.Contains(line, "Registered tunnel connection") {
			tunnel.updateStatus(func(status *TunnelStatus) {
				status.Connected = true
			})
		}
		Emit(Event{Kind: kind, Message: tunnel.redactor.Redact(line)})
	}
}

func (tunnel *Tunnel) monitorReadiness(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	successes := 0
	for {
		status := tunnel.Status()
		readinessError := tunnel.readinessError(ctx, status)
		if readinessError == "" {
			successes++
			if successes >= tunnelReadinessSuccessTarget(status.Mode) && !status.Ready {
				tunnel.updateStatus(func(status *TunnelStatus) {
					status.Ready = true
					status.ReadinessError = ""
				})
				Emit(Event{Kind: "tunnel", Message: "tunnel ready at " + status.PublicURL})
			}
		} else if status.Ready || status.ReadinessError != readinessError {
			successes = 0
			tunnel.updateStatus(func(status *TunnelStatus) {
				status.Ready = false
				status.ReadinessError = readinessError
			})
		} else {
			successes = 0
		}
		if status.State == "failed" || status.State == "stopped" || status.State == "disabled" {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (tunnel *Tunnel) WaitReady(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	successes := 0
	for {
		status := tunnel.Status()
		readinessError := tunnel.readinessError(waitCtx, status)
		if readinessError == "" {
			successes++
			if successes >= tunnelReadinessSuccessTarget(status.Mode) {
				tunnel.updateStatus(func(status *TunnelStatus) {
					status.Ready = true
					status.ReadinessError = ""
				})
				return nil
			}
		}
		if status.ReadinessError != readinessError {
			successes = 0
			tunnel.updateStatus(func(status *TunnelStatus) {
				status.Ready = false
				status.ReadinessError = readinessError
			})
		} else if readinessError != "" {
			successes = 0
		}
		if status.State == "failed" {
			return fmt.Errorf("tunnel failed: %s", status.Error)
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("tunnel not ready before timeout: mode=%s state=%s connected=%t ready=%t public_url=%q readiness_error=%q", status.Mode, status.State, status.Connected, status.Ready, status.PublicURL, readinessError)
		case <-ticker.C:
		}
	}
}

func tunnelReadinessSuccessTarget(mode string) int {
	if !tunnelRequiresProcess(mode) || tunnelReadinessRequiredSuccesses < 1 {
		return 1
	}
	return tunnelReadinessRequiredSuccesses
}

func (tunnel *Tunnel) ready(ctx context.Context, status TunnelStatus) bool {
	return tunnel.readinessError(ctx, status) == ""
}

func (tunnel *Tunnel) readinessError(ctx context.Context, status TunnelStatus) string {
	switch status.Mode {
	case "", "off":
		if status.State == "disabled" {
			return ""
		}
		return "tunnel is not disabled"
	case "quick":
		return runningTunnelReadinessError(ctx, status)
	case "named":
		return namedTunnelReadinessError(ctx, status, tunnel.config.Tunnel.PublicURL)
	default:
		return "unsupported tunnel mode"
	}
}

func namedTunnelReadinessError(ctx context.Context, status TunnelStatus, configuredPublicURL string) string {
	configuredPublicURL = strings.TrimSpace(configuredPublicURL)
	if configuredPublicURL == "" {
		return "named tunnel public_url is empty"
	}
	if strings.TrimSpace(status.PublicURL) != configuredPublicURL {
		return fmt.Sprintf("named tunnel public_url %q does not match configured public_url %q", status.PublicURL, configuredPublicURL)
	}
	return runningTunnelReadinessError(ctx, status)
}

func runningTunnelReadinessError(ctx context.Context, status TunnelStatus) string {
	if status.State != "running" {
		return "cloudflared is not running"
	}
	if !status.Connected {
		return "cloudflared has not registered a tunnel connection"
	}
	if err := publicURLResolutionError(ctx, status.PublicURL); err != nil {
		return err.Error()
	}
	return ""
}

func publicURLResolutionError(ctx context.Context, rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("public_url is empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("public_url is invalid")
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err = lookupTunnelHost(lookupCtx, parsed.Hostname())
	if err != nil {
		return fmt.Errorf("public_url host %q does not resolve: %w", parsed.Hostname(), err)
	}
	return nil
}

func (tunnel *Tunnel) Status() TunnelStatus {
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	return tunnel.status
}

func (tunnel *Tunnel) setStatus(status TunnelStatus) {
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	tunnel.status = status
}

func (tunnel *Tunnel) updateStatus(update func(*TunnelStatus)) {
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	update(&tunnel.status)
}

func (tunnel *Tunnel) cloudflaredExecutable() (string, error) {
	candidates := cloudflaredCandidates(tunnel.config.Tunnel.CloudflaredPath)
	for _, path := range candidates {
		executable, err := exec.LookPath(path)
		if err == nil {
			return executable, nil
		}
	}
	return "", fmt.Errorf("cloudflared unavailable; checked %s", strings.Join(candidates, ", "))
}

func cloudflaredCandidates(configured string) []string {
	configured = strings.TrimSpace(configured)
	candidates := []string{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		for _, candidate := range candidates {
			if candidate == path {
				return
			}
		}
		candidates = append(candidates, path)
	}

	if configured == "@bundle/cloudflared" {
		if resources := os.Getenv("TRANSWARP_BUNDLE_RESOURCES"); resources != "" {
			add(filepath.Join(resources, "cloudflared"))
			return candidates
		}
		add(os.Getenv("TRANSWARP_CLOUDFLARED_PATH"))
		return candidates
	}

	if configured == "" {
		add(os.Getenv("TRANSWARP_CLOUDFLARED_PATH"))
		if resources := os.Getenv("TRANSWARP_BUNDLE_RESOURCES"); resources != "" {
			add(filepath.Join(resources, "cloudflared"))
		}
		add("cloudflared")
		return candidates
	}

	add(configured)
	return candidates
}

func cloudflaredEnvironment(config TunnelConfig) []string {
	environment := map[string]string{
		"PATH": "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin",
	}
	for _, key := range []string{"HOME", "TMPDIR", "USER", "LOGNAME", "SHELL"} {
		if value := os.Getenv(key); strings.TrimSpace(value) != "" {
			environment[key] = value
		}
	}
	if config.Mode == "named" {
		environment["TUNNEL_TOKEN"] = config.Token
	}

	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+environment[key])
	}
	return values
}

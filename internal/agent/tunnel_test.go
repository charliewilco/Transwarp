package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTunnelQuickModeStartsCloudflaredWithOriginURL(t *testing.T) {
	fake, argsPath := fakeCloudflared(t)
	config := Config{
		ListenAddress: "127.0.0.1:8188",
		Tunnel: TunnelConfig{
			Mode:            "quick",
			CloudflaredPath: fake,
		},
	}
	tunnel := NewTunnel(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tunnel.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer tunnel.Stop()

	args := waitForFile(t, argsPath)
	if !strings.Contains(args, "tunnel --no-autoupdate --url http://127.0.0.1:8188") {
		t.Fatalf("unexpected args: %s", args)
	}

	status := tunnel.Status()
	if status.State != "running" {
		t.Fatalf("unexpected tunnel state: %s", status.State)
	}
	if status.PID == 0 {
		t.Fatal("expected tunnel PID")
	}
}

func TestTunnelNamedModeStartsCloudflaredWithToken(t *testing.T) {
	fake, argsPath, tokenPath := fakeCloudflaredWithTokenCapture(t)
	config := Config{
		ListenAddress: "127.0.0.1:8188",
		Tunnel: TunnelConfig{
			Mode:            "named",
			CloudflaredPath: fake,
			Token:           "token-123",
		},
	}
	tunnel := NewTunnel(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tunnel.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer tunnel.Stop()

	args := waitForFile(t, argsPath)
	if !strings.Contains(args, "tunnel --no-autoupdate run") {
		t.Fatalf("unexpected args: %s", args)
	}
	if strings.Contains(args, "token-123") {
		t.Fatalf("tunnel token leaked through process arguments: %s", args)
	}
	token := waitForFile(t, tokenPath)
	if token != "token-123" {
		t.Fatalf("unexpected TUNNEL_TOKEN: %s", token)
	}
}

func TestTunnelQuickModeDoesNotPassInheritedEnvironmentToCloudflared(t *testing.T) {
	fake, argsPath, envPath := fakeCloudflaredWithEnvironmentCapture(t)
	t.Setenv("SHOULD_NOT_REACH_CLOUDFLARED", "local-secret")
	t.Setenv("TUNNEL_TOKEN", "inherited-token")

	tunnel := NewTunnel(Config{
		ListenAddress: "127.0.0.1:8188",
		Tunnel: TunnelConfig{
			Mode:            "quick",
			CloudflaredPath: fake,
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tunnel.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer tunnel.Stop()

	_ = waitForFile(t, argsPath)
	environment := waitForFile(t, envPath)
	if strings.Contains(environment, "SHOULD_NOT_REACH_CLOUDFLARED") || strings.Contains(environment, "local-secret") {
		t.Fatalf("cloudflared inherited arbitrary environment: %s", environment)
	}
	if strings.Contains(environment, "TUNNEL_TOKEN") || strings.Contains(environment, "inherited-token") {
		t.Fatalf("quick tunnel inherited tunnel token environment: %s", environment)
	}
	if !strings.Contains(environment, "PATH=/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin") {
		t.Fatalf("cloudflared environment missing fixed PATH: %s", environment)
	}
}

func TestCloudflaredEnvironmentIncludesOnlyNamedTunnelToken(t *testing.T) {
	t.Setenv("SHOULD_NOT_REACH_CLOUDFLARED", "local-secret")
	t.Setenv("TUNNEL_TOKEN", "inherited-token")
	t.Setenv("HOME", "/Users/charlie")

	environment := strings.Join(cloudflaredEnvironment(TunnelConfig{
		Mode:  "named",
		Token: "configured-token",
	}), "\n")

	if strings.Contains(environment, "SHOULD_NOT_REACH_CLOUDFLARED") || strings.Contains(environment, "local-secret") {
		t.Fatalf("cloudflared environment inherited arbitrary secret: %s", environment)
	}
	if strings.Contains(environment, "inherited-token") {
		t.Fatalf("cloudflared environment inherited prior tunnel token: %s", environment)
	}
	if !strings.Contains(environment, "TUNNEL_TOKEN=configured-token") {
		t.Fatalf("cloudflared environment missing configured token: %s", environment)
	}
	if !strings.Contains(environment, "HOME=/Users/charlie") {
		t.Fatalf("cloudflared environment missing allowed HOME: %s", environment)
	}
}

func TestTunnelScanRedactsTunnelTokenFromEmittedLogs(t *testing.T) {
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
	})

	tunnel := NewTunnel(Config{
		Tunnel: TunnelConfig{
			Mode:  "named",
			Token: "token-123",
		},
	})
	tunnel.scan("tunnel", strings.NewReader("ERR failed with token token-123\n"))

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = originalStdout

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("emitted event was not JSON: %s", string(data))
	}
	if strings.Contains(event.Message, "token-123") {
		t.Fatalf("tunnel log leaked token: %s", event.Message)
	}
	if !strings.Contains(event.Message, "[redacted]") {
		t.Fatalf("tunnel log did not redact token: %s", event.Message)
	}
}

func TestTunnelReportsMissingCloudflared(t *testing.T) {
	tunnel := NewTunnel(Config{
		ListenAddress: "127.0.0.1:8188",
		Tunnel: TunnelConfig{
			Mode:            "quick",
			CloudflaredPath: filepath.Join(t.TempDir(), "missing-cloudflared"),
		},
	})

	err := tunnel.Start(context.Background())
	if err == nil {
		t.Fatal("expected missing cloudflared to fail")
	}
	status := tunnel.Status()
	if status.State != "failed" {
		t.Fatalf("unexpected state: %s", status.State)
	}
	if !strings.Contains(status.Error, "cloudflared unavailable") {
		t.Fatalf("unexpected error: %s", status.Error)
	}
}

func TestCloudflaredExecutableUsesBundledEnvironmentPath(t *testing.T) {
	fake, _ := fakeCloudflared(t)
	t.Setenv("TRANSWARP_BUNDLE_RESOURCES", filepath.Dir(fake))

	tunnel := NewTunnel(Config{
		Tunnel: TunnelConfig{
			Mode:            "quick",
			CloudflaredPath: "@bundle/cloudflared",
		},
	})

	executable, err := tunnel.cloudflaredExecutable()
	if err != nil {
		t.Fatalf("cloudflaredExecutable returned error: %v", err)
	}
	if executable != fake {
		t.Fatalf("unexpected executable: %s", executable)
	}
}

func TestCloudflaredCandidatesUseBundleResourceDirectory(t *testing.T) {
	t.Setenv("TRANSWARP_CLOUDFLARED_PATH", "")
	t.Setenv("TRANSWARP_BUNDLE_RESOURCES", "/Applications/Transwarp.app/Contents/Resources")

	candidates := cloudflaredCandidates("@bundle/cloudflared")

	if len(candidates) != 1 {
		t.Fatalf("expected only bundle candidate: %+v", candidates)
	}
	if candidates[0] != "/Applications/Transwarp.app/Contents/Resources/cloudflared" {
		t.Fatalf("unexpected first candidate: %+v", candidates)
	}
}

func TestCloudflaredCandidatesUsePathFallbackWhenUnconfigured(t *testing.T) {
	t.Setenv("TRANSWARP_CLOUDFLARED_PATH", "/opt/transwarp/cloudflared")
	t.Setenv("TRANSWARP_BUNDLE_RESOURCES", "/Applications/Transwarp.app/Contents/Resources")

	candidates := cloudflaredCandidates("")

	if len(candidates) != 3 {
		t.Fatalf("expected env, bundle, and PATH candidates: %+v", candidates)
	}
	if candidates[0] != "/opt/transwarp/cloudflared" ||
		candidates[1] != "/Applications/Transwarp.app/Contents/Resources/cloudflared" ||
		candidates[2] != "cloudflared" {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
}

func TestStatusIncludesTunnelState(t *testing.T) {
	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		ListenAddress: "127.0.0.1:8188",
		Tunnel:        TunnelConfig{Mode: "quick"},
	})
	agent.tunnel.setStatus(TunnelStatus{
		Mode:   "quick",
		State:  "running",
		Origin: "http://127.0.0.1:8188",
		PID:    1234,
	})

	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	response := httptest.NewRecorder()
	agent.status(response, request)

	var status StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Tunnel.State != "running" {
		t.Fatalf("unexpected tunnel state: %s", status.Tunnel.State)
	}
	if status.Tunnel.PID != 1234 {
		t.Fatalf("unexpected tunnel pid: %d", status.Tunnel.PID)
	}
	if status.MachineID != "machine-123" {
		t.Fatalf("unexpected machine id: %s", status.MachineID)
	}
}

func TestRegistrationLoadDoesNotAcceptBuildsWhenTunnelIsNotReady(t *testing.T) {
	agent := New(Config{
		Tunnel: TunnelConfig{
			Mode: "quick",
		},
	})
	agent.tunnel.setStatus(TunnelStatus{
		Mode:      "quick",
		State:     "running",
		Connected: true,
		PublicURL: "https://example-name.trycloudflare.com",
		Ready:     false,
	})

	if load := agent.buildLoad(); load.AcceptingBuilds {
		t.Fatal("expected registration load to stop accepting builds while tunnel is unready")
	}

	agent.tunnel.setStatus(TunnelStatus{
		Mode:      "quick",
		State:     "running",
		Connected: true,
		PublicURL: "https://example-name.trycloudflare.com",
		Ready:     true,
	})
	if load := agent.buildLoad(); !load.AcceptingBuilds {
		t.Fatal("expected registration load to accept builds when tunnel is ready")
	}
}

func TestAgentPublicURLPrefersConfiguredValue(t *testing.T) {
	agent := New(Config{
		Tunnel: TunnelConfig{
			PublicURL: "https://configured.example.com",
		},
	})
	agent.tunnel.setStatus(TunnelStatus{
		Mode:      "quick",
		State:     "running",
		PublicURL: "https://quick.trycloudflare.com",
	})

	if publicURL := agent.publicURL(); publicURL != "https://configured.example.com" {
		t.Fatalf("unexpected public URL: %s", publicURL)
	}
}

func TestAgentPublicURLFallsBackToTunnelStatus(t *testing.T) {
	agent := New(Config{Tunnel: TunnelConfig{Mode: "quick"}})
	agent.tunnel.setStatus(TunnelStatus{
		Mode:      "quick",
		State:     "running",
		PublicURL: "https://quick.trycloudflare.com",
	})

	if publicURL := agent.publicURL(); publicURL != "https://quick.trycloudflare.com" {
		t.Fatalf("unexpected public URL: %s", publicURL)
	}
}

func TestTunnelScanCapturesQuickTunnelPublicURL(t *testing.T) {
	tunnel := NewTunnel(Config{Tunnel: TunnelConfig{Mode: "quick"}})

	tunnel.scan("tunnel", strings.NewReader("INF |  https://example-name.trycloudflare.com  |\n"))

	status := tunnel.Status()
	if status.PublicURL != "https://example-name.trycloudflare.com" {
		t.Fatalf("unexpected public URL: %s", status.PublicURL)
	}
}

func TestTunnelScanIgnoresQuickPublicURLForNamedTunnel(t *testing.T) {
	tunnel := NewTunnel(Config{
		Tunnel: TunnelConfig{
			Mode:      "named",
			PublicURL: "https://transwarp.example.com",
		},
	})
	tunnel.setStatus(TunnelStatus{
		Mode:      "named",
		State:     "running",
		PublicURL: "https://transwarp.example.com",
	})

	tunnel.scan("tunnel", strings.NewReader("INF |  https://example-name.trycloudflare.com  |\n"))

	if status := tunnel.Status(); status.PublicURL != "https://transwarp.example.com" {
		t.Fatalf("named tunnel public URL was overwritten: %s", status.PublicURL)
	}
}

func TestTunnelWaitReadyOffMode(t *testing.T) {
	tunnel := NewTunnel(Config{Tunnel: TunnelConfig{Mode: "off"}})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if err := tunnel.WaitReady(context.Background(), time.Second); err != nil {
		t.Fatalf("WaitReady returned error: %v", err)
	}
}

func TestTunnelWaitReadyQuickRequiresPublicURLAndConnection(t *testing.T) {
	stubLookupTunnelHost(t, true)
	tunnel := NewTunnel(Config{Tunnel: TunnelConfig{Mode: "quick"}})
	tunnel.setStatus(TunnelStatus{
		Mode:  "quick",
		State: "running",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errs := make(chan error, 1)
	go func() {
		errs <- tunnel.WaitReady(ctx, time.Second)
	}()

	time.Sleep(100 * time.Millisecond)
	tunnel.scan("tunnel", strings.NewReader("INF |  https://example-name.trycloudflare.com  |\n"))
	time.Sleep(100 * time.Millisecond)
	tunnel.scan("tunnel", strings.NewReader("INF Registered tunnel connection connIndex=0\n"))

	if err := <-errs; err != nil {
		t.Fatalf("WaitReady returned error: %v", err)
	}
	if !tunnel.Status().Ready {
		t.Fatal("expected tunnel status to be ready")
	}
}

func TestTunnelWaitReadyNamedRequiresConnection(t *testing.T) {
	stubLookupTunnelHost(t, true)
	tunnel := NewTunnel(Config{
		Tunnel: TunnelConfig{
			Mode:      "named",
			PublicURL: "https://transwarp.example.com",
		},
	})
	tunnel.setStatus(TunnelStatus{
		Mode:      "named",
		State:     "running",
		PublicURL: "https://transwarp.example.com",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errs := make(chan error, 1)
	go func() {
		errs <- tunnel.WaitReady(ctx, time.Second)
	}()

	time.Sleep(100 * time.Millisecond)
	tunnel.scan("tunnel", strings.NewReader("INF Registered tunnel connection connIndex=0\n"))

	if err := <-errs; err != nil {
		t.Fatalf("WaitReady returned error: %v", err)
	}
	if !tunnel.Status().Ready {
		t.Fatal("expected tunnel status to be ready")
	}
}

func TestTunnelWaitReadyNamedRejectsUnexpectedPublicURL(t *testing.T) {
	stubLookupTunnelHost(t, true)
	tunnel := NewTunnel(Config{
		Tunnel: TunnelConfig{
			Mode:      "named",
			PublicURL: "https://transwarp.example.com",
		},
	})
	tunnel.setStatus(TunnelStatus{
		Mode:      "named",
		State:     "running",
		Connected: true,
		PublicURL: "https://example-name.trycloudflare.com",
	})

	err := tunnel.WaitReady(context.Background(), 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected named tunnel with mismatched public URL to stay unready")
	}
	if tunnel.Status().Ready {
		t.Fatal("expected named tunnel status to remain unready")
	}
	if !strings.Contains(tunnel.Status().ReadinessError, "does not match configured public_url") {
		t.Fatalf("expected readiness error to describe public URL mismatch, got %q", tunnel.Status().ReadinessError)
	}
}

func TestTunnelWaitReadyRequiresResolvablePublicHost(t *testing.T) {
	stubLookupTunnelHost(t, false)
	tunnel := NewTunnel(Config{Tunnel: TunnelConfig{Mode: "quick"}})
	tunnel.setStatus(TunnelStatus{
		Mode:      "quick",
		State:     "running",
		Connected: true,
		PublicURL: "https://example-name.trycloudflare.com",
	})

	err := tunnel.WaitReady(context.Background(), 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected unresolved public URL to keep tunnel unready")
	}
	if tunnel.Status().Ready {
		t.Fatal("expected tunnel status to remain unready")
	}
	if !strings.Contains(tunnel.Status().ReadinessError, "does not resolve") {
		t.Fatalf("expected readiness error to describe DNS failure, got %q", tunnel.Status().ReadinessError)
	}
}

func TestTunnelWaitReadyRequiresConsecutiveResolvablePublicHost(t *testing.T) {
	withTunnelReadinessRequiredSuccesses(t, 3)
	var calls atomic.Int32
	stubLookupTunnelHostFunc(t, func(ctx context.Context, host string) ([]string, error) {
		if calls.Add(1) <= 2 {
			return []string{"203.0.113.10"}, nil
		}
		return nil, &net.DNSError{Name: host, IsNotFound: true}
	})
	tunnel := NewTunnel(Config{Tunnel: TunnelConfig{Mode: "quick"}})
	tunnel.setStatus(TunnelStatus{
		Mode:      "quick",
		State:     "running",
		Connected: true,
		PublicURL: "https://example-name.trycloudflare.com",
	})

	err := tunnel.WaitReady(context.Background(), 450*time.Millisecond)
	if err == nil {
		t.Fatal("expected intermittent public URL resolution to stay unready")
	}
	if tunnel.Status().Ready {
		t.Fatal("expected tunnel status to remain unready")
	}
	if !strings.Contains(tunnel.Status().ReadinessError, "does not resolve") {
		t.Fatalf("expected readiness error to describe DNS failure, got %q", tunnel.Status().ReadinessError)
	}
}

func TestTunnelMonitorReadinessClearsReadyWhenPublicHostStopsResolving(t *testing.T) {
	withTunnelReadinessRequiredSuccesses(t, 1)
	var resolves atomic.Bool
	resolves.Store(true)
	stubLookupTunnelHostFunc(t, func(ctx context.Context, host string) ([]string, error) {
		if resolves.Load() {
			return []string{"203.0.113.10"}, nil
		}
		return nil, &net.DNSError{Name: host, IsNotFound: true}
	})
	tunnel := NewTunnel(Config{Tunnel: TunnelConfig{Mode: "quick"}})
	tunnel.setStatus(TunnelStatus{
		Mode:      "quick",
		State:     "running",
		Connected: true,
		PublicURL: "https://example-name.trycloudflare.com",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tunnel.monitorReadiness(ctx)

	waitForTunnelStatus(t, tunnel, func(status TunnelStatus) bool {
		return status.Ready && status.ReadinessError == ""
	})

	resolves.Store(false)
	waitForTunnelStatus(t, tunnel, func(status TunnelStatus) bool {
		return !status.Ready && strings.Contains(status.ReadinessError, "does not resolve")
	})
}

func TestTunnelWaitReadyTimesOut(t *testing.T) {
	tunnel := NewTunnel(Config{Tunnel: TunnelConfig{Mode: "quick"}})
	tunnel.setStatus(TunnelStatus{Mode: "quick", State: "running"})

	err := tunnel.WaitReady(context.Background(), 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(tunnel.Status().ReadinessError, "has not registered") {
		t.Fatalf("expected readiness error to describe missing connection, got %q", tunnel.Status().ReadinessError)
	}
}

func fakeCloudflared(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "cloudflared")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuote(argsPath) + "\nsleep 30\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, argsPath
}

func fakeCloudflaredWithTokenCapture(t *testing.T) (string, string, string) {
	t.Helper()

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	tokenPath := filepath.Join(dir, "token.txt")
	script := filepath.Join(dir, "cloudflared")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuote(argsPath) + "\nprintf '%s\\n' \"$TUNNEL_TOKEN\" > " + shellQuote(tokenPath) + "\nsleep 30\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, argsPath, tokenPath
}

func fakeCloudflaredWithEnvironmentCapture(t *testing.T) (string, string, string) {
	t.Helper()

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	envPath := filepath.Join(dir, "env.txt")
	script := filepath.Join(dir, "cloudflared")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuote(argsPath) + "\n/usr/bin/env | /usr/bin/sort > " + shellQuote(envPath) + "\nsleep 30\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, argsPath, envPath
}

func fakeCloudflaredThatExits(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	script := filepath.Join(dir, "cloudflared")
	content := "#!/bin/sh\nprintf '%s\\n' 'INF |  https://example-name.trycloudflare.com  |'\nprintf '%s\\n' 'INF Registered tunnel connection connIndex=0'\nsleep 0.1\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			content := strings.TrimSpace(string(data))
			if content != "" {
				return content
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", path)
	return ""
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func stubLookupTunnelHost(t *testing.T, resolves bool) {
	t.Helper()

	stubLookupTunnelHostFunc(t, func(ctx context.Context, host string) ([]string, error) {
		if resolves {
			return []string{"203.0.113.10"}, nil
		}
		return nil, &net.DNSError{Name: host, IsNotFound: true}
	})
}

func stubLookupTunnelHostFunc(t *testing.T, lookup func(context.Context, string) ([]string, error)) {
	t.Helper()

	original := lookupTunnelHost
	lookupTunnelHost = lookup
	t.Cleanup(func() {
		lookupTunnelHost = original
	})
}

func withTunnelReadinessRequiredSuccesses(t *testing.T, successes int) {
	t.Helper()

	original := tunnelReadinessRequiredSuccesses
	tunnelReadinessRequiredSuccesses = successes
	t.Cleanup(func() {
		tunnelReadinessRequiredSuccesses = original
	})
}

func waitForTunnelStatus(t *testing.T, tunnel *Tunnel, matches func(TunnelStatus) bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	var status TunnelStatus
	for time.Now().Before(deadline) {
		status = tunnel.Status()
		if matches(status) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for tunnel status, last status: %+v", status)
}

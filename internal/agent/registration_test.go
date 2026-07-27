package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRegistrarIncludesMachineID(t *testing.T) {
	var payload RegistrationPayload
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     "127.0.0.1:8188",
		CIRegistrationURL: server.URL,
		Capabilities:      testCapabilities(),
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	if err := registrar.Register(context.Background()); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if payload.MachineID != "machine-123" {
		t.Fatalf("unexpected machine id: %s", payload.MachineID)
	}
	if payload.MachineName != "Mac Studio" {
		t.Fatalf("unexpected machine name: %s", payload.MachineName)
	}
	if payload.Capabilities.Architecture != "arm64" || payload.Capabilities.OS != "macOS" {
		t.Fatalf("unexpected capabilities: %+v", payload.Capabilities)
	}
}

func TestRegistrarIncludesCurrentBuildLoad(t *testing.T) {
	var payload RegistrationPayload
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     "127.0.0.1:8188",
		CIRegistrationURL: server.URL,
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})
	registrar.load = func() BuildLoad {
		return BuildLoad{AcceptingBuilds: false, ActiveBuilds: 1, QueuedBuilds: 2, QueuedBuildLimit: 25}
	}

	if err := registrar.Register(context.Background()); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if payload.ActiveBuilds != 1 || payload.QueuedBuilds != 2 || payload.QueuedBuildLimit != 25 {
		t.Fatalf("unexpected build load: active=%d queued=%d limit=%d", payload.ActiveBuilds, payload.QueuedBuilds, payload.QueuedBuildLimit)
	}
	if payload.AcceptingBuilds {
		t.Fatalf("unexpected accepting_builds: %t", payload.AcceptingBuilds)
	}
}

func TestRegistrarRefreshPostsHeartbeatImmediately(t *testing.T) {
	heartbeat := make(chan RegistrationPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/heartbeat" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		var payload RegistrationPayload
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		heartbeat <- payload
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	refresh := make(chan struct{}, 1)
	registrar := NewRegistrar(Config{
		MachineID:        "machine-123",
		MachineName:      "Mac Studio",
		ListenAddress:    "127.0.0.1:8188",
		CIHeartbeatURL:   server.URL + "/heartbeat",
		HeartbeatSeconds: 600,
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})
	registrar.refresh = refresh
	registrar.load = func() BuildLoad {
		return BuildLoad{AcceptingBuilds: false, ActiveBuilds: 1, QueuedBuilds: 2, QueuedBuildLimit: 25}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go registrar.RunHeartbeat(ctx)

	refresh <- struct{}{}
	payload := waitForRegistration(t, heartbeat)
	if payload.ActiveBuilds != 1 || payload.QueuedBuilds != 2 || payload.QueuedBuildLimit != 25 {
		t.Fatalf("unexpected refreshed build load: active=%d queued=%d limit=%d", payload.ActiveBuilds, payload.QueuedBuilds, payload.QueuedBuildLimit)
	}
	if payload.AcceptingBuilds {
		t.Fatalf("unexpected accepting_builds: %t", payload.AcceptingBuilds)
	}
}

func TestRegistrarLeaseDurationHasMinimum(t *testing.T) {
	registrar := NewRegistrar(Config{HeartbeatSeconds: 1})

	if got := registrar.leaseDuration(); got != minimumRegistrationLease {
		t.Fatalf("unexpected minimum lease duration: got %s want %s", got, minimumRegistrationLease)
	}
}

func TestRegistrarLeaseDurationScalesWithHeartbeat(t *testing.T) {
	registrar := NewRegistrar(Config{HeartbeatSeconds: 45})

	if got, want := registrar.leaseDuration(), 135*time.Second; got != want {
		t.Fatalf("unexpected lease duration: got %s want %s", got, want)
	}
}

func TestAgentRequestsRegistrationRefreshForAvailabilityAndBuildLoad(t *testing.T) {
	agent := New(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		CIRegistrationURL: "https://ci.example.com/transwarp/register",
		HeartbeatSeconds:  600,
		Tunnel:            TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})

	agent.setAcceptingBuilds(false)
	waitForRegistrationRefresh(t, agent)
	agent.setAcceptingBuilds(true)
	waitForRegistrationRefresh(t, agent)

	first, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "first"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer waitForRun(t, first)
	defer first.cancel()
	waitForRegistrationRefresh(t, agent)

	second, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "second"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	waitForRegistrationRefresh(t, agent)

	agent.cancelBuildRun(second, "test cleanup")
	waitForRegistrationRefresh(t, agent)
	waitForClosed(t, second)
}

func TestRegistrarSendsCloudflareAccessHeaders(t *testing.T) {
	var sawAccessHeaders bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		sawAccessHeaders = request.Header.Get("CF-Access-Client-Id") == "access-id" &&
			request.Header.Get("CF-Access-Client-Secret") == "access-secret"
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		ListenAddress:        "127.0.0.1:8188",
		CIRegistrationURL:    server.URL,
		CIAccessClientID:     "access-id",
		CIAccessClientSecret: "access-secret",
		Capabilities:         testCapabilities(),
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	if err := registrar.Register(context.Background()); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if !sawAccessHeaders {
		t.Fatal("registration request did not include Cloudflare Access headers")
	}
}

func TestRegistrarUsesCIHTTPClient(t *testing.T) {
	var sawRegistration bool
	original := newCIHTTPClient
	newCIHTTPClient = func(timeout time.Duration) *http.Client {
		if timeout != 15*time.Second {
			t.Fatalf("unexpected CI HTTP timeout: %s", timeout)
		}
		return &http.Client{Transport: agentRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			sawRegistration = true
			if request.URL.String() != "https://ci.example.com/transwarp/register" {
				t.Fatalf("unexpected registration URL: %s", request.URL.String())
			}
			if request.Header.Get("Authorization") != "Bearer registration-token" {
				t.Fatalf("missing registration token: %q", request.Header.Get("Authorization"))
			}
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Status:     "202 Accepted",
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		})}
	}
	t.Cleanup(func() {
		newCIHTTPClient = original
	})

	registrar := NewRegistrar(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     "127.0.0.1:8188",
		CIRegistrationURL: "https://ci.example.com/transwarp/register",
		RegistrationToken: "registration-token",
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	if err := registrar.Register(context.Background()); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if !sawRegistration {
		t.Fatal("registrar did not use CI HTTP client")
	}
}

func TestRegistrarDoesNotFollowRedirects(t *testing.T) {
	redirected := make(chan http.Header, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		redirected <- request.Header.Clone()
		response.WriteHeader(http.StatusAccepted)
	}))
	defer redirectTarget.Close()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		http.Redirect(response, request, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		ListenAddress:        "127.0.0.1:8188",
		CIRegistrationURL:    server.URL,
		RegistrationToken:    "registration-token",
		CIAccessClientID:     "access-id",
		CIAccessClientSecret: "access-secret",
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	err := registrar.Register(context.Background())
	if err == nil || !strings.Contains(err.Error(), "307 Temporary Redirect") {
		t.Fatalf("expected redirect registration error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected redirect to fail without retry, got %d attempts", attempts)
	}
	select {
	case headers := <-redirected:
		t.Fatalf("registration redirect leaked headers: Authorization=%q CF-Access-Client-Secret=%q", headers.Get("Authorization"), headers.Get("CF-Access-Client-Secret"))
	default:
	}
}

func TestAgentLifecycleUsesCloudflareAccessForCICallbacks(t *testing.T) {
	registered := make(chan RegistrationPayload, 1)
	reported := make(chan BuildReport, 1)
	deregistered := make(chan RegistrationPayload, 1)

	ciServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("CF-Access-Client-Id") != "access-id" ||
			request.Header.Get("CF-Access-Client-Secret") != "access-secret" {
			writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "missing access headers"})
			return
		}

		switch request.URL.Path {
		case "/register", "/heartbeat":
			var payload RegistrationPayload
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			select {
			case registered <- payload:
			default:
			}
			response.WriteHeader(http.StatusAccepted)
		case "/result":
			if request.Header.Get("Authorization") != "Bearer report-token" {
				t.Errorf("missing report bearer token: %q", request.Header.Get("Authorization"))
				response.WriteHeader(http.StatusUnauthorized)
				return
			}
			var report BuildReport
			if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
				t.Error(err)
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			reported <- report
			response.WriteHeader(http.StatusAccepted)
		case "/deregister":
			var payload RegistrationPayload
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			deregistered <- payload
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))
	defer ciServer.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listenAddress := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := New(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		ListenAddress:        listenAddress,
		SharedToken:          "runner-token",
		CIRegistrationURL:    ciServer.URL + "/register",
		CIHeartbeatURL:       ciServer.URL + "/heartbeat",
		CIDeregistrationURL:  ciServer.URL + "/deregister",
		RegistrationToken:    "registration-token",
		CIAccessClientID:     "access-id",
		CIAccessClientSecret: "access-secret",
		HeartbeatSeconds:     1,
		Tunnel:               TunnelConfig{Mode: "off", PublicURL: "http://" + listenAddress},
		Capabilities:         testCapabilities(),
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello through access callback lifecycle"},
			TimeoutSeconds:   10,
		}},
	})

	errs := make(chan error, 1)
	go func() {
		errs <- agent.Run(ctx)
	}()

	waitForRunnerHealth(t, listenAddress)
	registration := waitForRegistration(t, registered)
	if registration.MachineID != "machine-123" {
		t.Fatalf("unexpected registered machine id: %s", registration.MachineID)
	}
	if registration.Capabilities.Architecture != "arm64" {
		t.Fatalf("unexpected registered capabilities: %+v", registration.Capabilities)
	}
	status := fetchRunnerStatus(t, listenAddress, "runner-token")
	if status.Capabilities.Architecture != "arm64" {
		t.Fatalf("unexpected status capabilities: %+v", status.Capabilities)
	}
	if status.Registration.State != "registered" {
		t.Fatalf("expected registered status, got %+v", status.Registration)
	}
	if status.Registration.LeaseExpiresAt == nil {
		t.Fatalf("expected registration lease in status, got %+v", status.Registration)
	}

	startBody := bytes.NewBufferString(`{"job_id":"echo","request_id":"access-lifecycle","report_url":"` + ciServer.URL + `/result","report_token":"report-token"}`)
	startRequest, err := http.NewRequest(http.MethodPost, "http://"+listenAddress+"/v1/builds", startBody)
	if err != nil {
		t.Fatal(err)
	}
	startRequest.Header.Set("Authorization", "Bearer runner-token")
	startResponse, err := http.DefaultClient.Do(startRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer startResponse.Body.Close()
	if startResponse.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(startResponse.Body)
		t.Fatalf("unexpected start status %s: %s", startResponse.Status, string(data))
	}

	report := waitForBuildReport(t, reported)
	if report.Status != "passed" {
		t.Fatalf("unexpected report status: %s", report.Status)
	}
	if report.RequestID != "access-lifecycle" || report.MachineID != "machine-123" {
		t.Fatalf("unexpected report: %+v", report)
	}

	cancel()
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("agent returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop")
	}

	deregistration := waitForRegistration(t, deregistered)
	if deregistration.MachineID != "machine-123" {
		t.Fatalf("unexpected deregistered machine id: %s", deregistration.MachineID)
	}
}

func TestRegistrarRetriesDeregistrationUntilItSucceeds(t *testing.T) {
	attempts := 0
	deregistered := make(chan RegistrationPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		if attempts == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		var payload RegistrationPayload
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		deregistered <- payload
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:           "machine-123",
		MachineName:         "Mac Studio",
		ListenAddress:       "127.0.0.1:8188",
		CIDeregistrationURL: server.URL,
		RegistrationToken:   "registration-token",
		Capabilities:        testCapabilities(),
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	registrar.DeregisterWithRetry(ctx, time.Millisecond)

	select {
	case payload := <-deregistered:
		if payload.MachineID != "machine-123" {
			t.Fatalf("unexpected deregistered machine id: %s", payload.MachineID)
		}
	default:
		t.Fatal("registrar did not retry deregistration")
	}
	if attempts != 2 {
		t.Fatalf("expected two deregistration attempts, got %d", attempts)
	}
}

func TestAgentDeregistersWhenTunnelExitsUnexpectedly(t *testing.T) {
	withTunnelReadinessRequiredSuccesses(t, 1)
	stubLookupTunnelHost(t, true)

	registered := make(chan RegistrationPayload, 1)
	deregistered := make(chan RegistrationPayload, 1)
	ciServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload RegistrationPayload
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}

		switch request.URL.Path {
		case "/register":
			registered <- payload
			response.WriteHeader(http.StatusAccepted)
		case "/deregister":
			deregistered <- payload
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))
	defer ciServer.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listenAddress := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := New(Config{
		MachineID:           "machine-123",
		MachineName:         "Mac Studio",
		ListenAddress:       listenAddress,
		SharedToken:         "runner-token",
		CIRegistrationURL:   ciServer.URL + "/register",
		CIDeregistrationURL: ciServer.URL + "/deregister",
		Tunnel: TunnelConfig{
			Mode:            "quick",
			CloudflaredPath: fakeCloudflaredThatExits(t),
		},
		Capabilities: testCapabilities(),
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	errs := make(chan error, 1)
	go func() {
		errs <- agent.Run(ctx)
	}()

	waitForRunnerHealth(t, listenAddress)
	registration := waitForRegistration(t, registered)
	if registration.PublicURL != "https://example-name.trycloudflare.com" {
		t.Fatalf("unexpected public URL: %s", registration.PublicURL)
	}

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected tunnel exit to fail the runner")
		}
		if !strings.Contains(err.Error(), "cloudflared exited unexpectedly") {
			t.Fatalf("expected cloudflared exit error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop after tunnel exit")
	}

	deregistration := waitForRegistration(t, deregistered)
	if deregistration.MachineID != "machine-123" {
		t.Fatalf("unexpected deregistered machine id: %s", deregistration.MachineID)
	}
}

func TestAgentStopsHeartbeatsBeforeDeregisteringAfterTunnelExit(t *testing.T) {
	withTunnelReadinessRequiredSuccesses(t, 1)
	stubLookupTunnelHost(t, true)

	registered := make(chan RegistrationPayload, 1)
	deregistered := make(chan RegistrationPayload, 1)
	heartbeat := make(chan RegistrationPayload, 4)
	ciServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload RegistrationPayload
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}

		switch request.URL.Path {
		case "/register":
			registered <- payload
			response.WriteHeader(http.StatusAccepted)
		case "/heartbeat":
			heartbeat <- payload
			response.WriteHeader(http.StatusAccepted)
		case "/deregister":
			deregistered <- payload
			response.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(response, request)
		}
	}))
	defer ciServer.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listenAddress := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := New(Config{
		MachineID:           "machine-123",
		MachineName:         "Mac Studio",
		ListenAddress:       listenAddress,
		SharedToken:         "runner-token",
		CIRegistrationURL:   ciServer.URL + "/register",
		CIHeartbeatURL:      ciServer.URL + "/heartbeat",
		CIDeregistrationURL: ciServer.URL + "/deregister",
		HeartbeatSeconds:    1,
		Tunnel: TunnelConfig{
			Mode:            "quick",
			CloudflaredPath: fakeCloudflaredThatExits(t),
		},
		Capabilities: testCapabilities(),
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	errs := make(chan error, 1)
	go func() {
		errs <- agent.Run(ctx)
	}()

	waitForRunnerHealth(t, listenAddress)
	registration := waitForRegistration(t, registered)
	if registration.PublicURL != "https://example-name.trycloudflare.com" {
		t.Fatalf("unexpected public URL: %s", registration.PublicURL)
	}

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected tunnel exit to fail the runner")
		}
		if !strings.Contains(err.Error(), "cloudflared exited unexpectedly") {
			t.Fatalf("expected cloudflared exit error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop after tunnel exit")
	}

	deregistration := waitForRegistration(t, deregistered)
	if deregistration.MachineID != "machine-123" {
		t.Fatalf("unexpected deregistered machine id: %s", deregistration.MachineID)
	}

	select {
	case payload := <-heartbeat:
		t.Fatalf("heartbeat continued after deregistration: %+v", payload)
	case <-time.After(1200 * time.Millisecond):
	}
}

func TestRegistrarRetriesUntilRegistrationSucceeds(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		if attempts == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     "127.0.0.1:8188",
		CIRegistrationURL: server.URL,
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	if err := registrar.RegisterWithRetry(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("RegisterWithRetry returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
}

func TestRegistrarIncludesFailureResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "target token mismatch"})
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     "127.0.0.1:8188",
		CIRegistrationURL: server.URL,
		Capabilities:      testCapabilities(),
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	err := registrar.Register(context.Background())
	if err == nil {
		t.Fatal("expected registration failure")
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "target token mismatch") {
		t.Fatalf("expected status and response body, got %v", err)
	}
}

func TestRegistrarRedactsFailureResponseBodyInEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "registration-token rejected with access-secret"})
	}))
	defer server.Close()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
	})

	registrar := NewRegistrar(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		ListenAddress:        "127.0.0.1:8188",
		CIDeregistrationURL:  server.URL,
		RegistrationToken:    "registration-token",
		CIAccessClientID:     "access-id",
		CIAccessClientSecret: "access-secret",
		Capabilities:         testCapabilities(),
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	registrar.Deregister(context.Background())
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
		t.Fatalf("event was not JSON: %s", string(data))
	}
	if strings.Contains(event.Message, "registration-token") || strings.Contains(event.Message, "access-secret") {
		t.Fatalf("event leaked secrets: %s", event.Message)
	}
	if !strings.Contains(event.Message, "[redacted]") || !strings.Contains(event.Message, "401 Unauthorized") {
		t.Fatalf("event lost useful redacted failure detail: %s", event.Message)
	}
}

func TestRegistrationStatusRedactsFailureError(t *testing.T) {
	agent := New(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		ListenAddress:        "127.0.0.1:8188",
		SharedToken:          "runner-token",
		CIRegistrationURL:    "https://ci.example.com/transwarp/register",
		RegistrationToken:    "registration-token",
		CIAccessClientID:     "access-id",
		CIAccessClientSecret: "access-secret",
	})

	agent.updateRegistrationStatus(
		"failed",
		"register",
		nil,
		errors.New(`registration endpoint returned 401 Unauthorized: {"error":"registration-token rejected with access-secret"}`),
	)

	status := agent.registration()
	if strings.Contains(status.LastError, "registration-token") || strings.Contains(status.LastError, "access-secret") {
		t.Fatalf("registration status leaked secrets: %s", status.LastError)
	}
	if !strings.Contains(status.LastError, "[redacted]") || !strings.Contains(status.LastError, "401 Unauthorized") {
		t.Fatalf("registration status lost useful redacted failure detail: %s", status.LastError)
	}
}

func TestRegistrarOmitsEmptyFailureResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     "127.0.0.1:8188",
		CIRegistrationURL: server.URL,
		Capabilities:      testCapabilities(),
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	})

	err := registrar.Register(context.Background())
	if err == nil {
		t.Fatal("expected registration failure")
	}
	if err.Error() != "registration endpoint returned 503 Service Unavailable" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRetryStopsWhenContextIsCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	registrar := NewRegistrar(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     "127.0.0.1:8188",
		CIRegistrationURL: server.URL,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := registrar.RegisterWithRetry(ctx, time.Millisecond); err == nil {
		t.Fatal("expected canceled context to stop retry loop")
	}
}

func TestAgentRetriesTunnelReadinessBeforeRegistration(t *testing.T) {
	withTunnelReadinessRequiredSuccesses(t, 1)
	originalTimeout := registrationTunnelReadyTimeout
	registrationTunnelReadyTimeout = time.Millisecond
	originalDelay := registrationTunnelRetryDelay
	registrationTunnelRetryDelay = time.Millisecond
	defer func() {
		registrationTunnelReadyTimeout = originalTimeout
		registrationTunnelRetryDelay = originalDelay
	}()

	originalLookup := lookupTunnelHost
	lookupAttempts := 0
	lookupTunnelHost = func(ctx context.Context, host string) ([]string, error) {
		lookupAttempts++
		if lookupAttempts == 1 {
			return nil, fmt.Errorf("dns not ready")
		}
		return []string{"127.0.0.1"}, nil
	}
	defer func() {
		lookupTunnelHost = originalLookup
	}()

	registrations := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		registrations++
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	config := Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     "127.0.0.1:8188",
		CIRegistrationURL: server.URL,
		Tunnel: TunnelConfig{
			Mode:      "named",
			PublicURL: "https://transwarp.example.com",
		},
		Jobs: []JobConfig{{
			ID: "xcode-debug",
		}},
	}
	agent := New(config)
	tunnel := NewTunnel(config)
	tunnel.setStatus(TunnelStatus{
		Mode:      "named",
		State:     "running",
		PublicURL: "https://transwarp.example.com",
		Connected: true,
	})
	agent.tunnel = tunnel

	registrar, err := agent.registerWhenTunnelReady(context.Background(), tunnel, config)
	if err != nil {
		t.Fatalf("registerWhenTunnelReady returned error: %v", err)
	}
	if registrar == nil {
		t.Fatal("expected registrar")
	}
	if lookupAttempts < 2 {
		t.Fatalf("expected readiness to be retried, got %d lookup attempts", lookupAttempts)
	}
	if registrations != 1 {
		t.Fatalf("expected one registration, got %d", registrations)
	}
}

func TestAgentReportsServerFailureWhileWaitingForRegistration(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	registrationServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer registrationServer.Close()

	agent := New(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		ListenAddress:     listener.Addr().String(),
		SharedToken:       "runner-token",
		CIRegistrationURL: registrationServer.URL,
		Tunnel:            TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "xcode-version",
			Label:            "Xcode Version",
			WorkingDirectory: "/tmp",
			Command:          "/usr/bin/xcodebuild",
			Arguments:        []string{"-version"},
			TimeoutSeconds:   300,
		}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = agent.Run(ctx)
	if err == nil {
		t.Fatal("expected occupied listen address to fail startup")
	}
	if !strings.Contains(err.Error(), "server failed") {
		t.Fatalf("expected server failure, got %v", err)
	}
}

func TestAgentRejectsUnsupportedHostBeforeServing(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listenAddress := listener.Addr().String()
	_ = listener.Close()

	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		ListenAddress: listenAddress,
		SharedToken:   "runner-token",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "13.6",
			Architecture: "arm64",
		},
		Tunnel: TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "xcode-version",
			Label:            "Xcode Version",
			WorkingDirectory: "/tmp",
			Command:          "/usr/bin/xcodebuild",
			Arguments:        []string{"-version"},
			TimeoutSeconds:   300,
		}},
	})

	err = agent.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "macOS 14 or newer") {
		t.Fatalf("expected unsupported host error, got %v", err)
	}
	if response, getErr := http.Get("http://" + listenAddress + "/health"); getErr == nil {
		_ = response.Body.Close()
		t.Fatal("unsupported host should not start the runner HTTP server")
	}
}

func waitForRunnerHealth(t *testing.T, listenAddress string) {
	t.Helper()

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("runner did not become healthy at %s", listenAddress)
		case <-ticker.C:
			response, err := http.Get("http://" + listenAddress + "/health")
			if err != nil {
				continue
			}
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
	}
}

func waitForRegistration(t *testing.T, registrations <-chan RegistrationPayload) RegistrationPayload {
	t.Helper()

	select {
	case payload := <-registrations:
		return payload
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for registration callback")
		return RegistrationPayload{}
	}
}

func waitForRegistrationRefresh(t *testing.T, agent *Agent) {
	t.Helper()

	select {
	case <-agent.registrationRefresh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for registration refresh request")
	}
}

func waitForBuildReport(t *testing.T, reports <-chan BuildReport) BuildReport {
	t.Helper()

	select {
	case report := <-reports:
		return report
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for build report callback")
		return BuildReport{}
	}
}

func fetchRunnerStatus(t *testing.T, listenAddress string, token string) StatusResponse {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, "http://"+listenAddress+"/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected status response %s: %s", response.Status, string(data))
	}

	var status StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

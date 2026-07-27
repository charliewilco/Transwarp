package coordinator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRegisterListAndDeregisterTarget(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Listen:      "127.0.0.1:8188",
		PublicURL:   "https://transwarp.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
			CPUBrand:     "Apple M3 Max",
			CPUCount:     16,
			XcodeVersion: "Xcode 16.4 (Build version 16F6)",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 1 {
		t.Fatalf("expected one target, got %d", len(targets))
	}
	if targets[0].MachineID != "machine-123" {
		t.Fatalf("unexpected machine id: %s", targets[0].MachineID)
	}
	if targets[0].Capabilities.Architecture != "arm64" {
		t.Fatalf("unexpected capabilities: %+v", targets[0].Capabilities)
	}

	deregisterBody := bytes.NewBufferString(`{"machine_id":"machine-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/deregister", deregisterBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected deregister status: %s", response.Status)
	}

	targets = getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 0 {
		t.Fatalf("expected no targets, got %d", len(targets))
	}
}

func TestDeregisterReleasesUnacceptedDispatchReservation(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:        "machine-123",
		MachineName:      "Mac Studio",
		PublicURL:        "https://transwarp.example.com",
		QueuedBuildLimit: 1,
		Jobs:             []string{"xcode-debug"},
		LeaseExpiresAt:   time.Now().Add(time.Minute),
	})
	activateDispatch(server, DispatchRequest{
		MachineID: "machine-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	})
	if _, err := server.reserveActiveDispatchTarget("run-123", "machine-123"); err != nil {
		t.Fatalf("reserveActiveDispatchTarget returned error: %v", err)
	}

	deregisterBody := bytes.NewBufferString(`{"machine_id":"machine-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/deregister", deregisterBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected deregister status: %s", response.Status)
	}

	server.mu.Lock()
	_, active := server.active["run-123"]
	reservations := server.targetReservations["machine-123"]
	server.mu.Unlock()
	if active {
		t.Fatalf("unaccepted active dispatch remained after target deregistration: %+v", server.active)
	}
	if reservations != 0 {
		t.Fatalf("target reservation remained after target deregistration: %d", reservations)
	}
}

func TestDeregisterPreservesAcceptedDispatchForResultCallback(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      "https://runner.example.com",
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	activateDispatch(server, DispatchRequest{
		MachineID: "machine-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	})
	if _, err := server.reserveActiveDispatchTarget("run-123", "machine-123"); err != nil {
		t.Fatalf("reserveActiveDispatchTarget returned error: %v", err)
	}
	if err := server.markActiveDispatchBuild("run-123", "machine-123", "https://runner.example.com", "build-123"); err != nil {
		t.Fatal(err)
	}

	deregisterBody := bytes.NewBufferString(`{"machine_id":"machine-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/deregister", deregisterBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected deregister status: %s", response.Status)
	}

	result := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
		PublicURL: "https://runner.example.com",
	})
	defer result.Body.Close()
	if result.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(result.Body)
		t.Fatalf("unexpected result status after deregistration: %s body: %s", result.Status, string(data))
	}
}

func TestCoordinatorSeparatesCITokenFromTargetCallbackToken(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TargetToken:    "target-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	target := targetWithDefaultCapabilities(Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      "https://transwarp.example.com",
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	targetData, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", bytes.NewReader(targetData))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("coordinator token should not register targets, got %s", response.Status)
	}

	registerTarget(t, testServer.URL, "target-token", target)
	if targets := getTargets(t, testServer.URL, "coord-token"); len(targets) != 1 {
		t.Fatalf("coordinator token should list targets, got %+v", targets)
	}

	dispatchBody := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-unauthorized"}`)
	dispatchRequest, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", dispatchBody)
	if err != nil {
		t.Fatal(err)
	}
	dispatchRequest.Header.Set("Authorization", "Bearer target-token")
	dispatchResponse, err := testServer.Client().Do(dispatchRequest)
	if err != nil {
		t.Fatal(err)
	}
	dispatchResponse.Body.Close()
	if dispatchResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("target token should not dispatch CI work, got %s", dispatchResponse.Status)
	}

	activateStartedDispatch(server, DispatchRequest{
		MachineID: "machine-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")
	unauthorizedResult := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	unauthorizedResult.Body.Close()
	if unauthorizedResult.StatusCode != http.StatusUnauthorized {
		t.Fatalf("coordinator token should not record target result callbacks, got %s", unauthorizedResult.Status)
	}

	authorizedResult := recordResult(t, testServer.URL, "target-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	authorizedResult.Body.Close()
	if authorizedResult.StatusCode != http.StatusAccepted {
		t.Fatalf("target token should record result callbacks, got %s", authorizedResult.Status)
	}
}

func TestDeregisterRequiresMachineID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/deregister", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected deregister status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "machine_id is required") {
		t.Fatalf("expected machine_id error, got %s", string(data))
	}
}

func TestRegisterRejectsUnsafeMachineID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{
		"machine_id": "machine/123",
		"machine_name": "Mac Studio",
		"public_url": "https://transwarp.example.com",
		"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
		"jobs": ["xcode-debug"]
	}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected register status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "machine_id") {
		t.Fatalf("expected machine_id validation error, got %s", string(data))
	}
}

func TestDeregisterRejectsUnsafeMachineID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/deregister", bytes.NewBufferString(`{"machine_id":"machine/123"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected deregister status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "machine_id") {
		t.Fatalf("expected machine_id validation error, got %s", string(data))
	}
}

func TestRegisterRejectsUnsupportedTargetCapabilities(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{
		"machine_id": "machine-123",
		"machine_name": "Old Mac",
		"public_url": "https://transwarp.example.com",
		"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "amd64"},
		"jobs": ["xcode-debug"]
	}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected register status: %s: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "Apple Silicon") {
		t.Fatalf("expected Apple Silicon rejection, got %s", string(data))
	}

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 0 {
		t.Fatalf("unsupported target should not be registered: %+v", targets)
	}
}

func TestRegisterRejectsNegativeTargetLoad(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "active builds",
			body: `{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "https://transwarp.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"active_builds": -1,
				"jobs": ["xcode-debug"]
			}`,
			want: "active_builds must be non-negative",
		},
		{
			name: "queued builds",
			body: `{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "https://transwarp.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"queued_builds": -1,
				"jobs": ["xcode-debug"]
			}`,
			want: "queued_builds must be non-negative",
		},
		{
			name: "queued build limit",
			body: `{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "https://transwarp.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"queued_build_limit": -1,
				"jobs": ["xcode-debug"]
			}`,
			want: "queued_build_limit must be non-negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, Options{
				Token:          "coord-token",
				TranswarpToken: "runner-token",
				PublicURL:      "https://coordinator.test",
			})
			testServer := httptest.NewServer(server.Handler())
			defer testServer.Close()

			request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer coord-token")
			request.Header.Set("Content-Type", "application/json")

			response, err := testServer.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			data, _ := io.ReadAll(response.Body)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("unexpected register status: %s body: %s", response.Status, string(data))
			}
			if !strings.Contains(string(data), test.want) {
				t.Fatalf("expected %q, got %s", test.want, string(data))
			}

			targets := getTargets(t, testServer.URL, "coord-token")
			if len(targets) != 0 {
				t.Fatalf("negative-load target should not be registered: %+v", targets)
			}
		})
	}
}

func TestTargetEligibilityRequiresModernMacOSCapabilities(t *testing.T) {
	tests := []struct {
		name         string
		capabilities Capabilities
		want         string
	}{
		{
			name:         "missing capabilities",
			capabilities: Capabilities{},
			want:         "capabilities are required",
		},
		{
			name: "missing os version",
			capabilities: Capabilities{
				OS:           "macOS",
				Architecture: "arm64",
			},
			want: "macOS version is required",
		},
		{
			name: "old macOS",
			capabilities: Capabilities{
				OS:           "macOS",
				OSVersion:    "13.6",
				Architecture: "arm64",
			},
			want: "macOS 14 or newer",
		},
		{
			name: "unparseable macOS",
			capabilities: Capabilities{
				OS:           "macOS",
				OSVersion:    "Sonoma",
				Architecture: "arm64",
			},
			want: "could not be parsed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := targetEligibilityError(Target{
				MachineID:    "machine-123",
				MachineName:  "Mac",
				PublicURL:    "https://transwarp.example.com",
				Capabilities: test.capabilities,
				Jobs:         []string{"xcode-debug"},
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestRegisterRejectsRemoteHTTPTargetPublicURL(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{
		"machine_id": "machine-123",
		"machine_name": "Mac Studio",
		"listen": "127.0.0.1:8188",
		"public_url": "http://transwarp.example.com",
		"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
		"jobs": ["xcode-debug"]
	}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected register status: %s: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "target public_url must use https") {
		t.Fatalf("expected public_url https rejection, got %s", string(data))
	}

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 0 {
		t.Fatalf("invalid target should not be registered: %+v", targets)
	}
}

func TestRegisterRejectsTargetPublicURLWithPath(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{
		"machine_id": "machine-123",
		"machine_name": "Mac Studio",
		"listen": "127.0.0.1:8188",
		"public_url": "https://transwarp.example.com/status",
		"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
		"jobs": ["xcode-debug"]
	}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected register status: %s: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "target public_url must be a base URL") {
		t.Fatalf("expected public_url base URL rejection, got %s", string(data))
	}
}

func TestRegisterRejectsUnknownJSONFields(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{
		"machine_id": "machine-123",
		"machine_name": "Mac Studio",
		"listen": "127.0.0.1:8188",
		"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
		"jobs": ["xcode-debug"],
		"unexpected": true
	}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected register status: %s", response.Status)
	}
	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 0 {
		t.Fatalf("unknown register field should not create target: %+v", targets)
	}
}

func TestRegisterAllowsLoopbackHTTPTargetPublicURL(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Listen:      "127.0.0.1:8188",
		PublicURL:   "http://127.0.0.1:8188",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 1 {
		t.Fatalf("expected loopback target to register, got %d", len(targets))
	}
}

func TestRegisterFillsMissingLease(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	before := time.Now()
	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Listen:      "127.0.0.1:8188",
		PublicURL:   "https://transwarp.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs: []string{"xcode-debug"},
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 1 {
		t.Fatalf("expected target to register, got %d", len(targets))
	}
	minLease := before.Add(defaultTargetLease - time.Second)
	maxLease := before.Add(defaultTargetLease + time.Second)
	if targets[0].LeaseExpiresAt.Before(minLease) || targets[0].LeaseExpiresAt.After(maxLease) {
		t.Fatalf("unexpected default lease: %s not between %s and %s", targets[0].LeaseExpiresAt, minLease, maxLease)
	}
}

func TestRegisterCapsFarFutureLease(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	before := time.Now()
	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Listen:      "127.0.0.1:8188",
		PublicURL:   "https://transwarp.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: before.Add(24 * time.Hour),
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 1 {
		t.Fatalf("expected target to register, got %d", len(targets))
	}
	maxLease := before.Add(maxTargetLease + time.Second)
	if targets[0].LeaseExpiresAt.After(maxLease) {
		t.Fatalf("lease was not capped: %s after %s", targets[0].LeaseExpiresAt, maxLease)
	}
}

func TestRegisterRejectsRemoteListenFallback(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{
		"machine_id": "machine-123",
		"machine_name": "Mac Studio",
		"listen": "10.0.0.12:8188",
		"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
		"jobs": ["xcode-debug"]
	}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected register status: %s: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "must be loopback") {
		t.Fatalf("expected listen loopback rejection, got %s", string(data))
	}
}

func TestRegisterRejectsTargetWithoutPublicURLOrListen(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{
		"machine_id": "machine-123",
		"machine_name": "Mac Studio",
		"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
		"jobs": ["xcode-debug"]
	}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected register status: %s: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "public_url or loopback listen") {
		t.Fatalf("expected missing endpoint rejection, got %s", string(data))
	}
}

func TestRegisterRejectsUnsafeJobID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{
		"machine_id": "machine-123",
		"machine_name": "Mac Studio",
		"public_url": "https://transwarp.example.com",
		"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
		"jobs": ["xcode/debug"]
	}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/register", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected register status: %s: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "job_id") {
		t.Fatalf("expected job_id validation error, got %s", string(data))
	}
}

func TestListTargetsHidesUnsupportedStaleTargets(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	seedTarget(server, Target{
		MachineID:   "machine-old",
		MachineName: "Old Mac",
		PublicURL:   "https://old.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "amd64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:   "machine-new",
		MachineName: "Apple Silicon Mac",
		PublicURL:   "https://new.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 1 {
		t.Fatalf("expected one eligible target, got %+v", targets)
	}
	if targets[0].MachineID != "machine-new" {
		t.Fatalf("unexpected listed target: %+v", targets[0])
	}
}

func TestListTargetsHidesPausedTargets(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	paused := false

	seedTarget(server, Target{
		MachineID:       "machine-paused",
		MachineName:     "Paused Mac",
		PublicURL:       "https://paused.example.com",
		AcceptingBuilds: &paused,
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:   "machine-available",
		MachineName: "Available Mac",
		PublicURL:   "https://available.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 1 {
		t.Fatalf("expected one available target, got %+v", targets)
	}
	if targets[0].MachineID != "machine-available" {
		t.Fatalf("unexpected listed target: %+v", targets[0])
	}
}

func TestListTargetsHidesFullQueues(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	seedTarget(server, Target{
		MachineID:        "machine-full",
		MachineName:      "Full Mac",
		PublicURL:        "https://full.example.com",
		QueuedBuilds:     2,
		QueuedBuildLimit: 2,
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:        "machine-available",
		MachineName:      "Available Mac",
		PublicURL:        "https://available.example.com",
		QueuedBuilds:     1,
		QueuedBuildLimit: 2,
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 1 {
		t.Fatalf("expected one available target, got %+v", targets)
	}
	if targets[0].MachineID != "machine-available" {
		t.Fatalf("unexpected listed target: %+v", targets[0])
	}
}

func TestListTargetsPreferLowerLoad(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	seedTarget(server, Target{
		MachineID:      "machine-busy",
		MachineName:    "Busy Mac",
		PublicURL:      "https://busy.example.com",
		ActiveBuilds:   1,
		QueuedBuilds:   0,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:      "machine-queued",
		MachineName:    "Queued Mac",
		PublicURL:      "https://queued.example.com",
		ActiveBuilds:   0,
		QueuedBuilds:   2,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:      "machine-idle",
		MachineName:    "Idle Mac",
		PublicURL:      "https://idle.example.com",
		ActiveBuilds:   0,
		QueuedBuilds:   0,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	got := []string{}
	for _, target := range targets {
		got = append(got, target.MachineID)
	}
	want := []string{"machine-idle", "machine-queued", "machine-busy"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected target order: got %v want %v", got, want)
	}
}

func TestDispatchTargetsPreferLowerLoad(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})

	seedTarget(server, Target{
		MachineID:      "machine-busy",
		MachineName:    "Busy Mac",
		PublicURL:      "https://busy.example.com",
		ActiveBuilds:   1,
		QueuedBuilds:   0,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:      "machine-queued",
		MachineName:    "Queued Mac",
		PublicURL:      "https://queued.example.com",
		ActiveBuilds:   0,
		QueuedBuilds:   2,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:      "machine-idle",
		MachineName:    "Idle Mac",
		PublicURL:      "https://idle.example.com",
		ActiveBuilds:   0,
		QueuedBuilds:   0,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets, ok := server.dispatchTargets(DispatchRequest{JobID: "xcode-debug", RequestID: "run-123"}, httptest.NewRecorder())
	if !ok {
		t.Fatal("expected dispatch targets")
	}
	got := []string{}
	for _, target := range targets {
		got = append(got, target.MachineID)
	}
	want := []string{"machine-idle", "machine-queued", "machine-busy"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected target order: got %v want %v", got, want)
	}
}

func TestDispatchTargetsSkipFullQueues(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})

	seedTarget(server, Target{
		MachineID:        "machine-full",
		MachineName:      "Full Mac",
		PublicURL:        "https://full.example.com",
		QueuedBuilds:     2,
		QueuedBuildLimit: 2,
		Jobs:             []string{"xcode-debug"},
		LeaseExpiresAt:   time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:        "machine-available",
		MachineName:      "Available Mac",
		PublicURL:        "https://available.example.com",
		QueuedBuilds:     1,
		QueuedBuildLimit: 2,
		Jobs:             []string{"xcode-debug"},
		LeaseExpiresAt:   time.Now().Add(time.Minute),
	})

	targets, ok := server.dispatchTargets(DispatchRequest{JobID: "xcode-debug", RequestID: "run-123"}, httptest.NewRecorder())
	if !ok {
		t.Fatal("expected dispatch targets")
	}
	if len(targets) != 1 || targets[0].MachineID != "machine-available" {
		t.Fatalf("expected only available target, got %+v", targets)
	}
}

func TestDispatchTargetsSkipPausedTargets(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	paused := false

	seedTarget(server, Target{
		MachineID:       "machine-paused",
		MachineName:     "Paused Mac",
		PublicURL:       "https://paused.example.com",
		AcceptingBuilds: &paused,
		Jobs:            []string{"xcode-debug"},
		LeaseExpiresAt:  time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:      "machine-available",
		MachineName:    "Available Mac",
		PublicURL:      "https://available.example.com",
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets, ok := server.dispatchTargets(DispatchRequest{JobID: "xcode-debug", RequestID: "run-123"}, httptest.NewRecorder())
	if !ok {
		t.Fatal("expected dispatch targets")
	}
	if len(targets) != 1 || targets[0].MachineID != "machine-available" {
		t.Fatalf("expected only available target, got %+v", targets)
	}
}

func TestPinnedDispatchRejectsPausedTarget(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	paused := false

	seedTarget(server, Target{
		MachineID:       "machine-paused",
		MachineName:     "Paused Mac",
		PublicURL:       "https://paused.example.com",
		AcceptingBuilds: &paused,
		Jobs:            []string{"xcode-debug"},
		LeaseExpiresAt:  time.Now().Add(time.Minute),
	})

	response := httptest.NewRecorder()
	_, ok := server.dispatchTargets(DispatchRequest{
		MachineID: "machine-paused",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, response)
	if ok {
		t.Fatal("expected pinned paused target to be rejected")
	}
	if response.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "target is paused") {
		t.Fatalf("expected paused target error, got %s", response.Body.String())
	}
}

func TestPinnedDispatchRejectsFullQueue(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})

	seedTarget(server, Target{
		MachineID:        "machine-full",
		MachineName:      "Full Mac",
		PublicURL:        "https://full.example.com",
		QueuedBuilds:     2,
		QueuedBuildLimit: 2,
		Jobs:             []string{"xcode-debug"},
		LeaseExpiresAt:   time.Now().Add(time.Minute),
	})

	response := httptest.NewRecorder()
	_, ok := server.dispatchTargets(DispatchRequest{
		MachineID: "machine-full",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, response)
	if ok {
		t.Fatal("expected pinned full target to be rejected")
	}
	if response.Code != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "target queue is full (2/2 queued)") {
		t.Fatalf("expected queue full error, got %s", response.Body.String())
	}
}

func TestDispatchTargetReservationAffectsLoadUntilReleased(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})

	seedTarget(server, Target{
		MachineID:        "machine-nearly-full",
		MachineName:      "Nearly Full Mac",
		PublicURL:        "https://nearly-full.example.com",
		QueuedBuilds:     1,
		QueuedBuildLimit: 2,
		Jobs:             []string{"xcode-debug"},
		LeaseExpiresAt:   time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:        "machine-open",
		MachineName:      "Open Mac",
		PublicURL:        "https://open.example.com",
		QueuedBuilds:     0,
		QueuedBuildLimit: 2,
		Jobs:             []string{"xcode-debug"},
		LeaseExpiresAt:   time.Now().Add(time.Minute),
	})
	activateDispatch(server, DispatchRequest{JobID: "xcode-debug", RequestID: "run-1"})

	reserved, err := server.reserveActiveDispatchTarget("run-1", "machine-nearly-full")
	if err != nil {
		t.Fatalf("reserveActiveDispatchTarget returned error: %v", err)
	}
	if reserved.QueuedBuilds != 2 {
		t.Fatalf("expected reservation to fill queue, got %+v", reserved)
	}

	targets, ok := server.dispatchTargets(DispatchRequest{JobID: "xcode-debug", RequestID: "run-2"}, httptest.NewRecorder())
	if !ok {
		t.Fatal("expected dispatch targets")
	}
	if len(targets) != 1 || targets[0].MachineID != "machine-open" {
		t.Fatalf("expected reservation to hide full target, got %+v", targets)
	}

	server.endDispatch("run-1")
	targets, ok = server.dispatchTargets(DispatchRequest{JobID: "xcode-debug", RequestID: "run-3"}, httptest.NewRecorder())
	if !ok {
		t.Fatal("expected dispatch targets after release")
	}
	got := []string{}
	for _, target := range targets {
		got = append(got, target.MachineID)
	}
	if !slices.Contains(got, "machine-nearly-full") {
		t.Fatalf("expected released target to be eligible again, got %+v", targets)
	}
}

func TestPinnedDispatchReservationAffectsLoadUntilReleased(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})

	seedTarget(server, Target{
		MachineID:        "machine-pinned",
		MachineName:      "Pinned Mac",
		PublicURL:        "https://pinned.example.com",
		QueuedBuilds:     1,
		QueuedBuildLimit: 2,
		Jobs:             []string{"xcode-debug"},
		LeaseExpiresAt:   time.Now().Add(time.Minute),
	})
	activateDispatch(server, DispatchRequest{
		MachineID: "machine-pinned",
		JobID:     "xcode-debug",
		RequestID: "run-1",
	})

	reserved, err := server.reserveActiveDispatchTarget("run-1", "machine-pinned")
	if err != nil {
		t.Fatalf("reserveActiveDispatchTarget returned error: %v", err)
	}
	if reserved.QueuedBuilds != 2 {
		t.Fatalf("expected pinned reservation to fill queue, got %+v", reserved)
	}

	server.endDispatch("run-1")
	targets, ok := server.dispatchTargets(DispatchRequest{
		MachineID: "machine-pinned",
		JobID:     "xcode-debug",
		RequestID: "run-2",
	}, httptest.NewRecorder())
	if !ok {
		t.Fatal("expected pinned dispatch target after release")
	}
	if targets[0].QueuedBuilds != 1 {
		t.Fatalf("expected reservation release, got %+v", targets[0])
	}
}

func TestDispatchTargetsPreservePinnedTargetDespiteLoad(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})

	seedTarget(server, Target{
		MachineID:      "machine-busy",
		MachineName:    "Busy Mac",
		PublicURL:      "https://busy.example.com",
		ActiveBuilds:   1,
		QueuedBuilds:   4,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:      "machine-idle",
		MachineName:    "Idle Mac",
		PublicURL:      "https://idle.example.com",
		ActiveBuilds:   0,
		QueuedBuilds:   0,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets, ok := server.dispatchTargets(DispatchRequest{
		MachineID: "machine-busy",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, httptest.NewRecorder())
	if !ok {
		t.Fatal("expected pinned dispatch target")
	}
	if len(targets) != 1 || targets[0].MachineID != "machine-busy" {
		t.Fatalf("unexpected pinned targets: %+v", targets)
	}
}

func TestDispatchTargetsFilterCapabilityConstraints(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})

	seedTarget(server, Target{
		MachineID:   "machine-small",
		MachineName: "Small Mac",
		PublicURL:   "https://small.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
			CPUCount:     8,
			MemoryBytes:  17179869184,
			XcodeVersion: "Xcode 16.2 (Build version 16C100)",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	seedTarget(server, Target{
		MachineID:   "machine-large",
		MachineName: "Large Mac",
		PublicURL:   "https://large.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
			CPUCount:     14,
			MemoryBytes:  68719476736,
			XcodeVersion: "Xcode 16.4 (Build version 16F6)",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	targets, ok := server.dispatchTargets(DispatchRequest{
		JobID:           "xcode-debug",
		RequestID:       "run-123",
		MinCPUCount:     12,
		MinMemoryBytes:  34359738368,
		MinXcodeVersion: "16.4",
	}, httptest.NewRecorder())
	if !ok {
		t.Fatal("expected constrained dispatch target")
	}
	if len(targets) != 1 || targets[0].MachineID != "machine-large" {
		t.Fatalf("unexpected constrained targets: %+v", targets)
	}
}

func TestPinnedDispatchRejectsTargetThatMissesConstraints(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	seedTarget(server, Target{
		MachineID:   "machine-small",
		MachineName: "Small Mac",
		PublicURL:   "https://small.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
			CPUCount:     8,
			MemoryBytes:  17179869184,
			XcodeVersion: "Xcode 16.2 (Build version 16C100)",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	response := httptest.NewRecorder()
	_, ok := server.dispatchTargets(DispatchRequest{
		MachineID:       "machine-small",
		JobID:           "xcode-debug",
		RequestID:       "run-123",
		MinCPUCount:     12,
		MinMemoryBytes:  34359738368,
		MinXcodeVersion: "16.4",
	}, response)

	if ok {
		t.Fatal("expected pinned target to be rejected")
	}
	body := response.Body.String()
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body: %s", response.Code, body)
	}
	if !strings.Contains(body, "cpu_count") || !strings.Contains(body, "memory_bytes") || !strings.Contains(body, "xcode_version") {
		t.Fatalf("constraint error did not include all failures: %s", body)
	}
}

func TestNewRequiresTokens(t *testing.T) {
	if _, err := New(Options{TranswarpToken: "runner-token", PublicURL: "https://coordinator.test"}); err == nil || !strings.Contains(err.Error(), "coordinator token is required") {
		t.Fatalf("expected missing coordinator token error, got %v", err)
	}
	if _, err := New(Options{Token: "coord-token", PublicURL: "https://coordinator.test"}); err == nil || !strings.Contains(err.Error(), "transwarp runner token is required") {
		t.Fatalf("expected missing runner token error, got %v", err)
	}
}

func TestNewRejectsHeaderControlCharacters(t *testing.T) {
	tests := []struct {
		name    string
		options Options
	}{
		{
			name: "coordinator token",
			options: Options{
				Token:          "coord-token\nInjected: yes",
				TranswarpToken: "runner-token",
				PublicURL:      "https://coordinator.test",
			},
		},
		{
			name: "runner token",
			options: Options{
				Token:          "coord-token",
				TranswarpToken: "runner-token\nInjected: yes",
				PublicURL:      "https://coordinator.test",
			},
		},
		{
			name: "target token",
			options: Options{
				Token:          "coord-token",
				TargetToken:    "target-token\nInjected: yes",
				TranswarpToken: "runner-token",
				PublicURL:      "https://coordinator.test",
			},
		},
		{
			name: "access id",
			options: Options{
				Token:              "coord-token",
				TranswarpToken:     "runner-token",
				AccessClientID:     "access-id\rInjected: yes",
				AccessClientSecret: "access-secret",
				PublicURL:          "https://coordinator.test",
			},
		},
		{
			name: "access secret",
			options: Options{
				Token:              "coord-token",
				TranswarpToken:     "runner-token",
				AccessClientID:     "access-id",
				AccessClientSecret: "access-secret\nInjected: yes",
				PublicURL:          "https://coordinator.test",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.options)
			if err == nil || !strings.Contains(err.Error(), "single HTTP header value") {
				t.Fatalf("expected header value error, got %v", err)
			}
		})
	}
}

func TestNewRequiresCompleteAccessCredentials(t *testing.T) {
	if _, err := New(Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		AccessClientID: "access-id",
		PublicURL:      "https://coordinator.test",
	}); err == nil || !strings.Contains(err.Error(), "provided together") {
		t.Fatalf("expected partial Access credentials error, got %v", err)
	}
}

func TestNewValidatesPublicURL(t *testing.T) {
	if _, err := New(Options{Token: "coord-token", TranswarpToken: "runner-token"}); err == nil || !strings.Contains(err.Error(), "public URL is required") {
		t.Fatalf("expected missing public URL error, got %v", err)
	}
	if _, err := New(Options{Token: "coord-token", TranswarpToken: "runner-token", PublicURL: "http://coordinator.example.com"}); err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("expected remote HTTP public URL error, got %v", err)
	}
	if _, err := New(Options{Token: "coord-token", TranswarpToken: "runner-token", PublicURL: "https://user:password@coordinator.example.com"}); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credentialed public URL error, got %v", err)
	}
	if _, err := New(Options{Token: "coord-token", TranswarpToken: "runner-token", PublicURL: "http://127.0.0.1:8288"}); err != nil {
		t.Fatalf("expected loopback public URL to be accepted, got %v", err)
	}
}

func TestCoordinatorRejectsMissingBearerToken(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/transwarp/targets", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected missing-token status: %s", response.Status)
	}
}

func TestCoordinatorRejectsUnsafeDispatchMetadata(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123","ref":"--help"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "ref") {
		t.Fatalf("expected ref validation error, got %s", string(data))
	}
	if len(server.active) != 0 {
		t.Fatalf("invalid dispatch should not be marked active: %+v", server.active)
	}
}

func TestCoordinatorRejectsNegativeDispatchConstraints(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123","min_cpu_count":-1}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "min_cpu_count") {
		t.Fatalf("expected min_cpu_count validation error, got %s", string(data))
	}
	if len(server.active) != 0 {
		t.Fatalf("invalid dispatch should not be marked active: %+v", server.active)
	}
}

func TestCoordinatorRejectsInvalidMinXcodeVersionBeforeActiveDispatch(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123","min_xcode_version":"latest"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "min_xcode_version") {
		t.Fatalf("expected min_xcode_version validation error, got %s", string(data))
	}
	if len(server.active) != 0 {
		t.Fatalf("invalid dispatch should not be marked active: %+v", server.active)
	}
}

func TestListTargetsExpiresLeases(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		Listen:         "127.0.0.1:8188",
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(-time.Second),
	})

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 0 {
		t.Fatalf("expected expired target to be removed, got %d", len(targets))
	}
}

func TestListTargetsReleasesUnacceptedDispatchForExpiredLease(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:        "machine-123",
		MachineName:      "Mac Studio",
		PublicURL:        "https://transwarp.example.com",
		QueuedBuildLimit: 1,
		Jobs:             []string{"xcode-debug"},
		LeaseExpiresAt:   time.Now().Add(time.Minute),
	})
	activateDispatch(server, DispatchRequest{
		MachineID: "machine-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	})
	if _, err := server.reserveActiveDispatchTarget("run-123", "machine-123"); err != nil {
		t.Fatalf("reserveActiveDispatchTarget returned error: %v", err)
	}
	server.mu.Lock()
	target := server.targets["machine-123"]
	target.LeaseExpiresAt = time.Now().Add(-time.Second)
	server.targets["machine-123"] = target
	server.mu.Unlock()

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 0 {
		t.Fatalf("expected expired target to be removed, got %d", len(targets))
	}
	server.mu.Lock()
	_, active := server.active["run-123"]
	reservations := server.targetReservations["machine-123"]
	server.mu.Unlock()
	if active {
		t.Fatalf("unaccepted active dispatch remained after target lease expiry: %+v", server.active)
	}
	if reservations != 0 {
		t.Fatalf("target reservation remained after target lease expiry: %d", reservations)
	}
}

func TestListTargetsPreservesAcceptedDispatchForExpiredLeaseResultCallback(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      "https://runner.example.com",
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	activateDispatch(server, DispatchRequest{
		MachineID: "machine-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	})
	if _, err := server.reserveActiveDispatchTarget("run-123", "machine-123"); err != nil {
		t.Fatalf("reserveActiveDispatchTarget returned error: %v", err)
	}
	if err := server.markActiveDispatchBuild("run-123", "machine-123", "https://runner.example.com", "build-123"); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	target := server.targets["machine-123"]
	target.LeaseExpiresAt = time.Now().Add(-time.Second)
	server.targets["machine-123"] = target
	server.mu.Unlock()

	targets := getTargets(t, testServer.URL, "coord-token")
	if len(targets) != 0 {
		t.Fatalf("expected expired target to be removed, got %d", len(targets))
	}
	result := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
		PublicURL: "https://runner.example.com",
	})
	defer result.Body.Close()
	if result.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(result.Body)
		t.Fatalf("unexpected result status after target lease expiry: %s body: %s", result.Status, string(data))
	}
}

func TestRecordAndListResult(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		MachineID: "machine-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	body := bytes.NewBufferString(`{"build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","machine":"Mac Studio","status":"passed","exit_code":0}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/result", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected result status: %s", response.Status)
	}

	listRequest, err := http.NewRequest(http.MethodGet, testServer.URL+"/transwarp/results", nil)
	if err != nil {
		t.Fatal(err)
	}
	listRequest.Header.Set("Authorization", "Bearer coord-token")
	listResponse, err := testServer.Client().Do(listRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer listResponse.Body.Close()

	var results []BuildResult
	if err := json.NewDecoder(listResponse.Body).Decode(&results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].RequestID != "run-123" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestGetResultByRequestID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	seedResult(server, BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})

	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/transwarp/results/run-123", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get result status: %s", response.Status)
	}

	var result BuildResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.RequestID != "run-123" || result.BuildID != "build-123" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGetResultByRequestIDRequiresCIToken(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TargetToken:    "target-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	seedResult(server, BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})

	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/transwarp/results/run-123", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer target-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("target token should not read CI results, got %s", response.Status)
	}
}

func TestGetResultByRequestIDRejectsUnsafeRequestID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/transwarp/results/run%2F123", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected unsafe request status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "request_id") {
		t.Fatalf("expected request_id validation error, got %s", string(data))
	}
}

func TestGetResultByRequestIDReturnsNotFound(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	request, err := http.NewRequest(http.MethodGet, testServer.URL+"/transwarp/results/run-123", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected missing result status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "unknown request_id") {
		t.Fatalf("expected missing request_id error, got %s", string(data))
	}
}

func TestRecordResultRejectsResultWithoutActiveDispatch(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected orphan result status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "active dispatch") {
		t.Fatalf("expected active dispatch rejection, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("orphan result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsMissingBuildID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected missing build_id status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "build_id is required") {
		t.Fatalf("expected build_id rejection, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("missing build_id result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsUnsafeRequestID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run/123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected unsafe request_id status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "request_id") {
		t.Fatalf("expected request_id rejection, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("unsafe request_id result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsUnsafeBuildID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build/123")

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build/123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected unsafe build_id status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "build_id") {
		t.Fatalf("expected build_id rejection, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("unsafe build_id result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsUnsafeJobID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode/debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected unsafe job_id status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "job_id") {
		t.Fatalf("expected job_id rejection, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("unsafe job_id result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsUnsafeMachineID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine/123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected unsafe machine_id status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "machine_id") {
		t.Fatalf("expected machine_id rejection, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("unsafe machine_id result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsInvalidStatus(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "running",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected invalid status result status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "status must be passed, failed, or canceled") {
		t.Fatalf("expected status validation error, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("invalid status result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsInconsistentStatus(t *testing.T) {
	tests := []struct {
		name      string
		result    BuildResult
		wantError string
	}{
		{
			name: "passed with error",
			result: BuildResult{
				Status:   "passed",
				ExitCode: 65,
				Error:    "xcodebuild exited 65",
			},
			wantError: "passed results",
		},
		{
			name: "failed with clean exit",
			result: BuildResult{
				Status:   "failed",
				ExitCode: 0,
			},
			wantError: "failed results",
		},
		{
			name: "canceled without marker",
			result: BuildResult{
				Status:   "canceled",
				ExitCode: -1,
				Error:    "context canceled",
			},
			wantError: "canceled results",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, Options{
				Token:          "coord-token",
				TranswarpToken: "runner-token",
				PublicURL:      "https://coordinator.test",
			})
			testServer := httptest.NewServer(server.Handler())
			defer testServer.Close()

			activateStartedDispatch(server, DispatchRequest{
				JobID:     "xcode-debug",
				RequestID: "run-123",
			}, "machine-123", "build-123")

			result := test.result
			result.BuildID = "build-123"
			result.JobID = "xcode-debug"
			result.RequestID = "run-123"
			result.MachineID = "machine-123"
			result.Machine = "Mac Studio"
			response := recordResult(t, testServer.URL, "coord-token", result)
			defer response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("unexpected inconsistent result status: %s", response.Status)
			}
			data, _ := io.ReadAll(response.Body)
			if !strings.Contains(string(data), test.wantError) {
				t.Fatalf("expected %q validation error, got %s", test.wantError, string(data))
			}
			results := getResults(t, testServer.URL, "coord-token")
			if len(results) != 0 {
				t.Fatalf("inconsistent result was recorded: %+v", results)
			}
		})
	}
}

func TestRecordResultRejectsInconsistentTiming(t *testing.T) {
	startedAt := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(5 * time.Second)

	tests := []struct {
		name      string
		result    BuildResult
		wantError string
	}{
		{
			name: "negative duration",
			result: BuildResult{
				Status:     "passed",
				ExitCode:   0,
				DurationMS: -1,
			},
			wantError: "duration_ms must not be negative",
		},
		{
			name: "only started at",
			result: BuildResult{
				Status:     "passed",
				ExitCode:   0,
				StartedAt:  startedAt,
				DurationMS: 5000,
			},
			wantError: "started_at and ended_at must be recorded together",
		},
		{
			name: "ended before started",
			result: BuildResult{
				Status:     "passed",
				ExitCode:   0,
				StartedAt:  endedAt,
				EndedAt:    startedAt,
				DurationMS: 5000,
			},
			wantError: "ended_at must not be before started_at",
		},
		{
			name: "duration mismatch",
			result: BuildResult{
				Status:     "passed",
				ExitCode:   0,
				StartedAt:  startedAt,
				EndedAt:    endedAt,
				DurationMS: 4000,
			},
			wantError: "duration_ms must match started_at and ended_at",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, Options{
				Token:          "coord-token",
				TranswarpToken: "runner-token",
				PublicURL:      "https://coordinator.test",
			})
			testServer := httptest.NewServer(server.Handler())
			defer testServer.Close()

			activateStartedDispatch(server, DispatchRequest{
				JobID:     "xcode-debug",
				RequestID: "run-123",
			}, "machine-123", "build-123")

			result := test.result
			result.BuildID = "build-123"
			result.JobID = "xcode-debug"
			result.RequestID = "run-123"
			result.MachineID = "machine-123"
			result.Machine = "Mac Studio"
			response := recordResult(t, testServer.URL, "coord-token", result)
			defer response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("unexpected inconsistent timing result status: %s", response.Status)
			}
			data, _ := io.ReadAll(response.Body)
			if !strings.Contains(string(data), test.wantError) {
				t.Fatalf("expected %q validation error, got %s", test.wantError, string(data))
			}
			results := getResults(t, testServer.URL, "coord-token")
			if len(results) != 0 {
				t.Fatalf("inconsistent timing result was recorded: %+v", results)
			}
		})
	}
}

func TestRecordResultAllowsSmallDurationTimestampSkew(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	startedAt := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:    "build-123",
		JobID:      "xcode-debug",
		RequestID:  "run-123",
		MachineID:  "machine-123",
		Machine:    "Mac Studio",
		StartedAt:  startedAt,
		EndedAt:    startedAt.Add(5*time.Second + 75*time.Millisecond),
		ExitCode:   143,
		Status:     "canceled",
		Error:      "build canceled",
		DurationMS: 5000,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected skewed timing result status: %s %s", response.Status, string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 1 {
		t.Fatalf("expected skewed timing result to be recorded, got %+v", results)
	}
}

func TestRecordResultRejectsInvalidPublicURL(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateStartedDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
		PublicURL: "https://runner.example.com/status",
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected invalid public_url result status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "result public_url must be a base URL") {
		t.Fatalf("expected public_url validation error, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("invalid public_url result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsResultBeforeRunnerBuildAccepted(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	activateDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	})

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected pre-start result status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "before a runner build is accepted") {
		t.Fatalf("expected accepted runner build rejection, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("pre-start result was recorded: %+v", results)
	}
}

func TestRecordResultWaitsForAcceptedRunnerBuildRace(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	server.mu.Lock()
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			JobID:     "xcode-debug",
			RequestID: "run-123",
		},
		TargetMachineID: "machine-123",
	}
	server.mu.Unlock()

	callbackDone := make(chan int, 1)
	go func() {
		response := recordResult(t, testServer.URL, "coord-token", BuildResult{
			BuildID:   "build-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
			MachineID: "machine-123",
			Machine:   "Mac Studio",
			Status:    "passed",
			ExitCode:  0,
		})
		defer response.Body.Close()
		callbackDone <- response.StatusCode
	}()

	time.Sleep(25 * time.Millisecond)
	server.markActiveDispatchBuild("run-123", "machine-123", "http://127.0.0.1:18188", "build-123")

	select {
	case statusCode := <-callbackDone:
		if statusCode != http.StatusAccepted {
			t.Fatalf("unexpected race callback status: %d", statusCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("result callback did not complete")
	}

	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 1 || results[0].BuildID != "build-123" {
		t.Fatalf("race result was not recorded: %+v", results)
	}
}

func TestRecordResultAcceptsIdempotentDuplicate(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	result := BuildResult{
		BuildID:    "build-123",
		JobID:      "xcode-debug",
		RequestID:  "run-123",
		MachineID:  "machine-123",
		Machine:    "Mac Studio",
		Status:     "passed",
		ExitCode:   0,
		StartedAt:  time.Date(2026, 7, 26, 1, 0, 0, 0, time.UTC),
		EndedAt:    time.Date(2026, 7, 26, 1, 1, 0, 0, time.UTC),
		DurationMS: 60000,
	}
	activateStartedDispatch(server, DispatchRequest{
		JobID:     result.JobID,
		RequestID: result.RequestID,
	}, result.MachineID, result.BuildID)

	first := recordResult(t, testServer.URL, "coord-token", result)
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected first result status: %s", first.Status)
	}
	first.Body.Close()
	second := recordResult(t, testServer.URL, "coord-token", result)
	defer second.Body.Close()
	if second.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected duplicate result status: %s", second.Status)
	}

	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 1 || results[0].RequestID != "run-123" {
		t.Fatalf("unexpected results after duplicate: %+v", results)
	}
}

func TestRecordResultRejectsConflictingDuplicate(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	result := BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	}
	activateStartedDispatch(server, DispatchRequest{
		JobID:     result.JobID,
		RequestID: result.RequestID,
	}, result.MachineID, result.BuildID)
	first := recordResult(t, testServer.URL, "coord-token", result)
	first.Body.Close()
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected first result status: %s", first.Status)
	}

	result.Status = "failed"
	result.ExitCode = 65
	result.Error = "xcodebuild exited 65"
	second := recordResult(t, testServer.URL, "coord-token", result)
	defer second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected conflicting result status: %s", second.Status)
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 1 || results[0].Status != "passed" {
		t.Fatalf("conflicting result rewrote ledger: %+v", results)
	}
}

func TestRecordResultRejectsActiveDispatchMismatch(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	server.mu.Lock()
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			MachineID: "machine-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
			RepoURL:   "https://github.com/example/app.git",
			Ref:       "refs/heads/main",
			Commit:    "abc123",
		},
		TargetMachineID: "machine-123",
		RunnerBuildID:   "build-123",
	}
	server.mu.Unlock()

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "release",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		RepoURL:   "https://github.com/example/app.git",
		Ref:       "refs/heads/main",
		Commit:    "abc123",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected mismatch result status: %s", response.Status)
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("mismatched active result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsSelectedRunnerBuildMismatch(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	server.mu.Lock()
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			MachineID: "machine-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
		},
		TargetMachineID: "machine-123",
		RunnerBuildID:   "build-accepted",
	}
	server.mu.Unlock()

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-other",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected mismatch result status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "selected runner build") {
		t.Fatalf("expected runner build mismatch, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("mismatched build result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsMissingPublicURLForSelectedPublicRunner(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	server.mu.Lock()
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			MachineID: "machine-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
		},
		TargetMachineID: "machine-123",
		RunnerBaseURL:   "https://runner.example.com",
		RunnerBuildID:   "build-accepted",
	}
	server.mu.Unlock()

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-accepted",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected missing public_url status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "result public_url is required for selected public runner") {
		t.Fatalf("expected missing public_url rejection, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("missing public_url result was recorded: %+v", results)
	}
}

func TestRecordResultAllowsMissingPublicURLForSelectedLoopbackRunner(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	server.mu.Lock()
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			MachineID: "machine-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
		},
		TargetMachineID: "machine-123",
		RunnerBaseURL:   "http://127.0.0.1:8188",
		RunnerBuildID:   "build-accepted",
	}
	server.mu.Unlock()

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-accepted",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected loopback result status: %s", response.Status)
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 1 || results[0].RequestID != "run-123" {
		t.Fatalf("unexpected loopback results: %+v", results)
	}
}

func TestRecordResultRejectsSelectedRunnerPublicURLMismatch(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	server.mu.Lock()
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			MachineID: "machine-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
		},
		TargetMachineID: "machine-123",
		RunnerBaseURL:   "https://runner.example.com",
		RunnerBuildID:   "build-accepted",
	}
	server.mu.Unlock()

	response := recordResult(t, testServer.URL, "coord-token", BuildResult{
		BuildID:   "build-accepted",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
		PublicURL: "https://other-runner.example.com",
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected public_url mismatch status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "result public_url does not match selected runner") {
		t.Fatalf("expected public_url mismatch, got %s", string(data))
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("mismatched public_url result was recorded: %+v", results)
	}
}

func TestRecordResultRejectsUnknownJSONFields(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	activateDispatch(server, DispatchRequest{JobID: "xcode-debug", RequestID: "run-123"})

	body := bytes.NewBufferString(`{"build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","machine":"Mac Studio","status":"passed","exit_code":0,"unexpected":true}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/result", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected result status: %s", response.Status)
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("unknown result field should not be recorded: %+v", results)
	}
}

func TestStatePersistsTargetsAndResults(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "coordinator-state.json")
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})
	testServer := httptest.NewServer(server.Handler())

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		Listen:         "127.0.0.1:8188",
		PublicURL:      "https://transwarp.example.com",
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	activateStartedDispatch(server, DispatchRequest{
		MachineID: "machine-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, "machine-123", "build-123")

	body := bytes.NewBufferString(`{"build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","machine":"Mac Studio","status":"passed","exit_code":0}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/result", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	testServer.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected result status: %s", response.Status)
	}

	restarted := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})
	restartedServer := httptest.NewServer(restarted.Handler())
	defer restartedServer.Close()

	targets := getTargets(t, restartedServer.URL, "coord-token")
	if len(targets) != 1 || targets[0].MachineID != "machine-123" {
		t.Fatalf("unexpected persisted targets: %+v", targets)
	}

	results := getResults(t, restartedServer.URL, "coord-token")
	if len(results) != 1 || results[0].RequestID != "run-123" {
		t.Fatalf("unexpected persisted results: %+v", results)
	}
}

func TestStatePersistsAcceptedActiveDispatchForRestartedResultCallback(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "coordinator-state.json")
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			MachineID: "machine-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
			RepoURL:   "https://github.com/example/app.git",
			Ref:       "refs/heads/main",
			Commit:    "0123456789abcdef0123456789abcdef01234567",
		},
		TargetMachineID: "machine-123",
	}
	if err := server.markActiveDispatchBuild("run-123", "machine-123", "https://runner.example.com", "build-123"); err != nil {
		t.Fatal(err)
	}

	restarted := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})
	restartedServer := httptest.NewServer(restarted.Handler())
	defer restartedServer.Close()

	body := bytes.NewBufferString(`{
		"build_id": "build-123",
		"job_id": "xcode-debug",
		"request_id": "run-123",
		"machine_id": "machine-123",
		"machine": "Mac Studio",
		"repo_url": "https://github.com/example/app.git",
		"ref": "refs/heads/main",
		"commit": "0123456789abcdef0123456789abcdef01234567",
		"status": "passed",
		"exit_code": 0,
		"public_url": "https://runner.example.com"
	}`)
	request, err := http.NewRequest(http.MethodPost, restartedServer.URL+"/transwarp/result", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := restartedServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected restarted result status: %s body: %s", response.Status, string(data))
	}

	results := getResults(t, restartedServer.URL, "coord-token")
	if len(results) != 1 || results[0].BuildID != "build-123" {
		t.Fatalf("restarted coordinator did not record result: %+v", results)
	}
	loaded := readStateFile(t, statePath)
	if len(loaded.Active) != 0 {
		t.Fatalf("completed active dispatch remained persisted: %+v", loaded.Active)
	}
	if len(loaded.Results) != 1 || loaded.Results["run-123"].BuildID != "build-123" {
		t.Fatalf("completed result was not persisted: %+v", loaded.Results)
	}
}

func TestStateRejectsRestartedResultCallbackSourceMismatch(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "coordinator-state.json")
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			MachineID: "machine-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
			RepoURL:   "https://github.com/example/app.git",
			Ref:       "refs/heads/main",
			Commit:    "0123456789abcdef0123456789abcdef01234567",
		},
		TargetMachineID: "machine-123",
	}
	if err := server.markActiveDispatchBuild("run-123", "machine-123", "https://runner.example.com", "build-123"); err != nil {
		t.Fatal(err)
	}

	restarted := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})
	restartedServer := httptest.NewServer(restarted.Handler())
	defer restartedServer.Close()

	response := recordResult(t, restartedServer.URL, "coord-token", BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		RepoURL:   "https://github.com/example/other.git",
		Ref:       "refs/heads/main",
		Commit:    "0123456789abcdef0123456789abcdef01234567",
		Status:    "passed",
		ExitCode:  0,
		PublicURL: "https://runner.example.com",
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected restarted mismatch status: %s", response.Status)
	}
	data, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(data), "active dispatch") {
		t.Fatalf("expected active dispatch mismatch, got %s", string(data))
	}

	results := getResults(t, restartedServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("mismatched restarted result was recorded: %+v", results)
	}
	loaded := readStateFile(t, statePath)
	if len(loaded.Active) != 1 || loaded.Active["run-123"].RunnerBuildID != "build-123" {
		t.Fatalf("restarted mismatch should leave active dispatch persisted: %+v", loaded.Active)
	}
	if len(loaded.Results) != 0 {
		t.Fatalf("restarted mismatch should not persist result: %+v", loaded.Results)
	}
}

func TestStatePersistsActiveDispatchWithStableJSONKeys(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "coordinator-state.json")
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})
	server.active["run-123"] = activeDispatch{
		Request: DispatchRequest{
			MachineID: "machine-123",
			JobID:     "xcode-debug",
			RequestID: "run-123",
		},
		TargetMachineID: "machine-123",
	}
	if err := server.markActiveDispatchBuild("run-123", "machine-123", "https://runner.example.com", "build-123"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	stateJSON := string(data)
	for _, key := range []string{`"request"`, `"target_machine_id"`, `"runner_base_url"`, `"runner_build_id"`} {
		if !strings.Contains(stateJSON, key) {
			t.Fatalf("state does not contain stable key %s: %s", key, stateJSON)
		}
	}
	for _, key := range []string{`"Request"`, `"TargetMachineID"`, `"RunnerBaseURL"`, `"RunnerBuildID"`} {
		if strings.Contains(stateJSON, key) {
			t.Fatalf("state contains legacy key %s: %s", key, stateJSON)
		}
	}
}

func TestStateLoadsLegacyActiveDispatchKeys(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "coordinator-state.json")
	data := []byte(`{
		"active": {
			"run-123": {
				"Request": {
					"machine_id": "machine-123",
					"job_id": "xcode-debug",
					"request_id": "run-123"
				},
				"TargetMachineID": "machine-123",
				"RunnerBaseURL": "https://runner.example.com",
				"RunnerBuildID": "build-123"
			}
		}
	}`)
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	restarted := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})
	if len(restarted.active) != 1 {
		t.Fatalf("legacy active dispatch was not loaded: %+v", restarted.active)
	}
	active := restarted.active["run-123"]
	if active.Request.RequestID != "run-123" || active.TargetMachineID != "machine-123" || active.RunnerBaseURL != "https://runner.example.com" || active.RunnerBuildID != "build-123" {
		t.Fatalf("unexpected legacy active dispatch: %+v", active)
	}
}

func TestStateLoadDropsExpiredAndInvalidTargetsAndCapsLeases(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "coordinator-state.json")
	farFutureLease := time.Now().Add(24 * time.Hour)
	state := stateFile{
		Targets: map[string]Target{
			"machine-valid": {
				MachineID:      "machine-valid",
				MachineName:    "Mac Studio",
				Listen:         "127.0.0.1:8188",
				PublicURL:      "https://transwarp.example.com",
				Capabilities:   validTargetCapabilities(),
				Jobs:           []string{"xcode-debug"},
				LeaseExpiresAt: farFutureLease,
			},
			"machine-expired": {
				MachineID:      "machine-expired",
				MachineName:    "Offline Mac",
				Listen:         "127.0.0.1:8188",
				Jobs:           []string{"xcode-debug"},
				LeaseExpiresAt: time.Now().Add(-time.Minute),
			},
			"machine-invalid": {
				MachineID:      "machine-invalid",
				MachineName:    "Wrong Address Mac",
				Listen:         "10.0.0.12:8188",
				Jobs:           []string{"xcode-debug"},
				LeaseExpiresAt: time.Now().Add(time.Minute),
			},
			"machine-negative-load": {
				MachineID:        "machine-negative-load",
				MachineName:      "Impossible Load Mac",
				Listen:           "127.0.0.1:8189",
				Capabilities:     validTargetCapabilities(),
				QueuedBuilds:     -1,
				QueuedBuildLimit: 1,
				Jobs:             []string{"xcode-debug"},
				LeaseExpiresAt:   time.Now().Add(time.Minute),
			},
			"wrong-key": {
				MachineID:      "machine-wrong-key",
				MachineName:    "Wrong Key Mac",
				Listen:         "127.0.0.1:8188",
				PublicURL:      "https://wrong-key.example.com",
				Capabilities:   validTargetCapabilities(),
				Jobs:           []string{"xcode-debug"},
				LeaseExpiresAt: time.Now().Add(time.Minute),
			},
		},
		Results: map[string]BuildResult{
			"run-123": {
				BuildID:   "build-123",
				JobID:     "xcode-debug",
				RequestID: "run-123",
				MachineID: "machine-valid",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  0,
			},
			"missing-build": {
				JobID:     "xcode-debug",
				RequestID: "missing-build",
				MachineID: "machine-valid",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  0,
			},
			"wrong-key": {
				BuildID:   "build-wrong-key",
				JobID:     "xcode-debug",
				RequestID: "different-request",
				MachineID: "machine-valid",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  0,
			},
			"unsafe-machine": {
				BuildID:   "build-unsafe-machine",
				JobID:     "xcode-debug",
				RequestID: "unsafe-machine",
				MachineID: "machine/valid",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  0,
			},
			"inconsistent-passed": {
				BuildID:   "build-inconsistent",
				JobID:     "xcode-debug",
				RequestID: "inconsistent-passed",
				MachineID: "machine-valid",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  65,
			},
		},
		Active: map[string]activeDispatch{
			"run-active": {
				Request: DispatchRequest{
					JobID:     "xcode-debug",
					RequestID: "run-active",
				},
				TargetMachineID: "machine-valid",
				RunnerBaseURL:   "https://runner.example.com",
				RunnerBuildID:   "build-active",
			},
			"pre-start": {
				Request: DispatchRequest{
					JobID:     "xcode-debug",
					RequestID: "pre-start",
				},
				TargetMachineID: "machine-valid",
			},
			"unsafe-runner": {
				Request: DispatchRequest{
					JobID:     "xcode-debug",
					RequestID: "unsafe-runner",
				},
				TargetMachineID: "machine-valid",
				RunnerBaseURL:   "http://runner.example.com",
				RunnerBuildID:   "build-unsafe",
			},
			"invalid-min-xcode": {
				Request: DispatchRequest{
					JobID:           "xcode-debug",
					RequestID:       "invalid-min-xcode",
					MinXcodeVersion: "latest",
				},
				TargetMachineID: "machine-valid",
				RunnerBaseURL:   "https://runner.example.com",
				RunnerBuildID:   "build-invalid-min-xcode",
			},
			"completed": {
				Request: DispatchRequest{
					JobID:     "xcode-debug",
					RequestID: "run-123",
				},
				TargetMachineID: "machine-valid",
				RunnerBaseURL:   "https://runner.example.com",
				RunnerBuildID:   "build-123",
			},
		},
	}
	writeStateFile(t, statePath, state)

	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})

	targets := server.eligibleTargets()
	if len(targets) != 1 || targets[0].MachineID != "machine-valid" {
		t.Fatalf("unexpected loaded targets: %+v", targets)
	}
	if !targets[0].LeaseExpiresAt.Before(farFutureLease) {
		t.Fatalf("lease was not capped on load: %s", targets[0].LeaseExpiresAt)
	}
	if len(server.results) != 1 || server.results["run-123"].Status != "passed" {
		t.Fatalf("unexpected normalized results: %+v", server.results)
	}
	if len(server.active) != 1 || server.active["run-active"].RunnerBuildID != "build-active" {
		t.Fatalf("unexpected normalized active dispatches: %+v", server.active)
	}
	loaded := readStateFile(t, statePath)
	if len(loaded.Targets) != 1 || loaded.Targets["machine-valid"].MachineID != "machine-valid" {
		t.Fatalf("invalid targets were not pruned from persisted state: %+v", loaded.Targets)
	}
	if len(loaded.Results) != 1 || loaded.Results["run-123"].BuildID != "build-123" {
		t.Fatalf("invalid results were not pruned from persisted state: %+v", loaded.Results)
	}
	if len(loaded.Active) != 1 || loaded.Active["run-active"].RunnerBuildID != "build-active" {
		t.Fatalf("invalid active dispatches were not pruned from persisted state: %+v", loaded.Active)
	}
}

func TestListTargetsPersistsExpiredTargetPruning(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "coordinator-state.json")
	state := stateFile{
		Targets: map[string]Target{
			"machine-expired": {
				MachineID:      "machine-expired",
				MachineName:    "Offline Mac",
				Listen:         "127.0.0.1:8188",
				Jobs:           []string{"xcode-debug"},
				LeaseExpiresAt: time.Now().Add(-time.Minute),
			},
			"machine-active": {
				MachineID:      "machine-active",
				MachineName:    "Mac Studio",
				Listen:         "127.0.0.1:8188",
				PublicURL:      "https://transwarp.example.com",
				Capabilities:   validTargetCapabilities(),
				Jobs:           []string{"xcode-debug"},
				LeaseExpiresAt: time.Now().Add(time.Minute),
			},
		},
	}
	writeStateFile(t, statePath, state)
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		StatePath:      statePath,
	})

	targets := server.activeTargets()
	if len(targets) != 1 || targets[0].MachineID != "machine-active" {
		t.Fatalf("unexpected active targets: %+v", targets)
	}
	loaded := readStateFile(t, statePath)
	if _, ok := loaded.Targets["machine-expired"]; ok {
		t.Fatalf("expired target remained persisted: %+v", loaded.Targets)
	}
	if _, ok := loaded.Targets["machine-active"]; !ok {
		t.Fatalf("active target was not persisted: %+v", loaded.Targets)
	}
}

func TestDispatchUsesRegisteredTarget(t *testing.T) {
	var receivedAuth string
	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	reportErrors := make(chan error, 1)
	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedAuth = request.Header.Get("Authorization")
		switch request.URL.Path {
		case "/v1/builds":
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			buildMu.Lock()
			currentBuild := buildRequest
			buildMu.Unlock()
			reportErrors <- postCallbackResult(runner.Client(), currentBuild, BuildResult{
				JobID:     "xcode-debug",
				RequestID: "run-123",
				MachineID: "machine-123",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  0,
			})
			response.Header().Set("Content-Type", "application/x-ndjson")
			response.Write([]byte(`{"kind":"log","message":"from registered runner","sequence":1}` + "\n"))
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":2}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:          "coord-token",
		TargetToken:    "target-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		HTTPClient:     runner.Client(),
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "target-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var output bytes.Buffer
	if _, err := output.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if receivedAuth != "Bearer runner-token" {
		t.Fatalf("unexpected runner auth: %s", receivedAuth)
	}
	buildMu.Lock()
	reportToken := buildRequest.ReportToken
	buildMu.Unlock()
	if reportToken != "target-token" {
		t.Fatalf("runner result callback token should use target token, got %q", reportToken)
	}
	if !strings.Contains(output.String(), "from registered runner") {
		t.Fatalf("dispatch output missing runner log: %s", output.String())
	}
	if !strings.Contains(output.String(), `"build_id":"build-123"`) {
		t.Fatalf("dispatch output missing accepted runner build ID: %s", output.String())
	}
	if !strings.Contains(output.String(), `"request_id":"run-123"`) {
		t.Fatalf("dispatch output missing request ID metadata: %s", output.String())
	}
	if !strings.Contains(output.String(), `"machine_id":"machine-123"`) {
		t.Fatalf("dispatch output missing selected machine metadata: %s", output.String())
	}
	if !strings.Contains(output.String(), fmt.Sprintf(`"public_url":%q`, runner.URL)) {
		t.Fatalf("dispatch output missing selected runner URL metadata: %s", output.String())
	}
	if !strings.Contains(output.String(), "[result] recorded passed") {
		t.Fatalf("dispatch output missing result receipt: %s", output.String())
	}
	requireReportSuccess(t, reportErrors)
}

func TestDispatchDoesNotFollowRunnerRedirects(t *testing.T) {
	redirected := make(chan http.Header, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		redirected <- request.Header.Clone()
		response.WriteHeader(http.StatusAccepted)
	}))
	defer redirectTarget.Close()

	runnerAttempts := 0
	runner := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		runnerAttempts++
		http.Redirect(response, request, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:              "coord-token",
		TargetToken:        "target-token",
		TranswarpToken:     "runner-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		PublicURL:          "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "target-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-redirect"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if !strings.Contains(string(data), "307 Temporary Redirect") {
		t.Fatalf("dispatch stream missing redirect failure: %s", string(data))
	}
	if runnerAttempts != 1 {
		t.Fatalf("expected redirect to fail without retry, got %d attempts", runnerAttempts)
	}
	select {
	case headers := <-redirected:
		t.Fatalf("runner redirect leaked headers: Authorization=%q CF-Access-Client-Secret=%q", headers.Get("Authorization"), headers.Get("CF-Access-Client-Secret"))
	default:
	}
}

func TestCancelDispatchForwardsToSelectedRunnerBuild(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	var cancelOnce sync.Once
	var receivedAuth string
	var receivedAccessID string
	var receivedAccessSecret string
	runner := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
			close(started)
		case "/v1/builds/build-123/logs":
			<-canceled
			response.Write([]byte(`{"kind":"build","message":"canceled","sequence":1}` + "\n"))
		case "/v1/builds/build-123/cancel":
			receivedAuth = request.Header.Get("Authorization")
			receivedAccessID = request.Header.Get("CF-Access-Client-Id")
			receivedAccessSecret = request.Header.Get("CF-Access-Client-Secret")
			cancelOnce.Do(func() { close(canceled) })
			response.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:              "coord-token",
		TranswarpToken:     "runner-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		PublicURL:          "https://coordinator.test",
		HTTPClient:         runner.Client(),
		ResultWaitTimeout:  20 * time.Millisecond,
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	dispatchDone := make(chan string, 1)
	go func() {
		body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
		request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
		if err != nil {
			dispatchDone <- err.Error()
			return
		}
		request.Header.Set("Authorization", "Bearer coord-token")
		response, err := testServer.Client().Do(request)
		if err != nil {
			dispatchDone <- err.Error()
			return
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		dispatchDone <- string(data)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner build did not start")
	}
	for attempt := 0; attempt < 100; attempt++ {
		server.mu.Lock()
		active := server.active["run-123"]
		server.mu.Unlock()
		if active.RunnerBuildID == "build-123" {
			break
		}
		time.Sleep(10 * time.Millisecond)
		if attempt == 99 {
			t.Fatalf("coordinator did not remember runner build: %+v", active)
		}
	}

	cancelRequest, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatches/run-123/cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest.Header.Set("Authorization", "Bearer coord-token")
	cancelResponse, err := testServer.Client().Do(cancelRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelResponse.Body.Close()
	if cancelResponse.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(cancelResponse.Body)
		t.Fatalf("unexpected cancel status: %s body: %s", cancelResponse.Status, string(data))
	}

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("runner cancel endpoint was not called")
	}
	if receivedAuth != "Bearer runner-token" {
		t.Fatalf("unexpected runner auth: %s", receivedAuth)
	}
	if receivedAccessID != "access-id" || receivedAccessSecret != "access-secret" {
		t.Fatalf("unexpected Access headers: id=%s secret=%s", receivedAccessID, receivedAccessSecret)
	}
	select {
	case output := <-dispatchDone:
		if !strings.Contains(output, "dispatch failed:") {
			t.Fatalf("dispatch output did not finish with cancellation failure: %s", output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch did not finish after cancel")
	}
}

func TestCancelDispatchRejectsActiveDispatchBeforeRunnerBuildStarts(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	activateDispatch(server, DispatchRequest{
		JobID:     "xcode-debug",
		RequestID: "run-123",
	})

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatches/run-123/cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unexpected cancel status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "has not started") {
		t.Fatalf("expected not-started error, got %s", string(data))
	}
}

func TestCancelDispatchRejectsUnsafeRequestID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatches/run%20123/cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected cancel status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "request_id") {
		t.Fatalf("expected request_id validation error, got %s", string(data))
	}
}

func TestDispatchRejectsPinnedUnsupportedTarget(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	seedTarget(server, Target{
		MachineID:   "machine-123",
		MachineName: "Old Mac",
		PublicURL:   "https://transwarp.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "amd64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected dispatch status: %s: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "target is unsupported") || !strings.Contains(string(data), "Apple Silicon") {
		t.Fatalf("expected unsupported target error, got %s", string(data))
	}
}

func TestDispatchSelectsRegisteredTargetByJob(t *testing.T) {
	var receivedPaths []string
	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	reportErrors := make(chan error, 1)
	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedPaths = append(receivedPaths, request.URL.Path)
		switch request.URL.Path {
		case "/v1/builds":
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			buildMu.Lock()
			currentBuild := buildRequest
			buildMu.Unlock()
			reportErrors <- postCallbackResult(runner.Client(), currentBuild, BuildResult{
				JobID:     "xcode-debug",
				RequestID: "run-123",
				MachineID: "machine-bbb",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  0,
			})
			response.Write([]byte(`{"kind":"log","message":"auto selected runner","sequence":1}` + "\n"))
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":2}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		HTTPClient:     runner.Client(),
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-aaa",
		MachineName:    "Mac Mini",
		PublicURL:      "https://wrong.example.com",
		Jobs:           []string{"lint"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-bbb",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var output bytes.Buffer
	if _, err := output.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if !strings.Contains(output.String(), "auto selected runner") {
		t.Fatalf("dispatch output missing runner log: %s", output.String())
	}
	if !strings.Contains(output.String(), `"build_id":"build-123"`) {
		t.Fatalf("dispatch output missing accepted runner build ID: %s", output.String())
	}
	if !strings.Contains(output.String(), `"request_id":"run-123"`) {
		t.Fatalf("dispatch output missing request ID metadata: %s", output.String())
	}
	if !strings.Contains(output.String(), `"machine_id":"machine-bbb"`) {
		t.Fatalf("dispatch output missing selected machine metadata: %s", output.String())
	}
	if !strings.Contains(output.String(), fmt.Sprintf(`"public_url":%q`, runner.URL)) {
		t.Fatalf("dispatch output missing selected runner URL metadata: %s", output.String())
	}
	if !strings.Contains(output.String(), "[result] recorded passed") {
		t.Fatalf("dispatch output missing result receipt: %s", output.String())
	}
	if len(receivedPaths) != 2 {
		t.Fatalf("unexpected runner requests: %+v", receivedPaths)
	}
	requireReportSuccess(t, reportErrors)
}

func TestDispatchRejectsResultFromUnselectedMachine(t *testing.T) {
	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	reportErrors := make(chan error, 1)
	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			buildMu.Lock()
			currentBuild := buildRequest
			buildMu.Unlock()
			reportErrors <- postCallbackResult(runner.Client(), currentBuild, BuildResult{
				JobID:     "xcode-debug",
				RequestID: "run-123",
				MachineID: "machine-wrong",
				Machine:   "Wrong Mac",
				Status:    "passed",
				ExitCode:  0,
			})
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:             "coord-token",
		TranswarpToken:    "runner-token",
		PublicURL:         "https://coordinator.test",
		HTTPClient:        runner.Client(),
		ResultWaitTimeout: 20 * time.Millisecond,
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-selected",
		MachineName:    "Selected Mac",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var output bytes.Buffer
	if _, err := output.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if !strings.Contains(output.String(), "result callback for request run-123 was not recorded") {
		t.Fatalf("dispatch output did not show missing accepted result: %s", output.String())
	}
	select {
	case err := <-reportErrors:
		if err == nil || !strings.Contains(err.Error(), "409 Conflict") {
			t.Fatalf("expected result callback conflict, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("result callback was not attempted")
	}
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 0 {
		t.Fatalf("wrong-machine result was recorded: %+v", results)
	}
}

func TestDispatchSkipsUnsupportedAutoSelectedTarget(t *testing.T) {
	var receivedPaths []string
	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	reportErrors := make(chan error, 1)
	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedPaths = append(receivedPaths, request.URL.Path)
		switch request.URL.Path {
		case "/v1/builds":
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			buildMu.Lock()
			currentBuild := buildRequest
			buildMu.Unlock()
			reportErrors <- postCallbackResult(runner.Client(), currentBuild, BuildResult{
				JobID:     "xcode-debug",
				RequestID: "run-123",
				MachineID: "machine-bbb",
				Machine:   "Apple Silicon Mac",
				Status:    "passed",
				ExitCode:  0,
			})
			response.Write([]byte(`{"kind":"log","message":"eligible runner accepted build","sequence":1}` + "\n"))
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":2}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		HTTPClient:     runner.Client(),
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	seedTarget(server, Target{
		MachineID:   "machine-aaa",
		MachineName: "Old Mac",
		PublicURL:   "https://wrong.example.com",
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "amd64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:   "machine-bbb",
		MachineName: "Apple Silicon Mac",
		PublicURL:   runner.URL,
		Capabilities: Capabilities{
			OS:           "macOS",
			OSVersion:    "15.6",
			Architecture: "arm64",
		},
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var output bytes.Buffer
	if _, err := output.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if !strings.Contains(output.String(), "eligible runner accepted build") {
		t.Fatalf("dispatch output missing eligible runner log: %s", output.String())
	}
	if len(receivedPaths) != 2 {
		t.Fatalf("unexpected runner requests: %+v", receivedPaths)
	}
	requireReportSuccess(t, reportErrors)
}

func TestDispatchFallsBackWhenAutoSelectedTargetRejectsStart(t *testing.T) {
	busyRunner := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/builds" {
			t.Fatalf("busy runner should only receive build start, got %s", request.URL.Path)
		}
		writeJSON(response, http.StatusConflict, map[string]string{"error": "another build is already running"})
	}))
	defer busyRunner.Close()

	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	reportErrors := make(chan error, 1)
	var availableRunner *httptest.Server
	availableRunner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-available","status":"running"}`))
		case "/v1/builds/build-available/logs":
			buildMu.Lock()
			currentBuild := buildRequest
			buildMu.Unlock()
			reportErrors <- postCallbackResult(availableRunner.Client(), currentBuild, BuildResult{
				BuildID:   "build-available",
				JobID:     "xcode-debug",
				RequestID: "run-123",
				MachineID: "machine-bbb",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  0,
			})
			response.Write([]byte(`{"kind":"log","message":"fallback runner accepted build","sequence":1}` + "\n"))
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":2}` + "\n"))
		default:
			t.Fatalf("unexpected available runner path: %s", request.URL.Path)
		}
	}))
	defer availableRunner.Close()

	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		HTTPClient:     availableRunner.Client(),
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-aaa",
		MachineName:    "Busy Mac",
		PublicURL:      busyRunner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-bbb",
		MachineName:    "Available Mac",
		PublicURL:      availableRunner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var output bytes.Buffer
	if _, err := output.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if !strings.Contains(output.String(), "target machine-aaa rejected build start") {
		t.Fatalf("dispatch output missing fallback note: %s", output.String())
	}
	if !strings.Contains(output.String(), "fallback runner accepted build") {
		t.Fatalf("dispatch output missing fallback runner log: %s", output.String())
	}
	if !strings.Contains(output.String(), "[result] recorded passed") {
		t.Fatalf("dispatch output missing result receipt: %s", output.String())
	}
	requireReportSuccess(t, reportErrors)
}

func TestDispatchRejectsMissingRequestID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{"job_id":"xcode-debug"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
}

func TestDispatchRejectsUnsafeJobID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{"job_id":"xcode/debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected dispatch status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "job_id") {
		t.Fatalf("expected job_id validation error, got %s", string(data))
	}
	if _, ok := server.active["run-123"]; ok {
		t.Fatal("unsafe job_id should not create an active dispatch")
	}
}

func TestDispatchRejectsUnsafeMachineID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{"machine_id":"machine/123","job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected dispatch status: %s body: %s", response.Status, string(data))
	}
	if !strings.Contains(string(data), "machine_id") {
		t.Fatalf("expected machine_id validation error, got %s", string(data))
	}
	if _, ok := server.active["run-123"]; ok {
		t.Fatal("unsafe machine_id should not create an active dispatch")
	}
}

func TestDispatchRejectsUnknownJSONFields(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123","reportToken":"typo"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if _, ok := server.active["run-123"]; ok {
		t.Fatal("unknown dispatch field should not create an active dispatch")
	}
}

func TestDispatchRejectsTrailingJSON(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	body := bytes.NewBufferString(`{"job_id":"xcode-debug","request_id":"run-123"} {"job_id":"xcode-debug","request_id":"run-456"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if _, ok := server.active["run-123"]; ok {
		t.Fatal("trailing dispatch JSON should not create an active dispatch")
	}
}

func TestDispatchReturnsRecordedResultForDuplicateRequestID(t *testing.T) {
	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()

	seedResult(server, BuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Machine:   "Mac Studio",
		Status:    "passed",
		ExitCode:  0,
		PublicURL: "https://runner.example.com",
	})

	body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var output bytes.Buffer
	if _, err := output.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if !strings.Contains(output.String(), `"message":"accepted runner build"`) {
		t.Fatalf("recorded result output missing accepted-build metadata: %s", output.String())
	}
	if !strings.Contains(output.String(), `"build_id":"build-123"`) {
		t.Fatalf("recorded result output missing build ID: %s", output.String())
	}
	if !strings.Contains(output.String(), `"machine_id":"machine-123"`) {
		t.Fatalf("recorded result output missing machine ID: %s", output.String())
	}
	if !strings.Contains(output.String(), `"public_url":"https://runner.example.com"`) {
		t.Fatalf("recorded result output missing public URL: %s", output.String())
	}
	if !strings.Contains(output.String(), "[result] recorded passed\n") {
		t.Fatalf("unexpected recorded result output: %s", output.String())
	}
}

func TestDispatchRejectsInFlightDuplicateRequestIDBeforeRunnerBuildIsAccepted(t *testing.T) {
	buildStartAttempted := make(chan struct{})
	releaseBuildStart := make(chan struct{})
	releaseLogs := make(chan struct{})
	var once sync.Once
	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			once.Do(func() {
				close(buildStartAttempted)
			})
			<-releaseBuildStart
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			<-releaseLogs
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:             "coord-token",
		TranswarpToken:    "runner-token",
		PublicURL:         "https://coordinator.test",
		HTTPClient:        runner.Client(),
		ResultWaitTimeout: 20 * time.Millisecond,
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
		request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
		if err != nil {
			t.Error(err)
			return
		}
		request.Header.Set("Authorization", "Bearer coord-token")
		response, err := testServer.Client().Do(request)
		if err != nil {
			t.Error(err)
			return
		}
		defer response.Body.Close()
		_, _ = io.ReadAll(response.Body)
	}()

	select {
	case <-buildStartAttempted:
	case <-time.After(2 * time.Second):
		t.Fatal("first dispatch did not reach runner")
	}

	body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected duplicate dispatch conflict, got %s", response.Status)
	}

	close(releaseBuildStart)
	close(releaseLogs)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first dispatch did not finish")
	}
}

func TestDispatchReconnectsInFlightDuplicateRequestIDAfterRunnerBuildIsAccepted(t *testing.T) {
	releaseLogs := make(chan struct{})
	reportErrors := make(chan error, 1)
	buildAccepted := make(chan struct{})
	var acceptedOnce sync.Once
	var reportOnce sync.Once
	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	var requestMu sync.Mutex
	buildStarts := 0
	logTails := 0

	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			requestMu.Lock()
			buildStarts++
			requestMu.Unlock()
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildRequest.BuildID = "build-123"
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
			acceptedOnce.Do(func() {
				close(buildAccepted)
			})
		case "/v1/builds/build-123":
			response.Write([]byte(`{"build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			requestMu.Lock()
			logTails++
			requestMu.Unlock()
			<-releaseLogs
			reportOnce.Do(func() {
				buildMu.Lock()
				currentBuild := buildRequest
				buildMu.Unlock()
				reportErrors <- postCallbackResult(runner.Client(), currentBuild, BuildResult{
					JobID:     "xcode-debug",
					RequestID: "run-123",
					MachineID: "machine-123",
					Machine:   "Mac Studio",
					Status:    "passed",
					ExitCode:  0,
					PublicURL: runner.URL,
				})
			})
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:             "coord-token",
		TranswarpToken:    "runner-token",
		PublicURL:         "https://coordinator.test",
		HTTPClient:        runner.Client(),
		ResultWaitTimeout: time.Second,
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
		request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
		if err != nil {
			t.Error(err)
			return
		}
		request.Header.Set("Authorization", "Bearer coord-token")
		response, err := testServer.Client().Do(request)
		if err != nil {
			t.Error(err)
			return
		}
		defer response.Body.Close()
		_, _ = io.ReadAll(response.Body)
	}()

	select {
	case <-buildAccepted:
	case <-time.After(2 * time.Second):
		t.Fatal("first dispatch did not accept a runner build")
	}
	waitForActiveRunnerBuild(t, server, "run-123", "build-123")

	reconnectDone := make(chan string, 1)
	go func() {
		body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
		request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
		if err != nil {
			t.Error(err)
			reconnectDone <- ""
			return
		}
		request.Header.Set("Authorization", "Bearer coord-token")
		response, err := testServer.Client().Do(request)
		if err != nil {
			t.Error(err)
			reconnectDone <- ""
			return
		}
		defer response.Body.Close()
		var output bytes.Buffer
		_, readErr := output.ReadFrom(response.Body)
		if readErr != nil {
			t.Error(readErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Errorf("unexpected reconnect status: %s body: %s", response.Status, output.String())
		}
		reconnectDone <- output.String()
	}()

	waitForIntAtLeast(t, &requestMu, &logTails, 2, "duplicate dispatch did not tail existing runner build")
	close(releaseLogs)

	var reconnectOutput string
	select {
	case reconnectOutput = <-reconnectDone:
	case <-time.After(2 * time.Second):
		t.Fatal("duplicate dispatch did not reconnect")
	}
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first dispatch did not finish")
	}

	if !strings.Contains(reconnectOutput, `"message":"accepted runner build"`) {
		t.Fatalf("reconnect output missing accepted runner metadata: %s", reconnectOutput)
	}
	if !strings.Contains(reconnectOutput, "[result] recorded passed") {
		t.Fatalf("reconnect output missing result receipt: %s", reconnectOutput)
	}
	requestMu.Lock()
	starts := buildStarts
	tails := logTails
	requestMu.Unlock()
	if starts != 1 {
		t.Fatalf("duplicate dispatch should not start another runner build, got %d starts", starts)
	}
	if tails < 2 {
		t.Fatalf("duplicate dispatch should tail existing runner build, got %d tails", tails)
	}
	requireReportSuccess(t, reportErrors)
}

func TestDispatchSendsCloudflareAccessHeaders(t *testing.T) {
	var receivedClientID string
	var receivedClientSecret string
	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	var seenMu sync.Mutex
	reportErrors := make(chan error, 1)
	seen := map[string]bool{}
	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedClientID = request.Header.Get("CF-Access-Client-Id")
		receivedClientSecret = request.Header.Get("CF-Access-Client-Secret")
		seenMu.Lock()
		seen[request.URL.Path] = true
		seenMu.Unlock()
		switch request.URL.Path {
		case "/v1/builds":
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			buildMu.Lock()
			currentBuild := buildRequest
			buildMu.Unlock()
			reportErrors <- postCallbackResult(runner.Client(), currentBuild, BuildResult{
				JobID:     "xcode-debug",
				RequestID: "run-123",
				MachineID: "machine-123",
				Machine:   "Mac Studio",
				Status:    "passed",
				ExitCode:  0,
			})
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
		case "/v1/builds/build-123/cancel":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"canceling"}`))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:              "coord-token",
		TranswarpToken:     "runner-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		PublicURL:          "https://coordinator.test",
		HTTPClient:         runner.Client(),
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	_, _ = io.ReadAll(response.Body)
	if receivedClientID != "access-id" {
		t.Fatalf("unexpected access client id: %s", receivedClientID)
	}
	if receivedClientSecret != "access-secret" {
		t.Fatalf("unexpected access client secret: %s", receivedClientSecret)
	}
	seenMu.Lock()
	sawBuilds := seen["/v1/builds"]
	sawLogs := seen["/v1/builds/build-123/logs"]
	seenSnapshot := fmt.Sprintf("%+v", seen)
	seenMu.Unlock()
	if !sawBuilds || !sawLogs {
		t.Fatalf("missing expected runner requests: %s", seenSnapshot)
	}
	requireReportSuccess(t, reportErrors)
}

func TestDispatchWaitsForResultCallback(t *testing.T) {
	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	releaseReport := make(chan struct{})
	reportErrors := make(chan error, 1)
	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			buildMu.Lock()
			currentBuild := buildRequest
			buildMu.Unlock()
			go func() {
				<-releaseReport
				reportErrors <- postCallbackResult(runner.Client(), currentBuild, BuildResult{
					JobID:     "xcode-debug",
					RequestID: "run-123",
					MachineID: "machine-123",
					Machine:   "Mac Studio",
					Status:    "passed",
					ExitCode:  0,
				})
			}()
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		HTTPClient:     runner.Client(),
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	type dispatchResponse struct {
		statusCode int
		output     string
		err        error
	}
	done := make(chan dispatchResponse, 1)
	go func() {
		body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
		request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
		if err != nil {
			done <- dispatchResponse{err: err}
			return
		}
		request.Header.Set("Authorization", "Bearer coord-token")
		response, err := testServer.Client().Do(request)
		if err != nil {
			done <- dispatchResponse{err: err}
			return
		}
		defer response.Body.Close()

		var output bytes.Buffer
		if _, err := output.ReadFrom(response.Body); err != nil {
			done <- dispatchResponse{err: err}
			return
		}
		done <- dispatchResponse{statusCode: response.StatusCode, output: output.String()}
	}()

	select {
	case result := <-done:
		t.Fatalf("dispatch returned before result callback: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseReport)
	var result dispatchResponse
	select {
	case result = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch did not return after result callback")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.statusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %d", result.statusCode)
	}
	if !strings.Contains(result.output, "[build] passed") {
		t.Fatalf("dispatch output missing build status: %s", result.output)
	}
	if !strings.Contains(result.output, "[result] recorded passed") {
		t.Fatalf("dispatch output missing result receipt: %s", result.output)
	}
	requireReportSuccess(t, reportErrors)
	results := getResults(t, testServer.URL, "coord-token")
	if len(results) != 1 || results[0].RequestID != "run-123" {
		t.Fatalf("unexpected results after dispatch: %+v", results)
	}
}

func TestDispatchWaitsForFailedResultCallback(t *testing.T) {
	var buildMu sync.Mutex
	var buildRequest receivedBuildRequest
	releaseReport := make(chan struct{})
	reportErrors := make(chan error, 1)
	var runner *httptest.Server
	runner = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			buildMu.Lock()
			err := json.NewDecoder(request.Body).Decode(&buildRequest)
			buildMu.Unlock()
			if err != nil {
				http.Error(response, "invalid build request", http.StatusBadRequest)
				return
			}
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			buildMu.Lock()
			currentBuild := buildRequest
			buildMu.Unlock()
			go func() {
				<-releaseReport
				reportErrors <- postCallbackResult(runner.Client(), currentBuild, BuildResult{
					JobID:     "xcode-debug",
					RequestID: "run-123",
					MachineID: "machine-123",
					Machine:   "Mac Studio",
					Status:    "failed",
					Error:     "xcodebuild exited 65",
					ExitCode:  65,
				})
			}()
			response.Write([]byte(`{"kind":"build","message":"failed with exit code 65","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:          "coord-token",
		TranswarpToken: "runner-token",
		PublicURL:      "https://coordinator.test",
		HTTPClient:     runner.Client(),
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	type dispatchResponse struct {
		statusCode int
		output     string
		err        error
	}
	done := make(chan dispatchResponse, 1)
	go func() {
		body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
		request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
		if err != nil {
			done <- dispatchResponse{err: err}
			return
		}
		request.Header.Set("Authorization", "Bearer coord-token")
		response, err := testServer.Client().Do(request)
		if err != nil {
			done <- dispatchResponse{err: err}
			return
		}
		defer response.Body.Close()

		var output bytes.Buffer
		if _, err := output.ReadFrom(response.Body); err != nil {
			done <- dispatchResponse{err: err}
			return
		}
		done <- dispatchResponse{statusCode: response.StatusCode, output: output.String()}
	}()

	select {
	case result := <-done:
		t.Fatalf("dispatch returned before failed result callback: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseReport)
	var result dispatchResponse
	select {
	case result = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch did not return after failed result callback")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.statusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %d", result.statusCode)
	}
	if !strings.Contains(result.output, "[build] failed with exit code 65") {
		t.Fatalf("dispatch output missing build failure: %s", result.output)
	}
	if !strings.Contains(result.output, "[result] recorded failed") {
		t.Fatalf("dispatch output missing failed result receipt: %s", result.output)
	}
	if !strings.Contains(result.output, "dispatch failed: xcodebuild exited 65") {
		t.Fatalf("dispatch output missing failed result error: %s", result.output)
	}
	requireReportSuccess(t, reportErrors)
}

func TestDispatchFailsWhenResultCallbackIsMissing(t *testing.T) {
	runner := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected runner path: %s", request.URL.Path)
		}
	}))
	defer runner.Close()

	server := newTestServer(t, Options{
		Token:             "coord-token",
		TranswarpToken:    "runner-token",
		PublicURL:         "https://coordinator.test",
		HTTPClient:        runner.Client(),
		ResultWaitTimeout: 20 * time.Millisecond,
	})
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	server.publicURL = testServer.URL

	registerTarget(t, testServer.URL, "coord-token", Target{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		PublicURL:      runner.URL,
		Jobs:           []string{"xcode-debug"},
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})

	body := bytes.NewBufferString(`{"machine_id":"machine-123","job_id":"xcode-debug","request_id":"run-123"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/transwarp/dispatch", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer coord-token")
	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var output bytes.Buffer
	if _, err := output.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dispatch status: %s", response.Status)
	}
	if !strings.Contains(output.String(), "dispatch failed: result callback for request run-123 was not recorded") {
		t.Fatalf("dispatch output did not report missing result callback: %s", output.String())
	}
}

type receivedBuildRequest struct {
	JobID       string `json:"job_id"`
	RequestID   string `json:"request_id"`
	ReportURL   string `json:"report_url"`
	ReportToken string `json:"report_token"`
	BuildID     string `json:"-"`
}

func postCallbackResult(client *http.Client, build receivedBuildRequest, result BuildResult) error {
	if build.ReportURL == "" {
		return fmt.Errorf("missing report URL for request %s", build.RequestID)
	}
	if result.JobID == "" {
		result.JobID = build.JobID
	}
	if result.RequestID == "" {
		result.RequestID = build.RequestID
	}
	if result.BuildID == "" {
		result.BuildID = build.BuildID
		if result.BuildID == "" {
			result.BuildID = "build-123"
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, build.ReportURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+build.ReportToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected result callback status: %s", response.Status)
	}
	return nil
}

func requireReportSuccess(t *testing.T, reportErrors <-chan error) {
	t.Helper()

	select {
	case err := <-reportErrors:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("result callback was not attempted")
	}
}

func waitForActiveRunnerBuild(t *testing.T, server *Server, requestID string, buildID string) {
	t.Helper()

	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		server.mu.Lock()
		active, ok := server.active[requestID]
		server.mu.Unlock()
		if ok && active.RunnerBuildID == buildID {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("active dispatch %s did not record runner build %s", requestID, buildID)
		case <-ticker.C:
		}
	}
}

func waitForIntAtLeast(t *testing.T, mu *sync.Mutex, value *int, minimum int, message string) {
	t.Helper()

	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		mu.Lock()
		current := *value
		mu.Unlock()
		if current >= minimum {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("%s: got %d, want at least %d", message, current, minimum)
		case <-ticker.C:
		}
	}
}

func registerTarget(t *testing.T, baseURL string, token string, target Target) {
	t.Helper()

	target = targetWithDefaultCapabilities(target)
	data, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/transwarp/register", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected register status: %s", response.Status)
	}
}

func recordResult(t *testing.T, baseURL string, token string, result BuildResult) *http.Response {
	t.Helper()

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/transwarp/result", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func writeStateFile(t *testing.T, path string, state stateFile) {
	t.Helper()

	data, err := json.MarshalIndent(state, "", "\t")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readStateFile(t *testing.T, path string) stateFile {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func getTargets(t *testing.T, baseURL string, token string) []Target {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, baseURL+"/transwarp/targets", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var targets []Target
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		t.Fatal(err)
	}
	return targets
}

func seedTarget(server *Server, target Target) {
	server.mu.Lock()
	defer server.mu.Unlock()

	target = targetWithDefaultCapabilities(target)
	server.targets[target.MachineID] = target
}

func targetWithDefaultCapabilities(target Target) Target {
	if target.Capabilities.Empty() {
		target.Capabilities = validTargetCapabilities()
	}
	return target
}

func validTargetCapabilities() Capabilities {
	return Capabilities{
		OS:           "macOS",
		OSVersion:    "15.6",
		Architecture: "arm64",
	}
}

func activateDispatch(server *Server, request DispatchRequest) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.active[request.RequestID] = activeDispatch{
		Request: request,
	}
}

func activateStartedDispatch(server *Server, request DispatchRequest, machineID string, buildID string) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.active[request.RequestID] = activeDispatch{
		Request:         request,
		TargetMachineID: machineID,
		RunnerBuildID:   buildID,
	}
}

func seedResult(server *Server, result BuildResult) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.results[result.RequestID] = result
}

func getResults(t *testing.T, baseURL string, token string) []BuildResult {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, baseURL+"/transwarp/results", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var results []BuildResult
	if err := json.NewDecoder(response.Body).Decode(&results); err != nil {
		t.Fatal(err)
	}
	return results
}

func newTestServer(t *testing.T, options Options) *Server {
	t.Helper()

	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

package diagnose

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunChecksHealthStatusAccessHeadersAndJob(t *testing.T) {
	var sawHealthAccess bool
	var sawStatusAccess bool
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			sawHealthAccess = request.Header.Get("CF-Access-Client-Id") == "access-id" &&
				request.Header.Get("CF-Access-Client-Secret") == "access-secret"
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			sawStatusAccess = request.Header.Get("CF-Access-Client-Id") == "access-id" &&
				request.Header.Get("CF-Access-Client-Secret") == "access-secret" &&
				request.Header.Get("Authorization") == "Bearer runner-token"
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel_mode": "named",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "https://transwarp.example.com"
				},
				"registration": {
					"configured": true,
					"state": "registered",
					"last_action": "register",
					"lease_expires_at": "` + leaseExpiresAt + `"
				},
				"capabilities": {
					"os": "macOS",
					"os_version": "15.6",
					"architecture": "arm64",
					"cpu_brand": "Apple M3 Max",
					"cpu_count": 16,
					"xcode_version": "Xcode 16.4 (Build version 16F6)"
				},
				"public_url": "https://transwarp.example.com",
				"active_builds": 0,
				"queued_builds": 1,
				"queued_build_limit": 25,
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	output := bytes.Buffer{}
	err := Run(context.Background(), server.Client(), Request{
		BaseURL:            server.URL,
		Token:              "runner-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		JobID:              "xcode-debug",
		AllowHTTP:          true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &output)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !sawHealthAccess {
		t.Fatal("health request did not include Cloudflare Access headers")
	}
	if !sawStatusAccess {
		t.Fatal("status request did not include expected auth headers")
	}
	if !strings.Contains(output.String(), "diagnosis passed") {
		t.Fatalf("output missing success: %s", output.String())
	}
	if !strings.Contains(output.String(), "[ok] runner public_url=https://transwarp.example.com") {
		t.Fatalf("output missing public URL: %s", output.String())
	}
	if !strings.Contains(output.String(), "[ok] registration state=registered action=register") {
		t.Fatalf("output missing registration status: %s", output.String())
	}
	if !strings.Contains(output.String(), "[ok] registration lease_expires_at=") {
		t.Fatalf("output missing registration lease: %s", output.String())
	}
	if !strings.Contains(output.String(), "[ok] runner capabilities os=macOS arch=arm64") {
		t.Fatalf("output missing capabilities: %s", output.String())
	}
	if !strings.Contains(output.String(), "[ok] runner load active=0 queued=1/25") {
		t.Fatalf("output missing runner load: %s", output.String())
	}
}

func TestRunDoesNotFollowRunnerStatusRedirects(t *testing.T) {
	redirected := make(chan http.Header, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		redirected <- request.Header.Clone()
		response.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	statusAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			response.WriteHeader(http.StatusOK)
		case "/status":
			statusAttempts++
			http.Redirect(response, request, redirectTarget.URL, http.StatusTemporaryRedirect)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), nil, Request{
		BaseURL:            server.URL,
		Token:              "runner-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		AllowHTTP:          true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "307 Temporary Redirect") {
		t.Fatalf("expected redirect status error, got %v", err)
	}
	if statusAttempts != 1 {
		t.Fatalf("expected redirect to fail without retry, got %d attempts", statusAttempts)
	}
	select {
	case headers := <-redirected:
		t.Fatalf("diagnostic redirect leaked headers: Authorization=%q CF-Access-Client-Secret=%q", headers.Get("Authorization"), headers.Get("CF-Access-Client-Secret"))
	default:
	}
}

func TestRunRejectsUnexpectedRunnerMachineID(t *testing.T) {
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "other-machine",
				"machine_name": "Unexpected Mac",
				"tunnel_mode": "named",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "https://transwarp.example.com"
				},
				"registration": {
					"configured": true,
					"state": "registered",
					"lease_expires_at": "` + leaseExpiresAt + `"
				},
				"capabilities": {
					"os": "macOS",
					"os_version": "15.6",
					"architecture": "arm64"
				},
				"public_url": "https://transwarp.example.com",
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	var output bytes.Buffer
	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		MachineID: "expected-machine",
		JobID:     "xcode-debug",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &output)

	if err == nil || !strings.Contains(err.Error(), `runner machine_id "other-machine" does not match requested machine_id "expected-machine"`) {
		t.Fatalf("expected machine_id mismatch error, got %v", err)
	}
	if !strings.Contains(output.String(), "[ok] authenticated status reachable for Unexpected Mac (other-machine)") {
		t.Fatalf("output missing reached runner identity: %s", output.String())
	}
}

func TestRequestRejectsHeaderControlCharacters(t *testing.T) {
	tests := []struct {
		name    string
		request Request
	}{
		{
			name: "runner token",
			request: Request{
				BaseURL: "https://transwarp.example.com",
				Token:   "runner-token\nInjected: yes",
				LookupHost: func(context.Context, string) ([]string, error) {
					return []string{"127.0.0.1"}, nil
				},
			},
		},
		{
			name: "access id",
			request: Request{
				BaseURL:            "https://transwarp.example.com",
				Token:              "runner-token",
				AccessClientID:     "access-id\rInjected: yes",
				AccessClientSecret: "access-secret",
			},
		},
		{
			name: "access secret",
			request: Request{
				BaseURL:            "https://transwarp.example.com",
				Token:              "runner-token",
				AccessClientID:     "access-id",
				AccessClientSecret: "access-secret\nInjected: yes",
			},
		},
		{
			name: "coordinator token",
			request: Request{
				CoordinatorURL:   "https://coordinator.example.com",
				CoordinatorToken: "coord-token\nInjected: yes",
			},
		},
		{
			name: "optional runner probe token through coordinator",
			request: Request{
				CoordinatorURL:   "https://coordinator.example.com",
				CoordinatorToken: "coord-token",
				Token:            "runner-token\nInjected: yes",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()
			if err == nil || !strings.Contains(err.Error(), "single HTTP header value") {
				t.Fatalf("expected header value error, got %v", err)
			}
		})
	}
}

func TestRunRejectsFullRunnerQueue(t *testing.T) {
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel_mode": "named",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "https://transwarp.example.com"
				},
				"registration": {
					"configured": true,
					"state": "registered",
					"lease_expires_at": "` + leaseExpiresAt + `"
				},
				"public_url": "https://transwarp.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"active_builds": 1,
				"queued_builds": 25,
				"queued_build_limit": 25,
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		JobID:     "xcode-debug",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "runner is unavailable: queue is full (25/25 queued)") {
		t.Fatalf("expected full queue error, got %v", err)
	}
}

func TestRunRejectsPausedRunner(t *testing.T) {
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel_mode": "named",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "https://transwarp.example.com"
				},
				"registration": {
					"configured": true,
					"state": "registered",
					"lease_expires_at": "` + leaseExpiresAt + `"
				},
				"public_url": "https://transwarp.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"accepting_builds": false,
				"active_builds": 0,
				"queued_builds": 0,
				"queued_build_limit": 25,
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		JobID:     "xcode-debug",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "runner is unavailable: runner is paused") {
		t.Fatalf("expected paused runner error, got %v", err)
	}
}

func TestRunRejectsRunnerThatIsNotAcceptingCIDispatches(t *testing.T) {
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel_mode": "named",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "https://transwarp.example.com"
				},
				"registration": {
					"configured": true,
					"state": "registered",
					"lease_expires_at": "` + leaseExpiresAt + `"
				},
				"public_url": "https://transwarp.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"accepting_builds": true,
				"ci_accepting_builds": false,
				"active_builds": 0,
				"queued_builds": 0,
				"queued_build_limit": 25,
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		JobID:     "xcode-debug",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "runner is unavailable: runner is not accepting CI dispatches") {
		t.Fatalf("expected effective CI availability error, got %v", err)
	}
}

func TestRunRejectsRegisteredRunnerWithoutLease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "http://127.0.0.1"
				},
				"registration": {
					"configured": true,
					"state": "registered",
					"last_action": "register"
				},
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "registered without lease_expires_at") {
		t.Fatalf("expected missing lease error, got %v", err)
	}
}

func TestRunRejectsRegisteredRunnerWithExpiredLease(t *testing.T) {
	leaseExpiresAt := jsonTime(time.Now().Add(-time.Minute))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "http://127.0.0.1"
				},
				"registration": {
					"configured": true,
					"state": "registered",
					"last_action": "heartbeat",
					"lease_expires_at": "` + leaseExpiresAt + `"
				},
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "registration lease expired") {
		t.Fatalf("expected expired lease error, got %v", err)
	}
}

func TestRunRejectsUnsupportedRunnerArchitecture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {"mode": "off", "state": "disabled"},
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "amd64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "Apple Silicon") {
		t.Fatalf("expected unsupported architecture error, got %v", err)
	}
}

func TestReportAndValidateCapabilitiesRequiresModernMacOSVersion(t *testing.T) {
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
			err := reportAndValidateCapabilities("runner", test.capabilities, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestRunRejectsRunnerThatMissesCapabilityConstraints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {"mode": "off", "state": "disabled"},
				"capabilities": {
					"os": "macOS",
					"os_version": "15.6",
					"architecture": "arm64",
					"cpu_count": 8,
					"memory_bytes": 17179869184,
					"xcode_version": "Xcode 16.2 (Build version 16C100)"
				},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:         server.URL,
		Token:           "runner-token",
		JobID:           "xcode-debug",
		MinCPUCount:     12,
		MinMemoryBytes:  34359738368,
		MinXcodeVersion: "16.4",
		AllowHTTP:       true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "runner does not satisfy constraints") {
		t.Fatalf("expected capability constraint error, got %v", err)
	}
	if !strings.Contains(err.Error(), "cpu_count") || !strings.Contains(err.Error(), "memory_bytes") || !strings.Contains(err.Error(), "xcode_version") {
		t.Fatalf("constraint error did not include all failures: %v", err)
	}
}

func TestRunRejectsRegistrationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {"mode": "off", "state": "disabled", "connected": false, "ready": true},
				"registration": {
					"configured": true,
					"state": "heartbeat_failed",
					"last_action": "heartbeat",
					"last_error": "registration endpoint returned 503 Service Unavailable"
				},
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "registration heartbeat_failed") {
		t.Fatalf("expected registration failure error, got %v", err)
	}
}

func TestRunRejectsMissingAdvertisedJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "http://127.0.0.1"
				},
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["test"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		JobID:     "xcode-debug",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not advertised") {
		t.Fatalf("expected missing job error, got %v", err)
	}
}

func TestRunRejectsConnectedTunnelWithoutPublicURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {"mode": "named", "state": "running", "connected": true, "ready": true},
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "public_url is empty") {
		t.Fatalf("expected missing public_url error, got %v", err)
	}
}

func TestRunRejectsConnectedTunnelThatIsNotReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {
					"mode": "quick",
					"state": "running",
					"connected": true,
					"ready": false,
					"public_url": "https://example-name.trycloudflare.com",
					"readiness_error": "public_url host does not resolve"
				},
				"public_url": "https://example-name.trycloudflare.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "public endpoint is not ready") {
		t.Fatalf("expected tunnel readiness error, got %v", err)
	}
	if !strings.Contains(err.Error(), `public_url="https://example-name.trycloudflare.com"`) {
		t.Fatalf("expected readiness error to include public URL, got %v", err)
	}
	if !strings.Contains(err.Error(), `readiness_error="public_url host does not resolve"`) {
		t.Fatalf("expected readiness error detail, got %v", err)
	}
}

func TestRunRejectsMismatchedPublicURLHost(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "https://other.example.com"
				},
				"public_url": "https://other.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL: server.URL,
		Token:   "runner-token",
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"203.0.113.10"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected public_url mismatch error, got %v", err)
	}
}

func TestRunRejectsReportedPublicURLWithPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/status":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {
					"mode": "named",
					"state": "running",
					"connected": true,
					"ready": true,
					"public_url": "` + serverURL(request) + `/status"
				},
				"public_url": "` + serverURL(request) + `/status",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "runner-token",
		AllowHTTP: true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "runner public_url must be a base URL") {
		t.Fatalf("expected public_url base URL error, got %v", err)
	}
}

func TestRunCoordinatorChecksTargetsAuthAccessAndJob(t *testing.T) {
	var sawHealthAccess bool
	var sawTargetsAuth bool
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			sawHealthAccess = request.Header.Get("CF-Access-Client-Id") == "access-id" &&
				request.Header.Get("CF-Access-Client-Secret") == "access-secret"
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			sawTargetsAuth = request.Header.Get("Authorization") == "Bearer coord-token" &&
				request.Header.Get("CF-Access-Client-Id") == "access-id" &&
				request.Header.Get("CF-Access-Client-Secret") == "access-secret"
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"listen": "127.0.0.1:8188",
				"public_url": "https://transwarp.example.com",
				"active_builds": 1,
				"queued_builds": 2,
				"queued_build_limit": 25,
				"lease_expires_at": "` + leaseExpiresAt + `",
				"capabilities": {
					"os": "macOS",
					"os_version": "15.6",
					"architecture": "arm64",
					"xcode_version": "Xcode 16.4 (Build version 16F6)"
				},
				"jobs": ["xcode-debug"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	output := bytes.Buffer{}
	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:     server.URL,
		CoordinatorToken:   "coord-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		MachineID:          "machine-123",
		JobID:              "xcode-debug",
		AllowHTTP:          true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &output)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !sawHealthAccess {
		t.Fatal("coordinator health request did not include Cloudflare Access headers")
	}
	if !sawTargetsAuth {
		t.Fatal("coordinator targets request did not include expected auth headers")
	}
	if !strings.Contains(output.String(), "[ok] target Mac Studio (machine-123) is available") {
		t.Fatalf("output missing target availability: %s", output.String())
	}
	if !strings.Contains(output.String(), "[ok] target load active=1 queued=2/25") {
		t.Fatalf("output missing target load: %s", output.String())
	}
	if !strings.Contains(output.String(), "[ok] target lease_expires_at=") {
		t.Fatalf("output missing target lease: %s", output.String())
	}
	if !strings.Contains(output.String(), "[ok] target capabilities os=macOS arch=arm64") {
		t.Fatalf("output missing target capabilities: %s", output.String())
	}
	if !strings.Contains(output.String(), "diagnosis passed") {
		t.Fatalf("output missing success: %s", output.String())
	}
}

func TestRunCoordinatorDoesNotFollowTargetRedirects(t *testing.T) {
	redirected := make(chan http.Header, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		redirected <- request.Header.Clone()
		response.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	targetAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			response.WriteHeader(http.StatusOK)
		case "/transwarp/targets":
			targetAttempts++
			http.Redirect(response, request, redirectTarget.URL, http.StatusTemporaryRedirect)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), nil, Request{
		CoordinatorURL:     server.URL,
		CoordinatorToken:   "coord-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		AllowHTTP:          true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "307 Temporary Redirect") {
		t.Fatalf("expected redirect coordinator targets error, got %v", err)
	}
	if targetAttempts != 1 {
		t.Fatalf("expected redirect to fail without retry, got %d attempts", targetAttempts)
	}
	select {
	case headers := <-redirected:
		t.Fatalf("coordinator diagnostic redirect leaked headers: Authorization=%q CF-Access-Client-Secret=%q", headers.Get("Authorization"), headers.Get("CF-Access-Client-Secret"))
	default:
	}
}

func TestRunCoordinatorRejectsFullTargetQueue(t *testing.T) {
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"listen": "127.0.0.1:8188",
				"public_url": "https://transwarp.example.com",
				"active_builds": 1,
				"queued_builds": 25,
				"queued_build_limit": 25,
				"lease_expires_at": "` + leaseExpiresAt + `",
				"jobs": ["xcode-debug"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		MachineID:        "machine-123",
		JobID:            "xcode-debug",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "target \"machine-123\" is unavailable: queue is full (25/25 queued)") {
		t.Fatalf("expected full target queue error, got %v", err)
	}
}

func TestRunCoordinatorRejectsUnsupportedTargetArchitecture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "https://transwarp.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "amd64"},
				"jobs": ["xcode-debug"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		JobID:            "xcode-debug",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "Apple Silicon") {
		t.Fatalf("expected unsupported target architecture error, got %v", err)
	}
}

func TestRunCoordinatorRejectsTargetsThatMissCapabilityConstraints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "https://transwarp.example.com",
				"capabilities": {
					"os": "macOS",
					"os_version": "15.6",
					"architecture": "arm64",
					"cpu_count": 8,
					"memory_bytes": 17179869184,
					"xcode_version": "Xcode 16.2 (Build version 16C100)"
				},
				"jobs": ["xcode-debug"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		JobID:            "xcode-debug",
		MinCPUCount:      12,
		MinMemoryBytes:   34359738368,
		MinXcodeVersion:  "16.4",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "satisfies constraints") {
		t.Fatalf("expected coordinator constraint error, got %v", err)
	}
}

func TestRunCoordinatorOptionallyProbesSelectedRunner(t *testing.T) {
	var sawRunnerStatusAuth bool
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"listen": "127.0.0.1:8188",
				"public_url": "` + serverURL(request) + `",
				"lease_expires_at": "` + leaseExpiresAt + `",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}]`))
		case "/status":
			sawRunnerStatusAuth = request.Header.Get("Authorization") == "Bearer runner-token"
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"tunnel": {"mode": "off", "state": "disabled"},
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	output := bytes.Buffer{}
	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		Token:            "runner-token",
		JobID:            "xcode-debug",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &output)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !sawRunnerStatusAuth {
		t.Fatal("selected runner status request did not include runner token")
	}
	if !strings.Contains(output.String(), "[ok] selected runner health reachable through public_url") {
		t.Fatalf("output missing selected runner probe: %s", output.String())
	}
}

func TestRunCoordinatorReportsSelectedRunnerStatusFailure(t *testing.T) {
	leaseExpiresAt := jsonTime(time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "` + serverURL(request) + `",
				"lease_expires_at": "` + leaseExpiresAt + `",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}]`))
		case "/status":
			response.WriteHeader(http.StatusUnauthorized)
			_, _ = response.Write([]byte(`{"error":"unauthorized"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		Token:            "runner-token",
		JobID:            "xcode-debug",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "selected runner status check failed") {
		t.Fatalf("expected selected runner status error, got %v", err)
	}
}

func TestRunCoordinatorRejectsTargetWithoutLease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "` + serverURL(request) + `",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		JobID:            "xcode-debug",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "registered without lease_expires_at") {
		t.Fatalf("expected missing lease error, got %v", err)
	}
}

func TestRunCoordinatorRejectsTargetWithExpiredLease(t *testing.T) {
	leaseExpiresAt := jsonTime(time.Now().Add(-time.Minute))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "` + serverURL(request) + `",
				"lease_expires_at": "` + leaseExpiresAt + `",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		JobID:            "xcode-debug",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "registration lease expired") {
		t.Fatalf("expected expired lease error, got %v", err)
	}
}

func TestRunCoordinatorRejectsMissingAdvertisedJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "https://transwarp.example.com",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["test"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		JobID:            "xcode-debug",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `no active target advertises job "xcode-debug"`) {
		t.Fatalf("expected missing job error, got %v", err)
	}
}

func TestRunCoordinatorRejectsTargetWithoutPublicURLForRemoteDiagnostics(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"listen": "127.0.0.1:8188",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		JobID:            "xcode-debug",
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"203.0.113.10"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "registered without public_url") {
		t.Fatalf("expected missing public URL error, got %v", err)
	}
}

func TestRunCoordinatorRejectsTargetPublicURLWithPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"status":"ok"}`))
		case "/transwarp/targets":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"machine_id": "machine-123",
				"machine_name": "Mac Studio",
				"public_url": "https://transwarp.example.com/status",
				"capabilities": {"os": "macOS", "os_version": "15.6", "architecture": "arm64"},
				"jobs": ["xcode-debug"]
			}]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		CoordinatorURL:   server.URL,
		CoordinatorToken: "coord-token",
		JobID:            "xcode-debug",
		AllowHTTP:        true,
		LookupHost: func(context.Context, string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "target public_url must be a base URL") {
		t.Fatalf("expected target public_url base URL error, got %v", err)
	}
}

func TestValidateCoordinatorRequiresHTTPSByDefault(t *testing.T) {
	err := Request{CoordinatorURL: "http://127.0.0.1:8288", CoordinatorToken: "token"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https validation error, got %v", err)
	}

	err = Request{CoordinatorURL: "http://127.0.0.1:8288", CoordinatorToken: "token", AllowHTTP: true}.Validate()
	if err != nil {
		t.Fatalf("expected allow-http to pass, got %v", err)
	}
}

func TestValidateRequiresHTTPSByDefault(t *testing.T) {
	err := Request{BaseURL: "http://127.0.0.1:8188", Token: "token"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https validation error, got %v", err)
	}

	err = Request{BaseURL: "http://127.0.0.1:8188", Token: "token", AllowHTTP: true}.Validate()
	if err != nil {
		t.Fatalf("expected allow-http to pass, got %v", err)
	}
}

func TestValidateRequiresBaseURLs(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		want    string
	}{
		{
			name: "runner path",
			request: Request{
				BaseURL: "https://transwarp.example.com/status",
				Token:   "token",
			},
			want: "url must be a base URL",
		},
		{
			name: "runner query",
			request: Request{
				BaseURL: "https://transwarp.example.com?token=secret",
				Token:   "token",
			},
			want: "url must be a base URL",
		},
		{
			name: "runner fragment",
			request: Request{
				BaseURL: "https://transwarp.example.com#runner",
				Token:   "token",
			},
			want: "url must be a base URL",
		},
		{
			name: "runner credentials",
			request: Request{
				BaseURL: "https://user:pass@transwarp.example.com",
				Token:   "token",
			},
			want: "url must not include credentials",
		},
		{
			name: "runner remote http with allow",
			request: Request{
				BaseURL:   "http://transwarp.example.com",
				Token:     "token",
				AllowHTTP: true,
			},
			want: "url must use https unless it targets local loopback",
		},
		{
			name: "coordinator path",
			request: Request{
				CoordinatorURL:   "https://coordinator.example.com/transwarp",
				CoordinatorToken: "token",
			},
			want: "coordinator URL must be a base URL",
		},
		{
			name: "coordinator credentials",
			request: Request{
				CoordinatorURL:   "https://user:pass@coordinator.example.com",
				CoordinatorToken: "token",
			},
			want: "coordinator URL must not include credentials",
		},
		{
			name: "coordinator remote http with allow",
			request: Request{
				CoordinatorURL:   "http://coordinator.example.com",
				CoordinatorToken: "token",
				AllowHTTP:        true,
			},
			want: "coordinator URL must use https unless it targets local loopback",
		},
		{
			name: "negative min cpu count",
			request: Request{
				BaseURL:     "https://runner.example.com",
				Token:       "token",
				MinCPUCount: -1,
			},
			want: "min CPU count must not be negative",
		},
		{
			name: "unsafe machine id",
			request: Request{
				CoordinatorURL:   "https://coordinator.example.com",
				CoordinatorToken: "token",
				MachineID:        "machine/123",
			},
			want: "machine_id",
		},
		{
			name: "unsafe job id",
			request: Request{
				CoordinatorURL:   "https://coordinator.example.com",
				CoordinatorToken: "token",
				JobID:            "xcode/debug",
			},
			want: "job_id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}

func jsonTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

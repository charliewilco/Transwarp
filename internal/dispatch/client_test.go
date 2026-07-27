package dispatch

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRunStartsBuildTailsOutputAndPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("unexpected authorization: %s", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			response.Header().Set("Content-Type", "application/x-ndjson")
			response.Write([]byte(`{"kind":"build","message":"starting Echo","sequence":1}` + "\n"))
			response.Write([]byte(`{"kind":"log","message":"hello from mac","sequence":2}` + "\n"))
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":3}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	var output bytes.Buffer
	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	}, &output)

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(output.String(), "hello from mac") {
		t.Fatalf("output missing log line: %s", output.String())
	}
}

func TestRunSendsCloudflareAccessHeadersOnStartAndTail(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("CF-Access-Client-Id") != "access-id" {
			t.Fatalf("unexpected access client id: %s", request.Header.Get("CF-Access-Client-Id"))
		}
		if request.Header.Get("CF-Access-Client-Secret") != "access-secret" {
			t.Fatalf("unexpected access client secret: %s", request.Header.Get("CF-Access-Client-Secret"))
		}
		seen[request.URL.Path] = true
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:            server.URL,
		Token:              "token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		JobID:              "echo",
		RequestID:          "run-123",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !seen["/v1/builds"] || !seen["/v1/builds/build-123/logs"] {
		t.Fatalf("missing expected requests: %+v", seen)
	}
}

func TestStartDoesNotFollowRedirects(t *testing.T) {
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

	_, err := Start(context.Background(), nil, Request{
		BaseURL:            server.URL,
		Token:              "token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		JobID:              "echo",
		RequestID:          "run-redirect",
	})
	if err == nil || !strings.Contains(err.Error(), "307 Temporary Redirect") {
		t.Fatalf("expected redirect dispatch error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected redirect to fail without retry, got %d attempts", attempts)
	}
	select {
	case headers := <-redirected:
		t.Fatalf("dispatch redirect leaked headers: Authorization=%q CF-Access-Client-Secret=%q", headers.Get("Authorization"), headers.Get("CF-Access-Client-Secret"))
	default:
	}
}

func TestStartRejectsUnsafeBuildIDResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusAccepted)
		response.Write([]byte(`{"build_id":"build/123","status":"running"}`))
	}))
	defer server.Close()

	_, err := Start(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	})
	if err == nil || !strings.Contains(err.Error(), "build_id") {
		t.Fatalf("expected unsafe build_id to fail, got %v", err)
	}
}

func TestStartRetriesTransientPublicTunnelNetworkError(t *testing.T) {
	withDispatchRetryDelays(t, []time.Duration{0})
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if request.URL.Path != "/v1/builds" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if attempts == 1 {
			return nil, transientDNSError(request)
		}
		return jsonResponse(request, http.StatusAccepted, `{"build_id":"build-123","status":"running"}`), nil
	})}

	started, err := Start(context.Background(), client, Request{
		BaseURL:   "https://example.trycloudflare.com",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	})

	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if started.BuildID != "build-123" {
		t.Fatalf("unexpected build ID: %+v", started)
	}
	if attempts != 2 {
		t.Fatalf("expected retry after transient tunnel DNS error, got %d attempts", attempts)
	}
}

func TestStartRetriesRetryablePublicTunnelResponse(t *testing.T) {
	withDispatchRetryDelays(t, []time.Duration{0})
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if request.URL.Path != "/v1/builds" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if attempts == 1 {
			return jsonResponse(request, http.StatusServiceUnavailable, "cloudflare tunnel unavailable"), nil
		}
		return jsonResponse(request, http.StatusAccepted, `{"build_id":"build-123","status":"running"}`), nil
	})}

	started, err := Start(context.Background(), client, Request{
		BaseURL:   "https://example.trycloudflare.com",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	})

	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if started.BuildID != "build-123" {
		t.Fatalf("unexpected build ID: %+v", started)
	}
	if attempts != 2 {
		t.Fatalf("expected retry after retryable tunnel response, got %d attempts", attempts)
	}
}

func TestStartDoesNotRetryLoopbackNetworkError(t *testing.T) {
	withDispatchRetryDelays(t, []time.Duration{0})
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		return nil, transientDNSError(request)
	})}

	_, err := Start(context.Background(), client, Request{
		BaseURL:   "http://127.0.0.1:18190",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	})

	if err == nil {
		t.Fatal("expected loopback network error")
	}
	if attempts != 1 {
		t.Fatalf("expected no loopback retry, got %d attempts", attempts)
	}
}

func TestStartDoesNotRetryLoopbackRetryableResponse(t *testing.T) {
	withDispatchRetryDelays(t, []time.Duration{0})
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		return jsonResponse(request, http.StatusServiceUnavailable, "local unavailable"), nil
	})}

	_, err := Start(context.Background(), client, Request{
		BaseURL:   "http://127.0.0.1:18190",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	})

	if err == nil || !strings.Contains(err.Error(), "local unavailable") {
		t.Fatalf("expected loopback start error body, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected no loopback response retry, got %d attempts", attempts)
	}
}

func TestGetCoordinatorResultFetchesRecordedResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/transwarp/results/run-123" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer coord-token" {
			t.Fatalf("unexpected authorization: %s", request.Header.Get("Authorization"))
		}
		if request.Header.Get("CF-Access-Client-Id") != "access-id" {
			t.Fatalf("unexpected access client id: %s", request.Header.Get("CF-Access-Client-Id"))
		}
		if request.Header.Get("CF-Access-Client-Secret") != "access-secret" {
			t.Fatalf("unexpected access client secret: %s", request.Header.Get("CF-Access-Client-Secret"))
		}
		response.Write([]byte(`{"build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","machine":"Mac Studio","status":"passed","exit_code":0,"public_url":"https://runner.example.com"}`))
	}))
	defer server.Close()

	result, err := GetCoordinatorResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:            server.URL,
		Token:              "coord-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		JobID:              "xcode-debug",
		RequestID:          "run-123",
	})

	if err != nil {
		t.Fatalf("GetCoordinatorResult returned error: %v", err)
	}
	runResult := result.RunResult()
	if runResult.BuildID != "build-123" || runResult.JobID != "xcode-debug" || runResult.MachineID != "machine-123" || runResult.Status != "passed" {
		t.Fatalf("unexpected run result: %+v", runResult)
	}
	if err := result.StatusError(); err != nil {
		t.Fatalf("passed result returned status error: %v", err)
	}
}

func TestGetCoordinatorResultReturnsFailedStatusError(t *testing.T) {
	result := CoordinatorBuildResult{
		BuildID:   "build-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		MachineID: "machine-123",
		Status:    "failed",
		ExitCode:  65,
		Error:     "xcodebuild exited 65",
	}

	if err := result.ValidateForRequest(CoordinatorRequest{RequestID: "run-123"}); err != nil {
		t.Fatalf("ValidateForRequest returned error: %v", err)
	}
	err := result.StatusError()
	if err == nil || !strings.Contains(err.Error(), "xcodebuild exited 65") {
		t.Fatalf("expected failed result status error, got %v", err)
	}
}

func TestGetCoordinatorResultRejectsMismatchedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"build_id":"build-123","job_id":"xcode-debug","request_id":"other-run","machine_id":"machine-123","machine":"Mac Studio","status":"passed","exit_code":0}`))
	}))
	defer server.Close()

	_, err := GetCoordinatorResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		RequestID: "run-123",
	})

	if err == nil || !strings.Contains(err.Error(), "does not match requested request_id") {
		t.Fatalf("expected mismatched request_id error, got %v", err)
	}
}

func TestGetCoordinatorResultReturnsHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, `{"error":"unknown request_id"}`, http.StatusNotFound)
	}))
	defer server.Close()

	_, err := GetCoordinatorResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		RequestID: "run-123",
	})

	if err == nil || !strings.Contains(err.Error(), "404 Not Found") || !strings.Contains(err.Error(), "unknown request_id") {
		t.Fatalf("expected result lookup HTTP error body, got %v", err)
	}
}

func TestStartReturnsFinalRetryablePublicTunnelResponse(t *testing.T) {
	withDispatchRetryDelays(t, []time.Duration{0})
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		return jsonResponse(request, http.StatusServiceUnavailable, "still unavailable"), nil
	})}

	_, err := Start(context.Background(), client, Request{
		BaseURL:   "https://example.trycloudflare.com",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	})

	if err == nil || !strings.Contains(err.Error(), "still unavailable") {
		t.Fatalf("expected final retryable response body, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected retry before returning final response, got %d attempts", attempts)
	}
}

func TestRunCoordinatorRetriesTransientPublicTunnelNetworkError(t *testing.T) {
	withDispatchRetryDelays(t, []time.Duration{0})
	attempts := 0
	stream := `{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","public_url":"https://runner.example.com"}` + "\n" +
		"[result] recorded passed\n"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if request.URL.Path != "/transwarp/dispatch" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if attempts == 1 {
			return nil, transientDNSError(request)
		}
		return jsonResponse(request, http.StatusOK, stream), nil
	})}

	result, err := RunCoordinatorWithResult(context.Background(), client, CoordinatorRequest{
		BaseURL:   "https://coordinator.example.com",
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("RunCoordinatorWithResult returned error: %v", err)
	}
	if result.BuildID != "build-123" || result.MachineID != "machine-123" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if attempts != 2 {
		t.Fatalf("expected retry after transient tunnel DNS error, got %d attempts", attempts)
	}
}

func TestRunCoordinatorRetriesRetryablePublicTunnelResponse(t *testing.T) {
	withDispatchRetryDelays(t, []time.Duration{0})
	attempts := 0
	stream := `{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","public_url":"https://runner.example.com"}` + "\n" +
		"[result] recorded passed\n"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if request.URL.Path != "/transwarp/dispatch" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if attempts == 1 {
			return jsonResponse(request, http.StatusBadGateway, "cloudflare bad gateway"), nil
		}
		return jsonResponse(request, http.StatusOK, stream), nil
	})}

	result, err := RunCoordinatorWithResult(context.Background(), client, CoordinatorRequest{
		BaseURL:   "https://coordinator.example.com",
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("RunCoordinatorWithResult returned error: %v", err)
	}
	if result.BuildID != "build-123" || result.MachineID != "machine-123" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if attempts != 2 {
		t.Fatalf("expected retry after retryable tunnel response, got %d attempts", attempts)
	}
}

func TestTailReconnectsAfterMissingTerminalStatus(t *testing.T) {
	tailCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/builds/build-123/logs" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		tailCount++
		response.Header().Set("Content-Type", "application/x-ndjson")
		if tailCount == 1 {
			if request.URL.Query().Get("after") != "0" {
				t.Fatalf("unexpected first after: %s", request.URL.RawQuery)
			}
			response.Write([]byte(`{"kind":"log","message":"line one","sequence":1}` + "\n"))
			return
		}
		if request.URL.Query().Get("after") != "1" {
			t.Fatalf("unexpected reconnect after: %s", request.URL.RawQuery)
		}
		response.Write([]byte(`{"kind":"build","message":"passed","sequence":2}` + "\n"))
	}))
	defer server.Close()

	var output bytes.Buffer
	err := Tail(context.Background(), server.Client(), Request{
		BaseURL: server.URL,
		Token:   "token",
		BuildID: "build-123",
	}, &output)
	if err != nil {
		t.Fatalf("Tail returned error: %v", err)
	}
	if tailCount != 2 {
		t.Fatalf("expected reconnect, got %d tails", tailCount)
	}
	if !strings.Contains(output.String(), "line one") {
		t.Fatalf("output missing first line: %s", output.String())
	}
}

func TestValidateBuildIDRejectsUnsafeValue(t *testing.T) {
	err := Request{
		BaseURL: "https://runner.example.com",
		Token:   "token",
		BuildID: "build/123",
	}.ValidateBuildID()
	if err == nil || !strings.Contains(err.Error(), "build_id") {
		t.Fatalf("expected unsafe build ID to fail, got %v", err)
	}
}

func TestCancelPostsCancelRequest(t *testing.T) {
	cancelled := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/builds/build-123/cancel" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		cancelled = true
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	err := Cancel(context.Background(), server.Client(), Request{
		BaseURL: server.URL,
		Token:   "token",
		BuildID: "build-123",
	})
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if !cancelled {
		t.Fatal("cancel request was not sent")
	}
}

func TestCancelCoordinatorPostsRequestIDCancel(t *testing.T) {
	cancelled := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/transwarp/dispatches/run-123/cancel" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer coord-token" {
			t.Fatalf("unexpected authorization: %s", request.Header.Get("Authorization"))
		}
		if request.Header.Get("CF-Access-Client-Id") != "access-id" {
			t.Fatalf("unexpected access client id: %s", request.Header.Get("CF-Access-Client-Id"))
		}
		if request.Header.Get("CF-Access-Client-Secret") != "access-secret" {
			t.Fatalf("unexpected access client secret: %s", request.Header.Get("CF-Access-Client-Secret"))
		}
		cancelled = true
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	err := CancelCoordinator(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:            server.URL,
		Token:              "coord-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		RequestID:          "run-123",
	})
	if err != nil {
		t.Fatalf("CancelCoordinator returned error: %v", err)
	}
	if !cancelled {
		t.Fatal("cancel request was not sent")
	}
}

func TestRunCoordinatorWithResultAppliesTimeoutWhenCanceling(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/transwarp/dispatches/run-123/cancel" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		RequestID: "run-123",
		Cancel:    true,
		Timeout:   20 * time.Millisecond,
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if result.RequestID != "run-123" {
		t.Fatalf("unexpected request ID: %s", result.RequestID)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("cancel request did not start")
	}
}

func TestRequestRejectsPartialCloudflareAccessCredentials(t *testing.T) {
	err := Request{
		BaseURL:        "https://transwarp.example.com",
		Token:          "token",
		AccessClientID: "access-id",
		JobID:          "echo",
		RequestID:      "run-123",
	}.ValidateStart()
	if err == nil {
		t.Fatal("expected partial Cloudflare Access credentials to fail")
	}
}

func TestRequestRequiresReportURLAndTokenTogether(t *testing.T) {
	tests := []Request{
		{
			BaseURL:   "https://transwarp.example.com",
			Token:     "token",
			JobID:     "echo",
			RequestID: "run-123",
			ReportURL: "https://ci.example.com/transwarp/result",
		},
		{
			BaseURL:     "https://transwarp.example.com",
			Token:       "token",
			JobID:       "echo",
			RequestID:   "run-123",
			ReportToken: "report-token",
		},
	}
	for _, request := range tests {
		err := request.ValidateStart()
		if err == nil || !strings.Contains(err.Error(), "report URL and report token") {
			t.Fatalf("expected report URL/token pair error, got %v", err)
		}
	}
}

func TestRequestRejectsHeaderControlCharacters(t *testing.T) {
	tests := []struct {
		name    string
		request Request
	}{
		{
			name: "token",
			request: Request{
				BaseURL:   "https://transwarp.example.com",
				Token:     "token\nInjected: yes",
				JobID:     "echo",
				RequestID: "run-123",
			},
		},
		{
			name: "access id",
			request: Request{
				BaseURL:            "https://transwarp.example.com",
				Token:              "token",
				AccessClientID:     "access-id\rInjected: yes",
				AccessClientSecret: "access-secret",
				JobID:              "echo",
				RequestID:          "run-123",
			},
		},
		{
			name: "access secret",
			request: Request{
				BaseURL:            "https://transwarp.example.com",
				Token:              "token",
				AccessClientID:     "access-id",
				AccessClientSecret: "access-secret\nInjected: yes",
				JobID:              "echo",
				RequestID:          "run-123",
			},
		},
		{
			name: "report token",
			request: Request{
				BaseURL:     "https://transwarp.example.com",
				Token:       "token",
				JobID:       "echo",
				RequestID:   "run-123",
				ReportURL:   "https://ci.example.com/transwarp/result",
				ReportToken: "report-token\nInjected: yes",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.ValidateStart()
			if err == nil || !strings.Contains(err.Error(), "single HTTP header value") {
				t.Fatalf("expected header value error, got %v", err)
			}
		})
	}
}

func TestRequestRejectsHeaderControlCharactersWhenUsingBuildID(t *testing.T) {
	tests := []struct {
		name    string
		request Request
	}{
		{
			name: "token",
			request: Request{
				BaseURL: "https://transwarp.example.com",
				Token:   "token\nInjected: yes",
				BuildID: "build-123",
			},
		},
		{
			name: "access secret",
			request: Request{
				BaseURL:            "https://transwarp.example.com",
				Token:              "token",
				AccessClientID:     "access-id",
				AccessClientSecret: "access-secret\nInjected: yes",
				BuildID:            "build-123",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.ValidateBuildID()
			if err == nil || !strings.Contains(err.Error(), "single HTTP header value") {
				t.Fatalf("expected header value error, got %v", err)
			}
		})
	}
}

func TestRequestValidatesReportURL(t *testing.T) {
	tests := []struct {
		name      string
		reportURL string
		wantError string
	}{
		{
			name:      "scheme",
			reportURL: "file:///tmp/result.json",
			wantError: "http or https",
		},
		{
			name:      "host",
			reportURL: "https:///transwarp/result",
			wantError: "host",
		},
		{
			name:      "credentials",
			reportURL: "https://user:password@ci.example.com/transwarp/result",
			wantError: "credentials",
		},
		{
			name:      "remote http",
			reportURL: "http://ci.example.com/transwarp/result",
			wantError: "https unless it targets local loopback",
		},
		{
			name:      "query",
			reportURL: "https://ci.example.com/transwarp/result?token=query-secret",
			wantError: "query or fragment",
		},
		{
			name:      "fragment",
			reportURL: "https://ci.example.com/transwarp/result#receipt",
			wantError: "query or fragment",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Request{
				BaseURL:     "https://transwarp.example.com",
				Token:       "token",
				JobID:       "echo",
				RequestID:   "run-123",
				ReportURL:   test.reportURL,
				ReportToken: "report-token",
			}.ValidateStart()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected report URL error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestRequestRequiresRequestID(t *testing.T) {
	err := Request{
		BaseURL: "https://transwarp.example.com",
		Token:   "token",
		JobID:   "echo",
	}.ValidateStart()
	if err == nil || !strings.Contains(err.Error(), "request ID") {
		t.Fatalf("expected missing request ID to fail, got %v", err)
	}
}

func TestRequestValidatesBaseURL(t *testing.T) {
	if err := (Request{
		BaseURL:   "https://transwarp.example.com",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	}).ValidateStart(); err != nil {
		t.Fatalf("expected HTTPS base URL to be accepted: %v", err)
	}
	if err := (Request{
		BaseURL:   "http://127.0.0.1:8188",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	}).ValidateStart(); err != nil {
		t.Fatalf("expected loopback HTTP base URL to be accepted: %v", err)
	}

	err := Request{
		BaseURL:   "http://transwarp.example.com",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	}.ValidateStart()
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected remote HTTP base URL to fail, got %v", err)
	}

	err = Request{
		BaseURL:   "https://user:password@transwarp.example.com",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
	}.ValidateStart()
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credentialed base URL to fail, got %v", err)
	}
}

func TestRequestRejectsUnsafeMetadata(t *testing.T) {
	err := Request{
		BaseURL:   "https://transwarp.example.com",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run-123",
		Ref:       "--help",
	}.ValidateStart()
	if err == nil || !strings.Contains(err.Error(), "ref") {
		t.Fatalf("expected unsafe ref to fail, got %v", err)
	}
}

func TestRequestRejectsUnsafeJobID(t *testing.T) {
	err := Request{
		BaseURL:   "https://transwarp.example.com",
		Token:     "token",
		JobID:     "xcode/debug",
		RequestID: "run-123",
	}.ValidateStart()
	if err == nil || !strings.Contains(err.Error(), "job_id") {
		t.Fatalf("expected unsafe job_id to fail, got %v", err)
	}
}

func TestRequestRejectsUnsafeRequestID(t *testing.T) {
	err := Request{
		BaseURL:   "https://transwarp.example.com",
		Token:     "token",
		JobID:     "echo",
		RequestID: "run/123",
	}.ValidateStart()
	if err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Fatalf("expected unsafe request_id to fail, got %v", err)
	}
}

func TestCoordinatorRequestRejectsUnsafeMetadata(t *testing.T) {
	err := CoordinatorRequest{
		BaseURL:   "https://coordinator.example.com",
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
		RepoURL:   "https://github.com/example/app.git\nInjected: true",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "repo_url") {
		t.Fatalf("expected unsafe repo_url to fail, got %v", err)
	}
}

func TestCoordinatorRequestRejectsUnsafeJobID(t *testing.T) {
	err := CoordinatorRequest{
		BaseURL:   "https://coordinator.example.com",
		Token:     "coord-token",
		JobID:     "xcode/debug",
		RequestID: "run-123",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "job_id") {
		t.Fatalf("expected unsafe job_id to fail, got %v", err)
	}
}

func TestCoordinatorRequestRejectsUnsafeRequestID(t *testing.T) {
	err := CoordinatorRequest{
		BaseURL:   "https://coordinator.example.com",
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run/123",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Fatalf("expected unsafe request_id to fail, got %v", err)
	}
}

func TestCoordinatorRequestRejectsHeaderControlCharacters(t *testing.T) {
	tests := []struct {
		name    string
		request CoordinatorRequest
		cancel  bool
	}{
		{
			name: "token",
			request: CoordinatorRequest{
				BaseURL:   "https://coordinator.example.com",
				Token:     "coord-token\nInjected: yes",
				JobID:     "xcode-debug",
				RequestID: "run-123",
			},
		},
		{
			name: "access id",
			request: CoordinatorRequest{
				BaseURL:            "https://coordinator.example.com",
				Token:              "coord-token",
				AccessClientID:     "access-id\rInjected: yes",
				AccessClientSecret: "access-secret",
				JobID:              "xcode-debug",
				RequestID:          "run-123",
			},
		},
		{
			name: "access secret",
			request: CoordinatorRequest{
				BaseURL:            "https://coordinator.example.com",
				Token:              "coord-token",
				AccessClientID:     "access-id",
				AccessClientSecret: "access-secret\nInjected: yes",
				JobID:              "xcode-debug",
				RequestID:          "run-123",
			},
		},
		{
			name: "cancel token",
			request: CoordinatorRequest{
				BaseURL:   "https://coordinator.example.com",
				Token:     "coord-token\nInjected: yes",
				RequestID: "run-123",
			},
			cancel: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			if test.cancel {
				err = test.request.ValidateCancel()
			} else {
				err = test.request.Validate()
			}
			if err == nil || !strings.Contains(err.Error(), "single HTTP header value") {
				t.Fatalf("expected header value error, got %v", err)
			}
		})
	}
}

func TestCoordinatorRequestRejectsUnsafeMachineID(t *testing.T) {
	err := CoordinatorRequest{
		BaseURL:   "https://coordinator.example.com",
		Token:     "coord-token",
		MachineID: "machine/123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "machine_id") {
		t.Fatalf("expected unsafe machine_id to fail, got %v", err)
	}
}

func TestCoordinatorCancelRejectsUnsafeRequestID(t *testing.T) {
	err := CoordinatorRequest{
		BaseURL:   "https://coordinator.example.com",
		Token:     "coord-token",
		RequestID: "run/123",
	}.ValidateCancel()
	if err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Fatalf("expected unsafe request ID to fail, got %v", err)
	}
}

func TestCoordinatorRequestRejectsRemoteHTTPBaseURL(t *testing.T) {
	err := CoordinatorRequest{
		BaseURL:   "http://coordinator.example.com",
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected remote HTTP coordinator URL to fail, got %v", err)
	}
}

func TestCoordinatorRequestRejectsNegativeMinCPUCount(t *testing.T) {
	err := CoordinatorRequest{
		BaseURL:     "https://coordinator.example.com",
		Token:       "coord-token",
		JobID:       "xcode-debug",
		RequestID:   "run-123",
		MinCPUCount: -1,
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "min CPU count") {
		t.Fatalf("expected negative min CPU count to fail, got %v", err)
	}
}

func TestCoordinatorRequestRejectsInvalidMinXcodeVersion(t *testing.T) {
	err := CoordinatorRequest{
		BaseURL:         "https://coordinator.example.com",
		Token:           "coord-token",
		JobID:           "xcode-debug",
		RequestID:       "run-123",
		MinXcodeVersion: "latest",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "min Xcode version") {
		t.Fatalf("expected invalid min Xcode version to fail, got %v", err)
	}
}

func TestRunFailsOnRemoteFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			response.Header().Set("Content-Type", "application/x-ndjson")
			response.Write([]byte(`{"kind":"log","message":"compiling","sequence":1}` + "\n"))
			response.Write([]byte(`{"kind":"build","message":"failed with exit code 65","sequence":2}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "token",
		JobID:     "xcode",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected remote failure to return error")
	}
	if !strings.Contains(err.Error(), "failed with exit code 65") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWithResultReturnsAcceptedBuildIDOnRemoteFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			response.Write([]byte(`{"kind":"build","message":"failed with exit code 65","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	result, err := RunWithResult(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "token",
		JobID:     "xcode",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected remote failure")
	}
	if result.RequestID != "run-123" {
		t.Fatalf("unexpected request ID: %s", result.RequestID)
	}
	if result.BuildID != "build-123" {
		t.Fatalf("unexpected build ID: %s", result.BuildID)
	}
	if result.JobID != "xcode" {
		t.Fatalf("unexpected job ID: %s", result.JobID)
	}
	if result.Status != "failed" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.ExitCode != 65 {
		t.Fatalf("unexpected exit code: %d", result.ExitCode)
	}
	if result.Error != "failed with exit code 65" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestRunWithResultReturnsTerminalStatusOnPass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	result, err := RunWithResult(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "token",
		JobID:     "xcode",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("RunWithResult returned error: %v", err)
	}
	if result.Status != "passed" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", result.ExitCode)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestRunFailsWhenResultReportFailsAfterBuildPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			response.Header().Set("Content-Type", "application/x-ndjson")
			response.Write([]byte(`{"kind":"log","message":"compiled","sequence":1}` + "\n"))
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":2}` + "\n"))
			response.Write([]byte(`{"kind":"error","message":"report failed: report endpoint returned 500 Internal Server Error","sequence":3}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	var output bytes.Buffer
	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "token",
		JobID:     "xcode",
		RequestID: "run-123",
	}, &output)

	if err == nil {
		t.Fatal("expected report failure to return error")
	}
	if !strings.Contains(err.Error(), "report failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), "[error] report failed") {
		t.Fatalf("output missing report failure: %s", output.String())
	}
}

func TestRunWithResultReturnsTerminalStatusWhenResultReportFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds":
			response.WriteHeader(http.StatusAccepted)
			response.Write([]byte(`{"build_id":"build-123","status":"running"}`))
		case "/v1/builds/build-123/logs":
			response.Write([]byte(`{"kind":"build","message":"passed","sequence":1}` + "\n"))
			response.Write([]byte(`{"kind":"error","message":"report failed: report endpoint returned 500 Internal Server Error","sequence":2}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	result, err := RunWithResult(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "token",
		JobID:     "xcode",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected report failure")
	}
	if result.Status != "passed" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", result.ExitCode)
	}
	if result.Error != "report failed: report endpoint returned 500 Internal Server Error" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestTailCancelsBuildWhenContextEnds(t *testing.T) {
	cancelled := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds/build-123/logs":
			response.Write([]byte(`{"kind":"log","message":"partial","sequence":1}` + "\n"))
		case "/v1/builds/build-123/cancel":
			cancelled = true
			response.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := Tail(ctx, server.Client(), Request{
		BaseURL: server.URL,
		Token:   "token",
		BuildID: "build-123",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected context error")
	}
	if !cancelled {
		t.Fatal("expected tail context cancellation to cancel remote build")
	}
}

func TestRunWithResultAppliesTimeoutWhenTailingExistingBuild(t *testing.T) {
	cancelled := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/builds/build-123/logs":
			response.Write([]byte(`{"kind":"log","message":"partial","sequence":1}` + "\n"))
		case "/v1/builds/build-123/cancel":
			cancelled = true
			response.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	result, err := RunWithResult(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "token",
		BuildID:   "build-123",
		RequestID: "run-123",
		Timeout:   20 * time.Millisecond,
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if result.BuildID != "build-123" {
		t.Fatalf("unexpected build ID: %s", result.BuildID)
	}
	if !cancelled {
		t.Fatal("expected timeout while tailing to cancel remote build")
	}
}

func TestRunReturnsHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		response.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	err := Run(context.Background(), server.Client(), Request{
		BaseURL:   server.URL,
		Token:     "bad",
		JobID:     "xcode",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCoordinatorDispatchesAndStreamsOutput(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/transwarp/dispatch" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer coord-token" {
			t.Fatalf("unexpected authorization: %s", request.Header.Get("Authorization"))
		}
		if request.Header.Get("CF-Access-Client-Id") != "access-id" {
			t.Fatalf("unexpected access client id: %s", request.Header.Get("CF-Access-Client-Id"))
		}
		if request.Header.Get("CF-Access-Client-Secret") != "access-secret" {
			t.Fatalf("unexpected access client secret: %s", request.Header.Get("CF-Access-Client-Secret"))
		}
		body := new(bytes.Buffer)
		if _, err := body.ReadFrom(request.Body); err != nil {
			t.Fatal(err)
		}
		receivedBody = body.String()
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","public_url":"https://runner.example.com"}` + "\n"))
		response.Write([]byte("building on registered mac\n"))
		response.Write([]byte("[build] passed\n"))
	}))
	defer server.Close()

	var output bytes.Buffer
	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:            server.URL,
		Token:              "coord-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		JobID:              "xcode-debug",
		RequestID:          "run-123",
		RepoURL:            "https://github.com/example/app.git",
		Ref:                "refs/heads/main",
		Commit:             "abc123",
		MinCPUCount:        12,
		MinMemoryBytes:     34359738368,
		MinXcodeVersion:    "16.4",
	}, &output)

	if err != nil {
		t.Fatalf("RunCoordinator returned error: %v", err)
	}
	if !strings.Contains(receivedBody, `"job_id":"xcode-debug"`) {
		t.Fatalf("coordinator request body missing job: %s", receivedBody)
	}
	if !strings.Contains(receivedBody, `"request_id":"run-123"`) {
		t.Fatalf("coordinator request body missing request id: %s", receivedBody)
	}
	if !strings.Contains(receivedBody, `"min_cpu_count":12`) {
		t.Fatalf("coordinator request body missing min cpu count: %s", receivedBody)
	}
	if !strings.Contains(receivedBody, `"min_memory_bytes":34359738368`) {
		t.Fatalf("coordinator request body missing min memory bytes: %s", receivedBody)
	}
	if !strings.Contains(receivedBody, `"min_xcode_version":"16.4"`) {
		t.Fatalf("coordinator request body missing min xcode version: %s", receivedBody)
	}
	if !strings.Contains(output.String(), "building on registered mac") {
		t.Fatalf("output missing coordinator stream: %s", output.String())
	}
	if result.BuildID != "build-123" {
		t.Fatalf("unexpected build ID: %s", result.BuildID)
	}
	if result.JobID != "xcode-debug" {
		t.Fatalf("unexpected job ID: %s", result.JobID)
	}
	if result.MachineID != "machine-123" {
		t.Fatalf("unexpected machine ID: %s", result.MachineID)
	}
	if result.PublicURL != "https://runner.example.com" {
		t.Fatalf("unexpected public URL: %s", result.PublicURL)
	}
}

func TestRunCoordinatorDoesNotFollowRedirects(t *testing.T) {
	redirected := make(chan http.Header, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		redirected <- request.Header.Clone()
		response.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		http.Redirect(response, request, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := RunCoordinatorWithResult(context.Background(), nil, CoordinatorRequest{
		BaseURL:            server.URL,
		Token:              "coord-token",
		AccessClientID:     "access-id",
		AccessClientSecret: "access-secret",
		JobID:              "xcode-debug",
		RequestID:          "run-redirect",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "307 Temporary Redirect") {
		t.Fatalf("expected redirect coordinator dispatch error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected redirect to fail without retry, got %d attempts", attempts)
	}
	select {
	case headers := <-redirected:
		t.Fatalf("coordinator redirect leaked headers: Authorization=%q CF-Access-Client-Secret=%q", headers.Get("Authorization"), headers.Get("CF-Access-Client-Secret"))
	default:
	}
}

func TestRunCoordinatorRequiresAcceptedBuildMetadataForSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte("building on registered mac\n"))
		response.Write([]byte("[result] recorded passed\n"))
	}))
	defer server.Close()

	var output bytes.Buffer
	err := RunCoordinator(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &output)

	if err == nil || !strings.Contains(err.Error(), "accepted runner build metadata") {
		t.Fatalf("expected missing accepted-build metadata error, got %v", err)
	}
	if !strings.Contains(output.String(), "[result] recorded passed") {
		t.Fatalf("output missing original stream: %s", output.String())
	}
}

func TestRunCoordinatorReturnsStreamedDispatchFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte("compiling\n"))
		response.Write([]byte("dispatch failed: failed with exit code 65\n"))
	}))
	defer server.Close()

	var output bytes.Buffer
	err := RunCoordinator(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &output)

	if err == nil {
		t.Fatal("expected streamed dispatch failure")
	}
	if !strings.Contains(err.Error(), "failed with exit code 65") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), "compiling") {
		t.Fatalf("output missing streamed line: %s", output.String())
	}
}

func TestRunCoordinatorWithResultReturnsDispatchIDsOnStreamedFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","public_url":"https://runner.example.com"}` + "\n"))
		response.Write([]byte("dispatch failed: failed with exit code 65\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil {
		t.Fatal("expected streamed dispatch failure")
	}
	if result.RequestID != "run-123" {
		t.Fatalf("unexpected request ID: %s", result.RequestID)
	}
	if result.BuildID != "build-123" {
		t.Fatalf("unexpected build ID: %s", result.BuildID)
	}
	if result.JobID != "xcode-debug" {
		t.Fatalf("unexpected job ID: %s", result.JobID)
	}
	if result.MachineID != "machine-123" {
		t.Fatalf("unexpected machine ID: %s", result.MachineID)
	}
	if result.PublicURL != "https://runner.example.com" {
		t.Fatalf("unexpected public URL: %s", result.PublicURL)
	}
}

func TestRunCoordinatorIgnoresBuildLogJSONWhenReturningDispatchIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"log","message":"user output","build_id":"build-spoof","machine_id":"machine-spoof","public_url":"https://spoof.example.com"}` + "\n"))
		response.Write([]byte(`{"build_id":"build-also-spoofed","machine_id":"machine-also-spoofed","public_url":"https://also-spoofed.example.com"}` + "\n"))
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","public_url":"https://runner.example.com"}` + "\n"))
		response.Write([]byte("[result] recorded passed\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("RunCoordinatorWithResult returned error: %v", err)
	}
	if result.BuildID != "build-123" {
		t.Fatalf("unexpected build ID: %s", result.BuildID)
	}
	if result.JobID != "xcode-debug" {
		t.Fatalf("unexpected job ID: %s", result.JobID)
	}
	if result.MachineID != "machine-123" {
		t.Fatalf("unexpected machine ID: %s", result.MachineID)
	}
	if result.PublicURL != "https://runner.example.com" {
		t.Fatalf("unexpected public URL: %s", result.PublicURL)
	}
}

func TestRunCoordinatorRequiresCompleteAcceptedBuildMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"log","message":"user output","build_id":"build/unsafe","machine_id":"machine/unsafe","public_url":"https://runner.example.com/path"}` + "\n"))
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug"}` + "\n"))
		response.Write([]byte("[result] recorded passed\n"))
	}))
	defer server.Close()

	_, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "missing request_id") {
		t.Fatalf("expected incomplete accepted-build metadata to fail, got %v", err)
	}
}

func TestRunCoordinatorRejectsUnsafeStreamedBuildID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build/123","job_id":"xcode-debug"}` + "\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "build_id") {
		t.Fatalf("expected unsafe streamed build_id to fail, got %v", err)
	}
	if result.BuildID != "" {
		t.Fatalf("unsafe build ID should not be returned: %s", result.BuildID)
	}
}

func TestRunCoordinatorRejectsUnsafeStreamedJobID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode/debug"}` + "\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "job_id") {
		t.Fatalf("expected unsafe streamed job_id to fail, got %v", err)
	}
	if result.BuildID != "" {
		t.Fatalf("build ID from unsafe coordinator metadata should not be returned: %s", result.BuildID)
	}
}

func TestRunCoordinatorRejectsUnsafeStreamedMachineID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine/123","public_url":"https://runner.example.com"}` + "\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "machine_id") {
		t.Fatalf("expected unsafe streamed machine_id to fail, got %v", err)
	}
	if result.BuildID != "" {
		t.Fatalf("build ID from unsafe coordinator metadata should not be returned: %s", result.BuildID)
	}
}

func TestRunCoordinatorRejectsUnsafeStreamedPublicURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","public_url":"https://runner.example.com/path"}` + "\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "public_url") {
		t.Fatalf("expected unsafe streamed public_url to fail, got %v", err)
	}
	if result.BuildID != "" {
		t.Fatalf("build ID from unsafe coordinator metadata should not be returned: %s", result.BuildID)
	}
}

func TestRunCoordinatorRejectsAcceptedMetadataForDifferentRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"other-run","machine_id":"machine-123","public_url":"https://runner.example.com"}` + "\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "does not match requested request_id") {
		t.Fatalf("expected request_id mismatch to fail, got %v", err)
	}
	if result.BuildID != "" {
		t.Fatalf("build ID from mismatched coordinator metadata should not be returned: %s", result.BuildID)
	}
}

func TestRunCoordinatorRejectsAcceptedMetadataForDifferentJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"other-job","request_id":"run-123","machine_id":"machine-123","public_url":"https://runner.example.com"}` + "\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "does not match requested job_id") {
		t.Fatalf("expected job_id mismatch to fail, got %v", err)
	}
	if result.BuildID != "" {
		t.Fatalf("build ID from mismatched coordinator metadata should not be returned: %s", result.BuildID)
	}
}

func TestRunCoordinatorRejectsAcceptedMetadataForDifferentPinnedMachine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"other-machine","public_url":"https://runner.example.com"}` + "\n"))
	}))
	defer server.Close()

	result, err := RunCoordinatorWithResult(context.Background(), server.Client(), CoordinatorRequest{
		BaseURL:   server.URL,
		Token:     "coord-token",
		MachineID: "machine-123",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "does not match requested machine_id") {
		t.Fatalf("expected machine_id mismatch to fail, got %v", err)
	}
	if result.BuildID != "" {
		t.Fatalf("build ID from mismatched coordinator metadata should not be returned: %s", result.BuildID)
	}
}

func TestRunCoordinatorDoesNotCancelAfterSuccessfulStreamEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := false
	stream := "" +
		`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"xcode-debug","request_id":"run-123","machine_id":"machine-123","public_url":"https://runner.example.com"}` + "\n" +
		"[result] recorded passed\n"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/transwarp/dispatch":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       &cancelAtEOFReadCloser{reader: strings.NewReader(stream), cancel: cancel},
				Request:    request,
			}, nil
		case "/transwarp/dispatches/run-123/cancel":
			cancelled = true
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Status:     "202 Accepted",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
			return nil, nil
		}
	})}

	result, err := RunCoordinatorWithResult(ctx, client, CoordinatorRequest{
		BaseURL:   "https://coordinator.example.com",
		Token:     "coord-token",
		JobID:     "xcode-debug",
		RequestID: "run-123",
	}, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("RunCoordinatorWithResult returned error after successful stream: %v", err)
	}
	if result.BuildID != "build-123" || result.MachineID != "machine-123" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if cancelled {
		t.Fatal("successful coordinator stream should not send a cancel request")
	}
}

func TestRunCoordinatorCancelsActiveDispatchWhenContextEnds(t *testing.T) {
	streamStarted := make(chan struct{})
	cancelled := make(chan struct{}, 1)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/transwarp/dispatch":
			reader, writer := io.Pipe()
			go func() {
				_, _ = writer.Write([]byte("starting\n"))
				close(streamStarted)
				<-request.Context().Done()
				_ = writer.CloseWithError(request.Context().Err())
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       reader,
				Request:    request,
			}, nil
		case "/transwarp/dispatches/run-123/cancel":
			select {
			case cancelled <- struct{}{}:
			default:
			}
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Status:     "202 Accepted",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
			return nil, nil
		}
	})}

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() {
		errs <- RunCoordinator(ctx, client, CoordinatorRequest{
			BaseURL:   "https://coordinator.example.com",
			Token:     "coord-token",
			JobID:     "xcode-debug",
			RequestID: "run-123",
		}, &bytes.Buffer{})
	}()

	select {
	case <-streamStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("coordinator stream did not start")
	}
	cancel()

	select {
	case err := <-errs:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("expected context canceled error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunCoordinator did not return after cancel")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("coordinator cancel request was not sent")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type cancelAtEOFReadCloser struct {
	reader *strings.Reader
	cancel context.CancelFunc
}

func (reader *cancelAtEOFReadCloser) Read(data []byte) (int, error) {
	n, err := reader.reader.Read(data)
	if err == io.EOF {
		reader.cancel()
	}
	return n, err
}

func (reader *cancelAtEOFReadCloser) Close() error {
	return nil
}

func withDispatchRetryDelays(t *testing.T, delays []time.Duration) {
	t.Helper()
	original := dispatchRetryDelays
	dispatchRetryDelays = delays
	t.Cleanup(func() {
		dispatchRetryDelays = original
	})
}

func transientDNSError(request *http.Request) error {
	return &url.Error{
		Op:  request.Method,
		URL: request.URL.String(),
		Err: &net.DNSError{Name: request.URL.Hostname(), IsNotFound: true},
	}
}

func jsonResponse(request *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestBuildEnvironmentDoesNotInheritArbitraryVariables(t *testing.T) {
	t.Setenv("SHOULD_NOT_LEAK", "secret")

	env := buildEnvironment(
		map[string]string{"PROJECT_LOCAL": "yes"},
		BuildRequest{RepoURL: "https://example.com/repo.git", Ref: "main", Commit: "abc123"},
		"/tmp/transwarp-workspace",
	)
	joined := strings.Join(env, "\n")

	if strings.Contains(joined, "SHOULD_NOT_LEAK") {
		t.Fatal("environment leaked process variable")
	}
	if !strings.Contains(joined, "PROJECT_LOCAL=yes") {
		t.Fatal("configured environment was missing")
	}
	if !strings.Contains(joined, "PATH=") {
		t.Fatal("PATH was missing")
	}
	if !strings.Contains(joined, "TRANSWARP_WORKSPACE=/tmp/transwarp-workspace") {
		t.Fatal("workspace environment was missing")
	}
}

func TestBuildEnvironmentIgnoresLocalTranswarpMetadataOverrides(t *testing.T) {
	env := environmentMap(buildEnvironment(
		map[string]string{
			"TRANSWARP_WORKSPACE": "/fake/workspace",
			"TRANSWARP_REPO_URL":  "https://example.com/fake.git",
			"TRANSWARP_REF":       "fake",
			"TRANSWARP_COMMIT":    "fake",
		},
		BuildRequest{
			RepoURL: "https://example.com/real.git",
			Ref:     "main",
			Commit:  "abc123",
		},
		"/tmp/transwarp-real-workspace",
	))

	if env["TRANSWARP_WORKSPACE"] != "/tmp/transwarp-real-workspace" {
		t.Fatalf("workspace metadata was overridden: %+v", env)
	}
	if env["TRANSWARP_REPO_URL"] != "https://example.com/real.git" {
		t.Fatalf("repo metadata was overridden: %+v", env)
	}
	if env["TRANSWARP_REF"] != "main" {
		t.Fatalf("ref metadata was overridden: %+v", env)
	}
	if env["TRANSWARP_COMMIT"] != "abc123" {
		t.Fatalf("commit metadata was overridden: %+v", env)
	}
}

func TestValidateBuildRequestForJobRejectsCheckoutMetadataForNonCheckoutJobs(t *testing.T) {
	job := JobConfig{
		ID:       "local",
		Checkout: false,
	}
	tests := []struct {
		name    string
		request BuildRequest
		want    string
	}{
		{
			name: "repo",
			request: BuildRequest{
				JobID:     "local",
				RequestID: "request-123",
				RepoURL:   "https://github.com/example/app.git",
			},
			want: "repo_url is only accepted for checkout jobs",
		},
		{
			name: "ref",
			request: BuildRequest{
				JobID:     "local",
				RequestID: "request-123",
				Ref:       "main",
			},
			want: "ref is only accepted for checkout jobs",
		},
		{
			name: "commit",
			request: BuildRequest{
				JobID:     "local",
				RequestID: "request-123",
				Commit:    "abc123",
			},
			want: "commit is only accepted for checkout jobs",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateBuildRequestForJob(test.request, job)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestBuildRequestValidatesReportURL(t *testing.T) {
	err := BuildRequest{JobID: "build", RequestID: "request-123", ReportURL: "file:///tmp/result.json", ReportToken: "report-token"}.Validate()
	if err == nil {
		t.Fatal("expected unsupported report URL scheme to fail")
	}
}

func TestBuildRequestRejectsReportURLWithoutHost(t *testing.T) {
	err := BuildRequest{JobID: "build", RequestID: "request-123", ReportURL: "https:///transwarp/result", ReportToken: "report-token"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected report URL without host to fail, got %v", err)
	}
}

func TestBuildRequestRejectsReportURLCredentials(t *testing.T) {
	err := BuildRequest{JobID: "build", RequestID: "request-123", ReportURL: "https://user:password@ci.example.com/transwarp/result", ReportToken: "report-token"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected report URL credentials to fail, got %v", err)
	}
}

func TestBuildRequestRejectsRemoteHTTPReportURL(t *testing.T) {
	err := BuildRequest{JobID: "build", RequestID: "request-123", ReportURL: "http://ci.example.com/transwarp/result", ReportToken: "report-token"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "https unless it targets local loopback") {
		t.Fatalf("expected remote http report URL to fail, got %v", err)
	}
}

func TestBuildRequestRejectsReportURLQueryOrFragment(t *testing.T) {
	tests := []string{
		"https://ci.example.com/transwarp/result?token=query-secret",
		"https://ci.example.com/transwarp/result#receipt",
	}
	for _, reportURL := range tests {
		err := BuildRequest{JobID: "build", RequestID: "request-123", ReportURL: reportURL, ReportToken: "report-token"}.Validate()
		if err == nil || !strings.Contains(err.Error(), "query or fragment") {
			t.Fatalf("expected query or fragment rejection for %q, got %v", reportURL, err)
		}
	}
}

func TestBuildRequestRequiresReportURLAndTokenTogether(t *testing.T) {
	tests := []BuildRequest{
		{JobID: "build", RequestID: "request-123", ReportURL: "https://ci.example.com/transwarp/result"},
		{JobID: "build", RequestID: "request-123", ReportToken: "report-token"},
	}
	for _, request := range tests {
		err := request.Validate()
		if err == nil || !strings.Contains(err.Error(), "report_url and report_token") {
			t.Fatalf("expected report_url/report_token pair error, got %v", err)
		}
	}
}

func TestBuildRequestRejectsReportTokenControlCharacters(t *testing.T) {
	err := BuildRequest{
		JobID:       "build",
		RequestID:   "request-123",
		ReportURL:   "https://ci.example.com/transwarp/result",
		ReportToken: "report-token\nInjected: yes",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "report_token") {
		t.Fatalf("expected report_token header value error, got %v", err)
	}
}

func TestBuildRequestRequiresRequestID(t *testing.T) {
	err := BuildRequest{JobID: "build"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Fatalf("expected missing request_id to fail, got %v", err)
	}
}

func TestBuildRequestRejectsUnsafeRequestID(t *testing.T) {
	err := BuildRequest{JobID: "build", RequestID: "request/123"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Fatalf("expected unsafe request_id to fail, got %v", err)
	}
}

func TestBuildRequestRejectsUnsafeJobID(t *testing.T) {
	err := BuildRequest{JobID: "xcode/debug", RequestID: "request-123"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "job_id") {
		t.Fatalf("expected unsafe job_id to fail, got %v", err)
	}
}

func TestBuildRequestRejectsUnsafeMetadata(t *testing.T) {
	tests := []struct {
		name      string
		request   BuildRequest
		wantError string
	}{
		{
			name: "repo URL control character",
			request: BuildRequest{
				JobID:     "build",
				RequestID: "request-123",
				RepoURL:   "https://github.com/example/app.git\nInjected: true",
			},
			wantError: "repo_url must not contain control characters",
		},
		{
			name: "ref option",
			request: BuildRequest{
				JobID:     "build",
				RequestID: "request-123",
				Ref:       "--help",
			},
			wantError: "ref",
		},
		{
			name: "commit whitespace",
			request: BuildRequest{
				JobID:     "build",
				RequestID: "request-123",
				Commit:    "abc123 next",
			},
			wantError: "commit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestStartBuildRejectsUnsafeMetadataBeforeQueueing(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"bad-ref","ref":"--help"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "ref") {
		t.Fatalf("expected ref validation error, got %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("invalid metadata should not queue a build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestStartBuildRejectsCheckoutMetadataForNonCheckoutJobBeforeQueueing(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"metadata-without-checkout","repo_url":"https://github.com/example/app.git","ref":"main"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "checkout jobs") {
		t.Fatalf("expected checkout metadata validation error, got %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("checkout metadata should not queue non-checkout build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestStartBuildRejectsUnknownJSONFieldsBeforeQueueing(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"unknown-field","reportToken":"typo"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body: %s", response.Code, response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("unknown JSON field should not queue a build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestStartBuildRejectsTrailingJSONBeforeQueueing(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"trailing-json"} {"job_id":"echo","request_id":"second"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body: %s", response.Code, response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("trailing JSON should not queue a build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestStartBuildAllowsReportURLOnConfiguredCIOrigin(t *testing.T) {
	received := make(chan BuildReport, 1)
	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var report BuildReport
		if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
			t.Fatal(err)
		}
		received <- report
		response.WriteHeader(http.StatusAccepted)
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		CIRegistrationURL: reportServer.URL + "/transwarp/register",
		Tunnel:            TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"allowed-report","report_url":"` + reportServer.URL + `/transwarp/result","report_token":"report-token"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body: %s", response.Code, response.Body.String())
	}
	var started BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	run, ok := agent.build(started.BuildID)
	if !ok {
		t.Fatal("build was not retained")
	}
	waitForRun(t, run)
	report := waitForReport(t, received)
	if report.RequestID != "allowed-report" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestStartBuildReturnsExistingRunForDuplicateRequestID(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"retry-123"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()
	agent.startBuild(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected first status: %d body: %s", response.Code, response.Body.String())
	}
	var first BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}

	body = bytes.NewBufferString(`{"job_id":"echo","request_id":"retry-123"}`)
	request = httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response = httptest.NewRecorder()
	agent.startBuild(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected retry status: %d body: %s", response.Code, response.Body.String())
	}
	var second BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&second); err != nil {
		t.Fatal(err)
	}

	if second.BuildID != first.BuildID {
		t.Fatalf("expected duplicate request to reuse build %s, got %s", first.BuildID, second.BuildID)
	}
}

func TestStartBuildRejectsNewBuildsWhenPaused(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})
	agent.setAcceptingBuilds(false)

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"paused-new"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()
	agent.startBuild(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected service unavailable, got %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "paused") {
		t.Fatalf("expected paused error, got %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("paused runner should not queue new builds; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestAvailabilityEndpointUpdatesStatus(t *testing.T) {
	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		ListenAddress: "127.0.0.1:8188",
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/availability", bytes.NewBufferString(`{"accepting_builds":false}`))
	response := httptest.NewRecorder()
	agent.setAvailability(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected availability status: %d body: %s", response.Code, response.Body.String())
	}
	var availability AvailabilityResponse
	if err := json.NewDecoder(response.Body).Decode(&availability); err != nil {
		t.Fatal(err)
	}
	if availability.AcceptingBuilds {
		t.Fatal("expected accepting_builds=false")
	}

	statusResponse := httptest.NewRecorder()
	agent.status(statusResponse, httptest.NewRequest(http.MethodGet, "/status", nil))
	var status StatusResponse
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.AcceptingBuilds {
		t.Fatalf("status did not reflect paused availability: %+v", status)
	}
}

func TestAvailabilityEndpointRequiresExplicitState(t *testing.T) {
	agent := New(Config{})
	request := httptest.NewRequest(http.MethodPost, "/v1/availability", bytes.NewBufferString(`{}`))
	response := httptest.NewRecorder()

	agent.setAvailability(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body: %s", response.Code, response.Body.String())
	}
}

func TestStartBuildReturnsExistingRunWhenPausedAfterAcceptedRequest(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"accepted-before-pause"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()
	agent.startBuild(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected first status: %d body: %s", response.Code, response.Body.String())
	}
	var first BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}

	agent.setAcceptingBuilds(false)
	body = bytes.NewBufferString(`{"job_id":"echo","request_id":"accepted-before-pause"}`)
	request = httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response = httptest.NewRecorder()
	agent.startBuild(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected retry status: %d body: %s", response.Code, response.Body.String())
	}
	var retry BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&retry); err != nil {
		t.Fatal(err)
	}
	if retry.BuildID != first.BuildID {
		t.Fatalf("retry returned different build: first=%s retry=%s", first.BuildID, retry.BuildID)
	}
}

func TestStartBuildDuplicateRequestIDReportsCompletedStatus(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"retry-complete"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()
	agent.startBuild(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected first status: %d body: %s", response.Code, response.Body.String())
	}
	var first BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}
	run, ok := agent.build(first.BuildID)
	if !ok {
		t.Fatal("build was not retained")
	}
	waitForRun(t, run)

	body = bytes.NewBufferString(`{"job_id":"echo","request_id":"retry-complete"}`)
	request = httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response = httptest.NewRecorder()
	agent.startBuild(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected retry status: %d body: %s", response.Code, response.Body.String())
	}
	var second BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&second); err != nil {
		t.Fatal(err)
	}

	if second.BuildID != first.BuildID {
		t.Fatalf("expected duplicate request to reuse build %s, got %s", first.BuildID, second.BuildID)
	}
	if second.Status != "passed" {
		t.Fatalf("expected completed duplicate to report passed, got %s", second.Status)
	}
}

func TestRecentBuildsIncludesTerminalLedgerHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := New(Config{})
	started := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	ended := started.Add(2 * time.Second)

	agent.writeLedger(LedgerRecord{
		BuildID:   "build-passed",
		JobID:     "xcode-debug",
		RequestID: "request-123",
		RepoURL:   "https://github.com/example/app.git",
		Ref:       "refs/heads/main",
		Commit:    "abc123",
		Workspace: "/Users/charlie/private/transwarp-workspace",
		StartedAt: started,
		EndedAt:   ended,
		ExitCode:  0,
	})

	builds := agent.recentBuilds(10)
	if len(builds) != 1 {
		t.Fatalf("expected one recent build from ledger, got %+v", builds)
	}
	build := builds[0]
	if build.BuildID != "build-passed" || build.Status != "passed" {
		t.Fatalf("unexpected ledger build status: %+v", build)
	}
	if build.Result == nil {
		t.Fatalf("ledger build did not include result: %+v", build)
	}
	if build.Result.RepoURL != "https://github.com/example/app.git" || build.Result.Ref != "refs/heads/main" || build.Result.Commit != "abc123" {
		t.Fatalf("ledger build lost CI metadata: %+v", build.Result)
	}

	data, err := json.Marshal(build)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "transwarp-workspace") || strings.Contains(string(data), "Workspace") {
		t.Fatalf("recent build status leaked local workspace path: %s", string(data))
	}
}

func TestRecentBuildsUsesLatestLedgerReportStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := New(Config{})
	started := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	record := LedgerRecord{
		BuildID:      "build-reported",
		JobID:        "xcode-debug",
		RequestID:    "request-123",
		StartedAt:    started,
		EndedAt:      started.Add(2 * time.Second),
		ExitCode:     0,
		ReportStatus: "pending",
	}

	agent.writeLedger(record)
	record.ReportStatus = "failed"
	record.ReportError = "report endpoint returned 500 Internal Server Error"
	agent.writeLedger(record)

	builds := agent.recentBuilds(10)
	if len(builds) != 1 {
		t.Fatalf("expected one de-duplicated ledger build, got %+v", builds)
	}
	if builds[0].ReportStatus != "failed" || builds[0].ReportError != "report endpoint returned 500 Internal Server Error" {
		t.Fatalf("expected latest report failure status, got %+v", builds[0])
	}
}

func TestRecentBuildsSkipsMalformedLedgerHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := New(Config{})
	path, err := ledgerPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join([]string{
		`not-json`,
		`{"build_id":"build/unsafe","job_id":"xcode-debug","request_id":"request-123","started_at":"2026-07-27T10:00:00Z","ended_at":"2026-07-27T10:00:01Z","exit_code":0}`,
		`{"build_id":"build-unsafe-report","job_id":"xcode-debug","request_id":"request-234","started_at":"2026-07-27T10:00:00Z","ended_at":"2026-07-27T10:00:01Z","exit_code":0,"report_status":"maybe"}`,
		`{"build_id":"build-good","job_id":"xcode-debug","request_id":"request-456","started_at":"2026-07-27T10:01:00Z","ended_at":"2026-07-27T10:01:01Z","exit_code":65,"error":"xcodebuild exited with 65"}`,
	}, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}

	builds := agent.recentBuilds(10)
	if len(builds) != 1 {
		t.Fatalf("expected one valid ledger build, got %+v", builds)
	}
	if builds[0].BuildID != "build-good" || builds[0].Status != "failed" {
		t.Fatalf("unexpected valid ledger build: %+v", builds[0])
	}
}

func TestStartBuildRejectsDuplicateRequestIDWithDifferentPayload(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{
			{
				ID:               "echo",
				Label:            "Echo",
				WorkingDirectory: t.TempDir(),
				Command:          "/bin/echo",
				Arguments:        []string{"hello"},
				TimeoutSeconds:   10,
			},
			{
				ID:               "other",
				Label:            "Other",
				WorkingDirectory: t.TempDir(),
				Command:          "/bin/echo",
				Arguments:        []string{"other"},
				TimeoutSeconds:   10,
			},
		},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"retry-123"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()
	agent.startBuild(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected first status: %d body: %s", response.Code, response.Body.String())
	}

	body = bytes.NewBufferString(`{"job_id":"other","request_id":"retry-123"}`)
	request = httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response = httptest.NewRecorder()
	agent.startBuild(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected duplicate conflict, got %d body: %s", response.Code, response.Body.String())
	}
}

func TestStartBuildRejectsReportURLOutsideConfiguredCIOrigin(t *testing.T) {
	agent := New(Config{
		MachineID:         "machine-123",
		MachineName:       "Mac Studio",
		CIRegistrationURL: "https://ci.example.com/transwarp/register",
		Tunnel:            TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"blocked-report","report_url":"https://other.example.com/transwarp/result","report_token":"report-token"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "is not allowed") {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 {
		t.Fatal("rejected report URL should not start a build")
	}
}

func TestReportBuildPostsTerminalReceipt(t *testing.T) {
	var received BuildReport
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer report-secret" {
			t.Fatalf("missing report auth header: %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("CF-Access-Client-Id") != "access-id" {
			t.Fatalf("missing Cloudflare Access client ID header: %q", request.Header.Get("CF-Access-Client-Id"))
		}
		if request.Header.Get("CF-Access-Client-Secret") != "access-secret" {
			t.Fatalf("missing Cloudflare Access client secret header: %q", request.Header.Get("CF-Access-Client-Secret"))
		}
		body := new(bytes.Buffer)
		if _, err := body.ReadFrom(request.Body); err != nil {
			t.Fatal(err)
		}
		rawBody = body.String()
		if err := json.Unmarshal(body.Bytes(), &received); err != nil {
			t.Fatal(err)
		}
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	started := time.Now().Add(-2 * time.Second)
	ended := time.Now()
	agent := New(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		CIAccessClientID:     "access-id",
		CIAccessClientSecret: "access-secret",
		Tunnel: TunnelConfig{
			PublicURL: "https://transwarp.example.com",
		},
	})

	err := agent.reportBuild(BuildRequest{
		JobID:       "xcode-debug",
		RequestID:   "run-123",
		ReportURL:   server.URL,
		ReportToken: "report-secret",
	}, LedgerRecord{
		JobID:     "xcode-debug",
		RequestID: "run-123",
		StartedAt: started,
		EndedAt:   ended,
		ExitCode:  0,
		Workspace: "/Users/charlie/Library/Caches/Transwarp/Workspaces/private-build",
	})
	if err != nil {
		t.Fatalf("reportBuild returned error: %v", err)
	}

	if received.Status != "passed" {
		t.Fatalf("unexpected status: %s", received.Status)
	}
	if received.Machine != "Mac Studio" {
		t.Fatalf("unexpected machine: %s", received.Machine)
	}
	if received.MachineID != "machine-123" {
		t.Fatalf("unexpected machine id: %s", received.MachineID)
	}
	if received.PublicURL != "https://transwarp.example.com" {
		t.Fatalf("unexpected public URL: %s", received.PublicURL)
	}
	if strings.Contains(rawBody, "workspace") || strings.Contains(rawBody, "private-build") {
		t.Fatalf("report leaked local workspace path: %s", rawBody)
	}
}

func TestReportBuildUsesCIHTTPClient(t *testing.T) {
	var sawReport bool
	original := newCIHTTPClient
	newCIHTTPClient = func(timeout time.Duration) *http.Client {
		if timeout != 15*time.Second {
			t.Fatalf("unexpected CI HTTP timeout: %s", timeout)
		}
		return &http.Client{Transport: agentRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			sawReport = true
			if request.URL.String() != "https://ci.example.com/transwarp/result" {
				t.Fatalf("unexpected report URL: %s", request.URL.String())
			}
			if request.Header.Get("Authorization") != "Bearer report-secret" {
				t.Fatalf("missing report token: %q", request.Header.Get("Authorization"))
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

	agent := New(Config{MachineID: "machine-123", MachineName: "Mac Studio"})
	now := time.Now()
	err := agent.reportBuild(BuildRequest{
		JobID:       "xcode-debug",
		RequestID:   "run-client",
		ReportURL:   "https://ci.example.com/transwarp/result",
		ReportToken: "report-secret",
	}, LedgerRecord{
		JobID:     "xcode-debug",
		RequestID: "run-client",
		StartedAt: now.Add(-time.Second),
		EndedAt:   now,
		ExitCode:  0,
	})
	if err != nil {
		t.Fatalf("reportBuild returned error: %v", err)
	}
	if !sawReport {
		t.Fatal("reportBuild did not use CI HTTP client")
	}
}

func TestReportBuildUsesLiveTunnelPublicURL(t *testing.T) {
	var received BuildReport
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		response.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "quick"},
	})
	agent.tunnel.setStatus(TunnelStatus{
		Mode:      "quick",
		State:     "running",
		PublicURL: "https://quick.trycloudflare.com",
		Connected: true,
	})

	now := time.Now()
	err := agent.reportBuild(BuildRequest{
		JobID:     "xcode-debug",
		RequestID: "run-quick",
		ReportURL: server.URL,
	}, LedgerRecord{
		JobID:     "xcode-debug",
		RequestID: "run-quick",
		StartedAt: now.Add(-time.Second),
		EndedAt:   now,
		ExitCode:  0,
	})
	if err != nil {
		t.Fatalf("reportBuild returned error: %v", err)
	}

	if received.PublicURL != "https://quick.trycloudflare.com" {
		t.Fatalf("unexpected public URL: %s", received.PublicURL)
	}
}

func TestReportBuildRetriesTransientFailures(t *testing.T) {
	withReportRetryDelays(t, []time.Duration{0})

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

	agent := New(Config{MachineID: "machine-123", MachineName: "Mac Studio"})
	now := time.Now()
	err := agent.reportBuild(BuildRequest{
		JobID:     "xcode-debug",
		RequestID: "run-retry",
		ReportURL: server.URL,
	}, LedgerRecord{
		JobID:     "xcode-debug",
		RequestID: "run-retry",
		StartedAt: now.Add(-time.Second),
		EndedAt:   now,
		ExitCode:  0,
	})
	if err != nil {
		t.Fatalf("reportBuild returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
}

func TestReportBuildDoesNotRetryClientErrors(t *testing.T) {
	withReportRetryDelays(t, []time.Duration{0, 0})

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	agent := New(Config{MachineID: "machine-123", MachineName: "Mac Studio"})
	now := time.Now()
	err := agent.reportBuild(BuildRequest{
		JobID:     "xcode-debug",
		RequestID: "run-unauthorized",
		ReportURL: server.URL,
	}, LedgerRecord{
		JobID:     "xcode-debug",
		RequestID: "run-unauthorized",
		StartedAt: now.Add(-time.Second),
		EndedAt:   now,
		ExitCode:  0,
	})
	if err == nil {
		t.Fatal("expected unauthorized report to fail")
	}
	if attempts != 1 {
		t.Fatalf("expected one attempt, got %d", attempts)
	}
}

func TestReportBuildDoesNotFollowRedirects(t *testing.T) {
	withReportRetryDelays(t, []time.Duration{0, 0})

	redirected := make(chan string, 1)
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		redirected <- request.Header.Get("Authorization")
		response.WriteHeader(http.StatusAccepted)
	}))
	defer redirectTarget.Close()

	attempts := 0
	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		http.Redirect(response, request, redirectTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer reportServer.Close()

	agent := New(Config{MachineID: "machine-123", MachineName: "Mac Studio"})
	now := time.Now()
	err := agent.reportBuild(BuildRequest{
		JobID:       "xcode-debug",
		RequestID:   "run-redirect",
		ReportURL:   reportServer.URL,
		ReportToken: "report-secret",
	}, LedgerRecord{
		JobID:     "xcode-debug",
		RequestID: "run-redirect",
		StartedAt: now.Add(-time.Second),
		EndedAt:   now,
		ExitCode:  0,
	})
	if err == nil || !strings.Contains(err.Error(), "307 Temporary Redirect") {
		t.Fatalf("expected redirect report error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected redirect to fail without retry, got %d attempts", attempts)
	}
	select {
	case auth := <-redirected:
		t.Fatalf("report redirect leaked Authorization header %q", auth)
	default:
	}
}

func TestStartBuildReturnsBuildIDAndRetainsLogs(t *testing.T) {
	received := make(chan BuildReport, 1)
	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var report BuildReport
		if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
			t.Fatal(err)
		}
		received <- report
		response.WriteHeader(http.StatusOK)
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		AllowedReportOrigins: []string{reportServer.URL},
		Tunnel: TunnelConfig{
			Mode: "off",
		},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello transwarp"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"run-456","report_url":"` + reportServer.URL + `","report_token":"report-token"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d", response.Code)
	}
	buildID := response.Header().Get("Transwarp-Build-ID")
	if buildID == "" {
		t.Fatal("missing build id header")
	}
	var started BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if started.BuildID != buildID {
		t.Fatalf("unexpected build id: %s", started.BuildID)
	}

	run, ok := agent.build(buildID)
	if !ok {
		t.Fatal("build was not retained")
	}
	waitForRun(t, run)
	events := run.Stream.eventsAfter(0)
	if !eventsContain(events, "hello transwarp") {
		t.Fatalf("retained stream did not include command output: %+v", events)
	}

	report := waitForReport(t, received)
	if report.Status != "passed" {
		t.Fatalf("unexpected report status: %s", report.Status)
	}
	if report.RequestID != "run-456" {
		t.Fatalf("unexpected request id: %s", report.RequestID)
	}
	if report.BuildID != buildID {
		t.Fatalf("unexpected report build id: %s", report.BuildID)
	}
}

func TestReportCallbackHoldsActiveSlotUntilReported(t *testing.T) {
	reportStarted := make(chan struct{}, 1)
	reportFinished := make(chan struct{}, 1)
	releaseReport := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseReport)
		})
	})

	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case reportStarted <- struct{}{}:
		default:
		}
		<-releaseReport
		response.WriteHeader(http.StatusAccepted)
		reportFinished <- struct{}{}
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	first, err := agent.startBuildRun(BuildRequest{JobID: "echo", RequestID: "first", ReportURL: reportServer.URL}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, reportStarted, "report callback did not start")

	if first.Done() {
		t.Fatal("tailed stream should stay open until report callback finishes")
	}
	if status := first.Status(); status.Status != "passed" || status.ReportStatus != "pending" {
		t.Fatalf("expected passed build with pending report, got %+v", status)
	}
	if count := agent.activeBuildCount(); count != 1 {
		t.Fatalf("expected active build slot to be held until report callback finishes, got %d", count)
	}

	second, err := agent.startBuildRun(BuildRequest{JobID: "echo", RequestID: "second"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatalf("second build should be accepted while first report callback is blocked: %v", err)
	}
	if status := second.Status(); status.Status != "queued" {
		t.Fatalf("second build should wait for first report callback, got %+v", status)
	}

	releaseOnce.Do(func() {
		close(releaseReport)
	})
	waitForSignal(t, reportFinished, "report callback did not finish")
	waitForRun(t, first)
	if status := first.Status(); status.ReportStatus != "reported" {
		t.Fatalf("expected reported status, got %+v", status)
	}
	waitForRun(t, second)
	if status := second.Status(); status.Status != "passed" {
		t.Fatalf("expected second build to run after first report callback, got %+v", status)
	}
}

func TestCancelBuildRejectsTerminalBuildWithPendingReport(t *testing.T) {
	reportStarted := make(chan struct{}, 1)
	reportFinished := make(chan struct{}, 1)
	releaseReport := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseReport)
		})
	})

	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case reportStarted <- struct{}{}:
		default:
		}
		<-releaseReport
		response.WriteHeader(http.StatusAccepted)
		reportFinished <- struct{}{}
	}))
	defer reportServer.Close()
	defer releaseOnce.Do(func() {
		close(releaseReport)
	})

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	run, err := agent.startBuildRun(BuildRequest{JobID: "echo", RequestID: "cancel-after-terminal", ReportURL: reportServer.URL}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, reportStarted, "report callback did not start")

	request := httptest.NewRequest(http.MethodPost, "/v1/builds/"+run.ID+"/cancel", nil)
	request.SetPathValue("buildID", run.ID)
	response := httptest.NewRecorder()
	agent.cancelBuild(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("unexpected cancel status: %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "result reporting is still in progress") {
		t.Fatalf("unexpected cancel body: %s", response.Body.String())
	}
	if eventsContain(run.Stream.eventsAfter(0), "cancel requested") {
		t.Fatalf("terminal pending-report build should not record a cancel request: %+v", run.Stream.eventsAfter(0))
	}

	releaseOnce.Do(func() {
		close(releaseReport)
	})
	waitForSignal(t, reportFinished, "report callback did not finish")
	waitForRun(t, run)
	if status := run.Status(); status.Status != "passed" || status.ReportStatus != "reported" {
		t.Fatalf("expected passed reported build, got %+v", status)
	}
}

func TestShutdownBuildsWaitsForTerminalReportWithoutCanceling(t *testing.T) {
	reportStarted := make(chan struct{}, 1)
	reportFinished := make(chan struct{}, 1)
	releaseReport := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseReport)
		})
	})

	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case reportStarted <- struct{}{}:
		default:
		}
		<-releaseReport
		response.WriteHeader(http.StatusAccepted)
		reportFinished <- struct{}{}
	}))
	defer reportServer.Close()
	defer releaseOnce.Do(func() {
		close(releaseReport)
	})

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	run, err := agent.startBuildRun(BuildRequest{JobID: "echo", RequestID: "shutdown-after-terminal", ReportURL: reportServer.URL}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}

	waitForSignal(t, reportStarted, "shutdown report callback did not start")
	shutdownFinished := make(chan struct{})
	go func() {
		agent.shutdownBuilds(2 * time.Second)
		close(shutdownFinished)
	}()

	if run.Closed() {
		t.Fatal("terminal build should stay open until report callback finishes")
	}
	if status := run.Status(); status.Status != "passed" || status.ReportStatus != "pending" {
		t.Fatalf("expected passed build with pending report, got %+v", status)
	}
	if eventsContain(run.Stream.eventsAfter(0), "runner shutting down; canceling build") {
		t.Fatalf("shutdown should not cancel terminal pending-report build: %+v", run.Stream.eventsAfter(0))
	}

	releaseOnce.Do(func() {
		close(releaseReport)
	})
	waitForSignal(t, reportFinished, "shutdown report callback did not finish")
	waitForSignal(t, shutdownFinished, "shutdown did not finish after report callback")
	if status := run.Status(); status.Status != "passed" || status.ReportStatus != "reported" {
		t.Fatalf("expected passed reported build, got %+v", status)
	}
}

func TestStartBuildRedactsSensitiveEnvironmentValues(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "env",
			Label:            "Env",
			WorkingDirectory: t.TempDir(),
			Command:          "/usr/bin/env",
			Environment: map[string]string{
				"API_TOKEN": "super-local-secret",
			},
			TimeoutSeconds: 10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"env","request_id":"redaction"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)
	var started BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	run, ok := agent.build(started.BuildID)
	if !ok {
		t.Fatal("build was not retained")
	}
	waitForRun(t, run)
	events := run.Stream.eventsAfter(0)
	messages := eventMessages(events)

	if strings.Contains(messages, "super-local-secret") {
		t.Fatalf("stream leaked secret: %s", messages)
	}
	if !strings.Contains(messages, "API_TOKEN=[redacted]") {
		t.Fatalf("stream did not include redacted token: %s", messages)
	}
}

func TestStartBuildRedactsShortSensitiveEnvironmentAssignments(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "env",
			Label:            "Env",
			WorkingDirectory: t.TempDir(),
			Command:          "/usr/bin/env",
			Environment: map[string]string{
				"API_TOKEN": "abc",
			},
			TimeoutSeconds: 10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"env","request_id":"short-redaction"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)
	var started BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	run, ok := agent.build(started.BuildID)
	if !ok {
		t.Fatal("build was not retained")
	}
	waitForRun(t, run)
	messages := eventMessages(run.Stream.eventsAfter(0))

	if strings.Contains(messages, "API_TOKEN=abc") {
		t.Fatalf("stream leaked short sensitive assignment: %s", messages)
	}
	if !strings.Contains(messages, "API_TOKEN=[redacted]") {
		t.Fatalf("stream did not redact short sensitive assignment: %s", messages)
	}
}

func TestStartBuildRedactsResolvedKeychainEnvironmentValues(t *testing.T) {
	original := lookupKeychainSecret
	lookupKeychainSecret = func(service string, account string) (string, error) {
		return "resolved-build-secret", nil
	}
	defer func() {
		lookupKeychainSecret = original
	}()

	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "env",
			"label": "Env",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/env",
			"environment": {
				"MATCH_PASSWORD": "keychain://co.charliewil.transwarp/machine-123/jobs/env/MATCH_PASSWORD"
			},
			"timeout_seconds": 10
		}]
	}`)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	agent := New(config)

	body := bytes.NewBufferString(`{"job_id":"env","request_id":"keychain-redaction"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)
	var started BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	run, ok := agent.build(started.BuildID)
	if !ok {
		t.Fatal("build was not retained")
	}
	waitForRun(t, run)
	messages := eventMessages(run.Stream.eventsAfter(0))

	if strings.Contains(messages, "resolved-build-secret") {
		t.Fatalf("stream leaked resolved keychain secret: %s", messages)
	}
	if !strings.Contains(messages, "MATCH_PASSWORD=[redacted]") {
		t.Fatalf("stream did not redact resolved keychain secret: %s", messages)
	}
}

func TestStartBuildRedactsExplicitValues(t *testing.T) {
	agent := New(Config{
		MachineID:      "machine-123",
		MachineName:    "Mac Studio",
		RedactedValues: []string{"literal-secret"},
		Tunnel:         TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"literal-secret"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"explicit-redaction"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)
	var started BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	run, ok := agent.build(started.BuildID)
	if !ok {
		t.Fatal("build was not retained")
	}
	waitForRun(t, run)
	messages := eventMessages(run.Stream.eventsAfter(0))

	if strings.Contains(messages, "literal-secret") {
		t.Fatalf("stream leaked explicit redaction value: %s", messages)
	}
	if !strings.Contains(messages, "[redacted]") {
		t.Fatalf("stream did not redact explicit value: %s", messages)
	}
}

func TestStartBuildRedactsReportTokenFromBuildStream(t *testing.T) {
	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusAccepted)
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		AllowedReportOrigins: []string{reportServer.URL},
		Tunnel:               TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"callback=report-secret"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"report-token-redaction","report_url":"` + reportServer.URL + `","report_token":"report-secret"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)
	var started BuildStartResponse
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	run, ok := agent.build(started.BuildID)
	if !ok {
		t.Fatal("build was not retained")
	}
	waitForRun(t, run)
	messages := eventMessages(run.Stream.eventsAfter(0))

	if strings.Contains(messages, "report-secret") {
		t.Fatalf("stream leaked report token: %s", messages)
	}
	if !strings.Contains(messages, "callback=[redacted]") {
		t.Fatalf("stream did not redact report token: %s", messages)
	}
}

func TestStartBuildRejectsReportURLQueryBeforeQueueing(t *testing.T) {
	agent := New(Config{
		MachineID:            "machine-123",
		MachineName:          "Mac Studio",
		AllowedReportOrigins: []string{"https://ci.example.com"},
		Tunnel:               TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello"},
			TimeoutSeconds:   10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"echo","request_id":"report-query-rejected","report_url":"https://ci.example.com/transwarp/result?token=query-secret","report_token":"report-secret"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "query or fragment") {
		t.Fatalf("expected query or fragment error, got %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("invalid report_url should not queue a build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestRedactorRedactsCheckoutAuthorizationHeader(t *testing.T) {
	redactor := NewRedactor(Config{
		Jobs: []JobConfig{{
			ID:                          "checkout",
			CheckoutAuthorizationHeader: "Authorization: Bearer local-token",
		}},
	})

	redacted := redactor.Redact("git failed with Authorization: Bearer local-token")

	if strings.Contains(redacted, "local-token") {
		t.Fatalf("checkout authorization header leaked: %s", redacted)
	}
	if !strings.Contains(redacted, "[redacted]") {
		t.Fatalf("checkout authorization header was not redacted: %s", redacted)
	}
}

func TestRedactorRedactsRunnerAccessCredentials(t *testing.T) {
	redactor := NewRedactor(Config{
		RunnerAccessClientID:     "runner-access-id",
		RunnerAccessClientSecret: "runner-access-secret",
	})

	redacted := redactor.Redact("CF-Access-Client-Id=runner-access-id CF-Access-Client-Secret=runner-access-secret")

	if strings.Contains(redacted, "runner-access-id") || strings.Contains(redacted, "runner-access-secret") {
		t.Fatalf("runner Access credentials leaked: %s", redacted)
	}
	if !strings.Contains(redacted, "[redacted]") {
		t.Fatalf("runner Access credentials were not redacted: %s", redacted)
	}
}

func TestRedactorRedactsQuotedSensitiveEnvironmentAssignments(t *testing.T) {
	redactor := NewRedactor(Config{
		Jobs: []JobConfig{{
			ID: "env-dump",
			Environment: map[string]string{
				"API_TOKEN": "abc",
			},
		}},
	})

	redacted := redactor.Redact(`{"API_TOKEN":"abc","MODE":"debug"} 'API_TOKEN': 'abc' API_TOKEN=abc`)

	if strings.Contains(redacted, "abc") {
		t.Fatalf("short sensitive assignment leaked: %s", redacted)
	}
	for _, expected := range []string{
		`"API_TOKEN":"[redacted]"`,
		`'API_TOKEN': '[redacted]'`,
		`API_TOKEN=[redacted]`,
		`"MODE":"debug"`,
	} {
		if !strings.Contains(redacted, expected) {
			t.Fatalf("redacted output missing %q: %s", expected, redacted)
		}
	}
}

func TestRedactorRedactsSigningAndCredentialEnvironmentValues(t *testing.T) {
	redactor := NewRedactor(Config{
		Jobs: []JobConfig{{
			ID: "codesign",
			Environment: map[string]string{
				"SIGN_IDENTITY":          "Developer ID Application: Example (TEAMID)",
				"APPLE_KEYCHAIN_PROFILE": "transwarp-notary-profile",
				"CERTIFICATE_P12":        "local-certificate-p12",
				"API_CREDENTIAL":         "service-credential",
				"NOTARY_PASSPHRASE":      "notary-passphrase",
				"DEVELOPER_DIR":          "/Applications/Xcode.app/Contents/Developer",
			},
		}},
	})

	redacted := redactor.Redact("identity=Developer ID Application: Example (TEAMID) profile=transwarp-notary-profile p12=local-certificate-p12 credential=service-credential passphrase=notary-passphrase developer=/Applications/Xcode.app/Contents/Developer")

	for _, secret := range []string{
		"Developer ID Application: Example (TEAMID)",
		"transwarp-notary-profile",
		"local-certificate-p12",
		"service-credential",
		"notary-passphrase",
	} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("sensitive environment value %q leaked: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "/Applications/Xcode.app/Contents/Developer") {
		t.Fatalf("non-sensitive environment value was redacted: %s", redacted)
	}
}

func TestReportRequestRedactorMasksCallbackSecrets(t *testing.T) {
	redactor := reportRequestRedactor(Redactor{}, BuildRequest{
		ReportURL:   "https://ci-user:ci-pass@ci.example.com/transwarp/result?token=query-secret&attempt=1",
		ReportToken: "report-secret",
	})

	redacted := redactor.Redact("POST https://ci-user:ci-pass@ci.example.com/transwarp/result?token=query-secret failed with Bearer report-secret")

	for _, secret := range []string{"ci-user", "ci-pass", "query-secret", "report-secret"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("report request secret %q leaked: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "ci.example.com") {
		t.Fatalf("redaction removed useful report host context: %s", redacted)
	}
}

func TestBuildPathHandlersRejectUnsafeBuildID(t *testing.T) {
	agent := New(Config{})
	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		method  string
		path    string
	}{
		{name: "status", handler: agent.buildStatus, method: http.MethodGet, path: "/v1/builds/build%2F123"},
		{name: "logs", handler: agent.tailBuildLogs, method: http.MethodGet, path: "/v1/builds/build%2F123/logs"},
		{name: "cancel", handler: agent.cancelBuild, method: http.MethodPost, path: "/v1/builds/build%2F123/cancel"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.SetPathValue("buildID", "build/123")
			response := httptest.NewRecorder()

			test.handler(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected bad request, got %d: %s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "build_id") {
				t.Fatalf("expected build_id validation error, got %s", response.Body.String())
			}
		})
	}
}

func TestTailBuildLogsReplaysAfterSequence(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "printf",
			Label:            "Print",
			WorkingDirectory: t.TempDir(),
			Command:          "/usr/bin/printf",
			Arguments:        []string{"one\ntwo\n"},
			TimeoutSeconds:   10,
		}},
	})
	run, err := agent.startBuildRun(BuildRequest{JobID: "printf", RequestID: "tail"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, run)

	request := httptest.NewRequest(http.MethodGet, "/v1/builds/"+run.ID+"/logs?after=1&follow=false", nil)
	request.SetPathValue("buildID", run.ID)
	response := httptest.NewRecorder()
	agent.tailBuildLogs(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "starting Print") {
		t.Fatalf("tail replay included skipped event: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "one") || !strings.Contains(response.Body.String(), "two") {
		t.Fatalf("tail replay missed logs: %s", response.Body.String())
	}
}

func TestStatusIncludesRecentBuilds(t *testing.T) {
	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		ListenAddress: "127.0.0.1:8188",
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello status"},
			TimeoutSeconds:   10,
		}},
	})

	run, err := agent.startBuildRun(BuildRequest{JobID: "echo", RequestID: "status-run"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, run)

	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	response := httptest.NewRecorder()
	agent.status(response, request)

	rawStatus := response.Body.String()
	if strings.Contains(rawStatus, "workspace") || strings.Contains(rawStatus, agent.config.Jobs[0].WorkingDirectory) {
		t.Fatalf("status leaked local workspace path: %s", rawStatus)
	}

	var status StatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.RecentBuilds) != 1 {
		t.Fatalf("expected one recent build, got %+v", status.RecentBuilds)
	}
	if status.RecentBuilds[0].BuildID != run.ID {
		t.Fatalf("unexpected build id: %s", status.RecentBuilds[0].BuildID)
	}
	if status.RecentBuilds[0].Status != "passed" {
		t.Fatalf("unexpected status: %s", status.RecentBuilds[0].Status)
	}
	if status.RecentBuilds[0].Result == nil || status.RecentBuilds[0].Result.ExitCode != 0 {
		t.Fatalf("missing result: %+v", status.RecentBuilds[0].Result)
	}
}

func TestBuildLogStreamIncludesReportFailure(t *testing.T) {
	withReportRetryDelays(t, nil)
	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, "ci unavailable", http.StatusInternalServerError)
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		ListenAddress: "127.0.0.1:8188",
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello status"},
			TimeoutSeconds:   10,
		}},
	})

	run, err := agent.startBuildRun(BuildRequest{JobID: "echo", RequestID: "report-failure", ReportURL: reportServer.URL}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, run)

	request := httptest.NewRequest(http.MethodGet, "/v1/builds/"+run.ID+"/logs?after=0&follow=false", nil)
	request.SetPathValue("buildID", run.ID)
	response := httptest.NewRecorder()
	agent.tailBuildLogs(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"message":"passed"`) {
		t.Fatalf("tail missed build result: %s", body)
	}
	if !strings.Contains(body, `"message":"report failed: report endpoint returned 500 Internal Server Error`) {
		t.Fatalf("tail missed report failure: %s", body)
	}
	status := run.Status()
	if status.ReportStatus != "failed" {
		t.Fatalf("expected failed report status, got %+v", status)
	}
	if !strings.Contains(status.ReportError, "500 Internal Server Error") {
		t.Fatalf("expected report error in status, got %+v", status)
	}
}

func TestReportFailureRedactsCallbackSecretsFromLogsAndStatus(t *testing.T) {
	withReportRetryDelays(t, nil)
	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, "callback rejected report-secret with ci-access-secret", http.StatusInternalServerError)
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:                "machine-123",
		MachineName:              "Mac Studio",
		CIAccessClientID:         "ci-access-id",
		CIAccessClientSecret:     "ci-access-secret",
		ListenAddress:            "127.0.0.1:8188",
		Tunnel:                   TunnelConfig{Mode: "off"},
		AllowedReportOrigins:     []string{reportServer.URL},
		RegistrationToken:        "registration-token",
		RunnerAccessClientID:     "runner-access-id",
		RunnerAccessClientSecret: "runner-access-secret",
		Jobs: []JobConfig{{
			ID:               "echo",
			Label:            "Echo",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/echo",
			Arguments:        []string{"hello status"},
			TimeoutSeconds:   10,
		}},
	})

	run, err := agent.startBuildRun(BuildRequest{
		JobID:       "echo",
		RequestID:   "report-failure-redaction",
		ReportURL:   reportServer.URL,
		ReportToken: "report-secret",
	}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, run)

	request := httptest.NewRequest(http.MethodGet, "/v1/builds/"+run.ID+"/logs?after=0&follow=false", nil)
	request.SetPathValue("buildID", run.ID)
	response := httptest.NewRecorder()
	agent.tailBuildLogs(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	body := response.Body.String()
	status := run.Status()
	combined := body + "\n" + status.ReportError
	for _, secret := range []string{"report-secret", "ci-access-secret"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("report failure leaked %q: %s", secret, combined)
		}
	}
	if !strings.Contains(combined, "[redacted]") || !strings.Contains(combined, "500 Internal Server Error") {
		t.Fatalf("report failure lost useful redacted context: %s", combined)
	}
}

func TestStatusIncludesQueuedBuilds(t *testing.T) {
	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		ListenAddress: "127.0.0.1:8188",
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	first, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "first"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer waitForRun(t, first)
	defer first.cancel()

	second, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "second"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	response := httptest.NewRecorder()
	agent.status(response, request)

	var status StatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.ActiveBuilds != 1 {
		t.Fatalf("expected one active build, got %d", status.ActiveBuilds)
	}
	if status.QueuedBuilds != 1 {
		t.Fatalf("expected one queued build, got %d", status.QueuedBuilds)
	}
	if status.QueuedBuildLimit != maxQueuedBuilds {
		t.Fatalf("expected queued build limit %d, got %d", maxQueuedBuilds, status.QueuedBuildLimit)
	}
	if len(status.RecentBuilds) != 2 {
		t.Fatalf("expected two recent builds, got %+v", status.RecentBuilds)
	}
	if status.RecentBuilds[0].BuildID != second.ID || status.RecentBuilds[0].Status != "queued" {
		t.Fatalf("expected latest build to be queued second build, got %+v", status.RecentBuilds[0])
	}

	agent.cancelBuildRun(second, "test cleanup")
}

func TestCancelBuildStopsRunningJob(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	run, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "cancel"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/builds/"+run.ID+"/cancel", nil)
	request.SetPathValue("buildID", run.ID)
	response := httptest.NewRecorder()
	agent.cancelBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected cancel status: %d", response.Code)
	}
	waitForRun(t, run)
	status := run.Status()
	if status.Status != "canceled" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestShutdownBuildsCancelsAndReportsRunningJob(t *testing.T) {
	received := make(chan BuildReport, 1)
	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var report BuildReport
		if err := json.NewDecoder(request.Body).Decode(&report); err != nil {
			t.Fatal(err)
		}
		received <- report
		response.WriteHeader(http.StatusAccepted)
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	run, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "shutdown-cancel", ReportURL: reportServer.URL}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}

	agent.shutdownBuilds(2 * time.Second)
	waitForClosed(t, run)

	status := run.Status()
	if status.Status != "canceled" {
		t.Fatalf("unexpected status: %+v", status)
	}
	report := waitForReport(t, received)
	if report.Status != "canceled" {
		t.Fatalf("unexpected report status: %+v", report)
	}
	if report.RequestID != "shutdown-cancel" {
		t.Fatalf("unexpected report request id: %s", report.RequestID)
	}
}

func TestShutdownBuildsWaitsForReportCallbackBeforeClosingRunningBuild(t *testing.T) {
	reportStarted := make(chan struct{}, 1)
	reportFinished := make(chan struct{}, 1)
	releaseReport := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseReport)
		})
	})

	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case reportStarted <- struct{}{}:
		default:
		}
		<-releaseReport
		response.WriteHeader(http.StatusAccepted)
		reportFinished <- struct{}{}
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	run, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "shutdown-callback", ReportURL: reportServer.URL}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}

	shutdownFinished := make(chan struct{})
	go func() {
		agent.shutdownBuilds(2 * time.Second)
		close(shutdownFinished)
	}()

	waitForSignal(t, reportStarted, "shutdown report callback did not start")
	if run.Closed() {
		t.Fatal("running build should stay open until shutdown report callback finishes")
	}
	if status := run.Status(); status.Status != "canceled" || status.ReportStatus != "pending" {
		t.Fatalf("expected canceled build with pending report, got %+v", status)
	}

	releaseOnce.Do(func() {
		close(releaseReport)
	})
	waitForSignal(t, reportFinished, "shutdown report callback did not finish")
	waitForSignal(t, shutdownFinished, "shutdown did not finish after report callback")
	if status := run.Status(); status.ReportStatus != "reported" {
		t.Fatalf("expected reported status, got %+v", status)
	}
}

func TestShutdownBuildsWaitsForReportCallbackBeforeClosingQueuedBuild(t *testing.T) {
	reportStarted := make(chan struct{}, 1)
	reportFinished := make(chan struct{}, 1)
	releaseReport := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseReport)
		})
	})

	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case reportStarted <- struct{}{}:
		default:
		}
		<-releaseReport
		response.WriteHeader(http.StatusAccepted)
		reportFinished <- struct{}{}
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	first, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "shutdown-active"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "shutdown-queued", ReportURL: reportServer.URL}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}

	shutdownFinished := make(chan struct{})
	go func() {
		agent.shutdownBuilds(2 * time.Second)
		close(shutdownFinished)
	}()

	waitForSignal(t, reportStarted, "shutdown queued report callback did not start")
	if second.Closed() {
		t.Fatal("queued build should stay open until shutdown report callback finishes")
	}
	if status := second.Status(); status.Status != "canceled" || status.ReportStatus != "pending" {
		t.Fatalf("expected canceled queued build with pending report, got %+v", status)
	}

	releaseOnce.Do(func() {
		close(releaseReport)
	})
	waitForSignal(t, reportFinished, "shutdown queued report callback did not finish")
	waitForSignal(t, shutdownFinished, "shutdown did not finish after queued report callback")
	waitForClosed(t, first)
	if second.Started() {
		t.Fatal("shutdown-canceled queued build should not start")
	}
	if status := second.Status(); status.ReportStatus != "reported" {
		t.Fatalf("expected reported status, got %+v", status)
	}
}

func TestCancelBuildStopsChildProcesses(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "spawn-child")
	content := "#!/bin/sh\n/bin/sleep 30 &\nprintf '%s\\n' \"$!\" > " + shellQuote(pidPath) + "\nwait\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "spawn-child",
			Label:            "Spawn Child",
			WorkingDirectory: dir,
			Command:          script,
			TimeoutSeconds:   10,
		}},
	})
	run, err := agent.startBuildRun(BuildRequest{JobID: "spawn-child", RequestID: "cancel-child"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}

	childPID := waitForPIDFile(t, pidPath)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds/"+run.ID+"/cancel", nil)
	request.SetPathValue("buildID", run.ID)
	response := httptest.NewRecorder()
	agent.cancelBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected cancel status: %d", response.Code)
	}
	waitForRun(t, run)
	waitForProcessExit(t, childPID)
}

func TestConcurrentBuildsQueueAndRunInOrder(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"1"},
			TimeoutSeconds:   10,
		}},
	})
	run, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "first"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}

	second, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "second"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatalf("second build should queue: %v", err)
	}

	if second.Status().Status != "queued" {
		t.Fatalf("expected second build to be queued, got %+v", second.Status())
	}
	if count := agent.queuedBuildCount(); count != 1 {
		t.Fatalf("expected one queued build, got %d", count)
	}

	waitForRun(t, run)
	waitForRun(t, second)
	if second.Status().Status != "passed" {
		t.Fatalf("expected queued build to pass after first, got %+v", second.Status())
	}
	if !second.Started() {
		t.Fatal("expected queued build to start after first completed")
	}
}

func TestConcurrentBuildsRejectWhenQueueIsFull(t *testing.T) {
	withMaxQueuedBuilds(t, 1)

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	first, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "first"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer waitForRun(t, first)
	defer first.cancel()

	second, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "second"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatalf("second build should fill queue: %v", err)
	}
	if second.Status().Status != "queued" {
		t.Fatalf("expected second build to be queued, got %+v", second.Status())
	}

	third, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "third"}, agent.config.Jobs[0])
	if err == nil {
		t.Fatalf("expected third build to be rejected, got %+v", third.Status())
	}
	if !strings.Contains(err.Error(), "build queue is full (1 queued)") {
		t.Fatalf("expected queue full error, got %v", err)
	}
	if count := agent.queuedBuildCount(); count != 1 {
		t.Fatalf("expected queue to stay at limit, got %d", count)
	}
}

func TestConcurrentBuildsReturnExistingRequestWhenQueueIsFull(t *testing.T) {
	withMaxQueuedBuilds(t, 1)

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	first, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "first"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer waitForRun(t, first)
	defer first.cancel()

	secondRequest := BuildRequest{JobID: "sleep", RequestID: "second"}
	second, err := agent.startBuildRun(secondRequest, agent.config.Jobs[0])
	if err != nil {
		t.Fatalf("second build should fill queue: %v", err)
	}

	retried, err := agent.startBuildRun(secondRequest, agent.config.Jobs[0])
	if err != nil {
		t.Fatalf("duplicate request should return existing queued build: %v", err)
	}
	if retried.ID != second.ID {
		t.Fatalf("expected duplicate request to return %s, got %s", second.ID, retried.ID)
	}
	if count := agent.queuedBuildCount(); count != 1 {
		t.Fatalf("expected duplicate request not to grow queue, got %d", count)
	}
}

func TestCancelQueuedBuildRemovesItFromQueue(t *testing.T) {
	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	first, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "first"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer waitForRun(t, first)
	defer first.cancel()

	second, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "second"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatalf("second build should queue: %v", err)
	}
	if second.Status().Status != "queued" {
		t.Fatalf("expected queued status, got %+v", second.Status())
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/builds/"+second.ID+"/cancel", nil)
	request.SetPathValue("buildID", second.ID)
	response := httptest.NewRecorder()
	agent.cancelBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected cancel status: %d", response.Code)
	}
	waitForClosed(t, second)
	if second.Started() {
		t.Fatal("canceled queued build should not start")
	}
	if second.Status().Status != "canceled" {
		t.Fatalf("expected queued build to be canceled, got %+v", second.Status())
	}
	if count := agent.queuedBuildCount(); count != 0 {
		t.Fatalf("expected empty queue, got %d", count)
	}
}

func TestCancelQueuedBuildWaitsForReportCallbackBeforeClosingStream(t *testing.T) {
	reportStarted := make(chan struct{}, 1)
	reportFinished := make(chan struct{}, 1)
	releaseReport := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseReport)
		})
	})

	reportServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case reportStarted <- struct{}{}:
		default:
		}
		<-releaseReport
		response.WriteHeader(http.StatusAccepted)
		reportFinished <- struct{}{}
	}))
	defer reportServer.Close()

	agent := New(Config{
		MachineID:   "machine-123",
		MachineName: "Mac Studio",
		Tunnel:      TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:               "sleep",
			Label:            "Sleep",
			WorkingDirectory: t.TempDir(),
			Command:          "/bin/sleep",
			Arguments:        []string{"5"},
			TimeoutSeconds:   10,
		}},
	})
	first, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "first"}, agent.config.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer waitForRun(t, first)
	defer first.cancel()

	second, err := agent.startBuildRun(BuildRequest{JobID: "sleep", RequestID: "second", ReportURL: reportServer.URL}, agent.config.Jobs[0])
	if err != nil {
		t.Fatalf("second build should queue: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/builds/"+second.ID+"/cancel", nil)
	request.SetPathValue("buildID", second.ID)
	response := httptest.NewRecorder()
	agent.cancelBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected cancel status: %d", response.Code)
	}
	waitForSignal(t, reportStarted, "queued cancel report callback did not start")
	if second.Done() {
		t.Fatal("tailed stream should stay open until queued cancel report callback finishes")
	}
	if status := second.Status(); status.Status != "canceled" || status.ReportStatus != "pending" {
		t.Fatalf("expected canceled build with pending report, got %+v", status)
	}

	releaseOnce.Do(func() {
		close(releaseReport)
	})
	waitForSignal(t, reportFinished, "queued cancel report callback did not finish")
	waitForClosed(t, second)
	if status := second.Status(); status.ReportStatus != "reported" {
		t.Fatalf("expected reported status, got %+v", status)
	}
}

func waitForRun(t *testing.T, run *BuildRun) {
	t.Helper()

	select {
	case <-run.done:
	case <-time.After(5 * time.Second):
		t.Fatal("build did not finish")
	}
}

func environmentMap(values []string) map[string]string {
	environment := map[string]string{}
	for _, value := range values {
		key, value, ok := strings.Cut(value, "=")
		if ok {
			environment[key] = value
		}
	}
	return environment
}

func waitForClosed(t *testing.T, run *BuildRun) {
	t.Helper()

	select {
	case <-run.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("build did not close")
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("invalid pid file: %s", data)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("pid file was not written")
	return 0
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d still exists", pid)
}

func waitForReport(t *testing.T, reports <-chan BuildReport) BuildReport {
	t.Helper()

	select {
	case report := <-reports:
		return report
	case <-time.After(5 * time.Second):
		t.Fatal("report was not delivered")
		return BuildReport{}
	}
}

func waitForSignal(t *testing.T, signals <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-signals:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}

func eventsContain(events []Event, message string) bool {
	return strings.Contains(eventMessages(events), message)
}

func eventMessages(events []Event) string {
	var builder strings.Builder
	for _, event := range events {
		builder.WriteString(event.Message)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func withReportRetryDelays(t *testing.T, delays []time.Duration) {
	t.Helper()

	original := reportRetryDelays
	reportRetryDelays = delays
	t.Cleanup(func() {
		reportRetryDelays = original
	})
}

func withMaxQueuedBuilds(t *testing.T, limit int) {
	t.Helper()

	original := maxQueuedBuilds
	maxQueuedBuilds = limit
	t.Cleanup(func() {
		maxQueuedBuilds = original
	})
}

type agentRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn agentRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

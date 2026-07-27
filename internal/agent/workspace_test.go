package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutJobClonesRequestedRepository(t *testing.T) {
	source := createGitRepository(t)
	workspaceRoot := t.TempDir()

	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		WorkspaceRoot: workspaceRoot,
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:                  "pwd",
			Label:               "Print Workspace",
			Checkout:            true,
			AllowedRepositories: []string{source},
			Command:             "/bin/cat",
			Arguments:           []string{"README.md"},
			TimeoutSeconds:      30,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"pwd","request_id":"checkout-smoke","repo_url":"` + source + `","ref":"main"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d body: %s", response.Code, response.Body.String())
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
	messages := eventMessages(run.Stream.eventsAfter(0))
	if !strings.Contains(messages, "hello checkout") {
		t.Fatalf("checkout output missing: %s", messages)
	}

	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace was not cleaned up: %d entries remain", len(entries))
	}
}

func TestCheckoutJobFetchesRequestedPullRef(t *testing.T) {
	source, _ := createGitRepositoryWithPullRef(t)
	workspaceRoot := t.TempDir()

	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		WorkspaceRoot: workspaceRoot,
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:                  "pull-ref",
			Label:               "Pull Ref",
			Checkout:            true,
			AllowedRepositories: []string{source},
			Command:             "/bin/cat",
			Arguments:           []string{"README.md"},
			TimeoutSeconds:      30,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"pull-ref","request_id":"checkout-pull-ref","repo_url":"` + source + `","ref":"refs/pull/123/merge"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d body: %s", response.Code, response.Body.String())
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
	messages := eventMessages(run.Stream.eventsAfter(0))
	if !strings.Contains(messages, "fetching refs/pull/123/merge") {
		t.Fatalf("checkout did not fetch requested pull ref: %s", messages)
	}
	if !strings.Contains(messages, "hello pull ref") {
		t.Fatalf("pull ref output missing: %s", messages)
	}

	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace was not cleaned up: %d entries remain", len(entries))
	}
}

func TestCheckoutJobFetchesRefBeforeCheckingOutCommit(t *testing.T) {
	source, commit := createGitRepositoryWithPullRef(t)

	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		WorkspaceRoot: t.TempDir(),
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:                  "pull-commit",
			Label:               "Pull Commit",
			Checkout:            true,
			AllowedRepositories: []string{source},
			Command:             "/bin/cat",
			Arguments:           []string{"README.md"},
			TimeoutSeconds:      30,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"pull-commit","request_id":"checkout-pull-commit","repo_url":"` + source + `","ref":"refs/pull/123/merge","commit":"` + commit + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d body: %s", response.Code, response.Body.String())
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
	messages := eventMessages(run.Stream.eventsAfter(0))
	if !strings.Contains(messages, "fetching refs/pull/123/merge") {
		t.Fatalf("checkout did not fetch requested pull ref: %s", messages)
	}
	if !strings.Contains(messages, "checking out "+commit) {
		t.Fatalf("checkout did not detach to requested commit: %s", messages)
	}
	if !strings.Contains(messages, "hello pull ref") {
		t.Fatalf("pull commit output missing: %s", messages)
	}
}

func TestCheckoutJobAcceptsMaxLengthIdentifiers(t *testing.T) {
	source := createGitRepository(t)
	workspaceRoot := t.TempDir()
	jobID := strings.Repeat("j", 128)
	requestID := strings.Repeat("r", 256)

	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		WorkspaceRoot: workspaceRoot,
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:                  jobID,
			Checkout:            true,
			AllowedRepositories: []string{source},
			Command:             "/bin/cat",
			Arguments:           []string{"README.md"},
			TimeoutSeconds:      30,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"` + jobID + `","request_id":"` + requestID + `","repo_url":"` + source + `","ref":"main"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d body: %s", response.Code, response.Body.String())
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
	messages := eventMessages(run.Stream.eventsAfter(0))
	if !strings.Contains(messages, "hello checkout") {
		t.Fatalf("checkout output missing: %s", messages)
	}

	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace was not cleaned up: %d entries remain", len(entries))
	}
}

func TestCheckoutJobRejectsUnallowedRepository(t *testing.T) {
	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		WorkspaceRoot: t.TempDir(),
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:                  "build",
			Checkout:            true,
			AllowedRepositories: []string{"https://example.com/allowed.git"},
			Command:             "/bin/echo",
			Arguments:           []string{"nope"},
			TimeoutSeconds:      10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"build","request_id":"bad-repo","repo_url":"https://example.com/other.git"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "not allowed") {
		t.Fatalf("expected rejection body, got: %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("invalid checkout should not queue a build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestCheckoutJobRequiresRevisionBeforeQueueing(t *testing.T) {
	source := createGitRepository(t)
	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		WorkspaceRoot: t.TempDir(),
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:                  "build",
			Checkout:            true,
			AllowedRepositories: []string{source},
			Command:             "/bin/echo",
			Arguments:           []string{"nope"},
			TimeoutSeconds:      10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"build","request_id":"missing-revision","repo_url":"` + source + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "ref or commit") {
		t.Fatalf("expected revision rejection body, got: %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("invalid checkout should not queue a build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestCheckoutJobRejectsRepositoryCredentialsBeforeQueueing(t *testing.T) {
	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		WorkspaceRoot: t.TempDir(),
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:                  "build",
			Checkout:            true,
			AllowedRepositories: []string{"https://github.com/example/app.git"},
			Command:             "/bin/echo",
			Arguments:           []string{"should not run"},
			TimeoutSeconds:      10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"build","request_id":"bad-repo-credentials","repo_url":"https://token:secret@github.com/example/app.git"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "repo_url must not include credentials") {
		t.Fatalf("expected credential rejection, got: %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("credential-bearing repo should not queue a build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestCheckoutJobRejectsUnsafeTargetBeforeGitCheckout(t *testing.T) {
	source := createGitRepository(t)
	agent := New(Config{
		MachineID:     "machine-123",
		MachineName:   "Mac Studio",
		WorkspaceRoot: t.TempDir(),
		Tunnel:        TunnelConfig{Mode: "off"},
		Jobs: []JobConfig{{
			ID:                  "build",
			Checkout:            true,
			AllowedRepositories: []string{source},
			Command:             "/bin/echo",
			Arguments:           []string{"should not run"},
			TimeoutSeconds:      10,
		}},
	})

	body := bytes.NewBufferString(`{"job_id":"build","request_id":"bad-target","repo_url":"` + source + `","ref":"--help"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", body)
	response := httptest.NewRecorder()

	agent.startBuild(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "checkout target must not start with '-'") {
		t.Fatalf("expected unsafe target rejection, got: %s", response.Body.String())
	}
	if agent.activeBuildCount() != 0 || agent.queuedBuildCount() != 0 {
		t.Fatalf("unsafe target should not queue a build; active=%d queued=%d", agent.activeBuildCount(), agent.queuedBuildCount())
	}
}

func TestCheckoutTargetValidationAllowsCommonRefsAndCommits(t *testing.T) {
	for _, target := range []string{
		"main",
		"release/1.0",
		"refs/heads/main",
		"refs/tags/v1.0.0",
		"refs/pull/123/merge",
		"0123456789abcdef0123456789abcdef01234567",
	} {
		if err := validateCheckoutTarget(target); err != nil {
			t.Fatalf("expected %q to be allowed: %v", target, err)
		}
	}
}

func TestCheckoutTargetValidationRejectsUnsafeTargets(t *testing.T) {
	for _, target := range []string{
		"--help",
		"refs/heads/main with-space",
		"refs/heads/main..next",
		"refs/heads/main.lock",
		"refs/heads/main@{1}",
		"refs/heads/main:other",
		"refs/heads/main~1",
		"refs/heads/main^",
		"refs/heads/main\nnext",
	} {
		if err := validateCheckoutTarget(target); err == nil {
			t.Fatalf("expected %q to be rejected", target)
		}
	}
}

func TestSafeWorkspacePrefixIsBoundedForValidIdentifierLimits(t *testing.T) {
	prefix := safeWorkspacePrefix(strings.Repeat("j", 128), strings.Repeat("r", 256))

	if len(prefix) > maxWorkspacePrefixLength {
		t.Fatalf("workspace prefix is too long: %d", len(prefix))
	}
	if !strings.HasPrefix(prefix, "jjjj") {
		t.Fatalf("workspace prefix lost job context: %q", prefix)
	}
	if !strings.HasSuffix(prefix, "-") {
		t.Fatalf("workspace prefix should leave a separator before random suffix: %q", prefix)
	}
}

func TestGitEnvironmentDoesNotInheritArbitraryVariables(t *testing.T) {
	t.Setenv("TRANSWARP_SHOULD_NOT_REACH_GIT", "secret")

	env := strings.Join(gitEnvironment(JobConfig{}, ""), "\n")

	if strings.Contains(env, "TRANSWARP_SHOULD_NOT_REACH_GIT") {
		t.Fatalf("git environment leaked process variable: %s", env)
	}
	if !strings.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("git environment should disable terminal prompts: %s", env)
	}
	if !strings.Contains(env, "GIT_ASKPASS=/bin/false") {
		t.Fatalf("git environment should disable askpass: %s", env)
	}
	if !strings.Contains(env, "SSH_ASKPASS=/bin/false") {
		t.Fatalf("git environment should disable ssh askpass: %s", env)
	}
	if !strings.Contains(env, "GIT_CONFIG_NOSYSTEM=1") {
		t.Fatalf("git environment should ignore system config: %s", env)
	}
	if !strings.Contains(env, "GIT_CONFIG_GLOBAL=/dev/null") {
		t.Fatalf("git environment should ignore user global config: %s", env)
	}
	if !strings.Contains(env, "GIT_CONFIG_SYSTEM=/dev/null") {
		t.Fatalf("git environment should ignore explicit system config: %s", env)
	}
	if !strings.Contains(env, "PATH=") {
		t.Fatalf("git environment should include PATH: %s", env)
	}
}

func TestGitEnvironmentInjectsCheckoutAuthorizationHeaderThroughGitConfig(t *testing.T) {
	env := strings.Join(gitEnvironment(
		JobConfig{CheckoutAuthorizationHeader: "Authorization: Bearer local-token"},
		"https://github.com/example/app.git",
	), "\n")

	if !strings.Contains(env, "GIT_CONFIG_COUNT=1") {
		t.Fatalf("git environment should enable one config entry: %s", env)
	}
	if !strings.Contains(env, "GIT_CONFIG_KEY_0=http.https://github.com/example/app.git.extraHeader") {
		t.Fatalf("git environment should scope extraHeader to repo URL: %s", env)
	}
	if !strings.Contains(env, "GIT_CONFIG_VALUE_0=Authorization: Bearer local-token") {
		t.Fatalf("git environment should include checkout auth header: %s", env)
	}
}

func createGitRepository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGitCommand(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello checkout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, dir, "add", "README.md")
	runGitCommand(t, dir, "-c", "user.name=Transwarp", "-c", "user.email=transwarp@example.com", "commit", "-m", "initial")
	return dir
}

func createGitRepositoryWithPullRef(t *testing.T) (string, string) {
	t.Helper()

	work := t.TempDir()
	runGitCommand(t, work, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, work, "add", "README.md")
	runGitCommand(t, work, "-c", "user.name=Transwarp", "-c", "user.email=transwarp@example.com", "commit", "-m", "initial")
	runGitCommand(t, work, "checkout", "-b", "pull-ref")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello pull ref\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, work, "add", "README.md")
	runGitCommand(t, work, "-c", "user.name=Transwarp", "-c", "user.email=transwarp@example.com", "commit", "-m", "pull ref")
	commit := gitOutput(t, work, "rev-parse", "HEAD")

	bare := filepath.Join(t.TempDir(), "source.git")
	runGitCommand(t, work, "init", "--bare", bare)
	runGitCommand(t, work, "remote", "add", "origin", bare)
	runGitCommand(t, work, "push", "origin", "main")
	runGitCommand(t, work, "push", "origin", "HEAD:refs/pull/123/merge")
	return bare, commit
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("/usr/bin/git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := exec.Command("/usr/bin/git", args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}

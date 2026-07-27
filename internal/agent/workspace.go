package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charliewilco/transwarp/internal/requestmeta"
)

type PreparedWorkspace struct {
	Path    string
	Cleanup func()
}

const maxWorkspacePrefixLength = 96

func (agent *Agent) prepareWorkspace(ctx context.Context, request BuildRequest, job JobConfig, stream *buildStream) (PreparedWorkspace, error) {
	if !job.Checkout {
		return PreparedWorkspace{Path: job.WorkingDirectory, Cleanup: func() {}}, nil
	}

	if strings.TrimSpace(request.RepoURL) == "" {
		return PreparedWorkspace{}, errors.New("repo_url is required for checkout jobs")
	}
	if !repositoryAllowed(request.RepoURL, job.AllowedRepositories) {
		return PreparedWorkspace{}, fmt.Errorf("repo_url %q is not allowed for job %q", request.RepoURL, job.ID)
	}
	if strings.TrimSpace(request.Ref) == "" && strings.TrimSpace(request.Commit) == "" {
		return PreparedWorkspace{}, errors.New("ref or commit is required for checkout jobs")
	}
	fetchTarget := checkoutFetchTarget(request)
	checkoutTarget := checkoutTarget(request)
	if err := validateCheckoutTarget(fetchTarget); err != nil {
		return PreparedWorkspace{}, err
	}
	if err := validateCheckoutTarget(checkoutTarget); err != nil {
		return PreparedWorkspace{}, err
	}

	root, err := agent.workspaceRoot()
	if err != nil {
		return PreparedWorkspace{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return PreparedWorkspace{}, fmt.Errorf("create workspace root: %w", err)
	}

	dir, err := os.MkdirTemp(root, safeWorkspacePrefix(job.ID, request.RequestID))
	if err != nil {
		return PreparedWorkspace{}, fmt.Errorf("create workspace: %w", err)
	}

	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			Emit(Event{Kind: "error", Message: "workspace cleanup failed: " + err.Error(), JobID: job.ID})
		}
	}

	stream.write(Event{Kind: "build", Message: "cloning " + request.RepoURL, JobID: job.ID})
	if err := runGit(ctx, dir, job, request.RepoURL, "clone", "--", request.RepoURL, "."); err != nil {
		cleanup()
		return PreparedWorkspace{}, err
	}

	if fetchTarget != "" {
		stream.write(Event{Kind: "build", Message: "fetching " + fetchTarget, JobID: job.ID})
		if err := runGit(ctx, dir, job, request.RepoURL, "fetch", "--depth=1", "origin", fetchTarget); err != nil {
			cleanup()
			return PreparedWorkspace{}, err
		}
	}

	if checkoutTarget != "" {
		target := checkoutTarget
		if request.Commit == "" && fetchTarget != "" {
			target = "FETCH_HEAD"
		}
		stream.write(Event{Kind: "build", Message: "checking out " + checkoutTarget, JobID: job.ID})
		if err := runGit(ctx, dir, job, request.RepoURL, "checkout", "--detach", target); err != nil {
			cleanup()
			return PreparedWorkspace{}, err
		}
	}

	return PreparedWorkspace{Path: dir, Cleanup: cleanup}, nil
}

func (agent *Agent) workspaceRoot() (string, error) {
	if strings.TrimSpace(agent.config.WorkspaceRoot) != "" {
		return agent.config.WorkspaceRoot, nil
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("workspace root unavailable: %w", err)
	}
	return filepath.Join(cache, "Transwarp", "Workspaces"), nil
}

func repositoryAllowed(repoURL string, allowed []string) bool {
	for _, candidate := range allowed {
		if repoURL == candidate {
			return true
		}
	}
	return false
}

func checkoutTarget(request BuildRequest) string {
	if strings.TrimSpace(request.Commit) != "" {
		return request.Commit
	}
	return strings.TrimSpace(request.Ref)
}

func checkoutFetchTarget(request BuildRequest) string {
	if strings.TrimSpace(request.Ref) != "" {
		return strings.TrimSpace(request.Ref)
	}
	return strings.TrimSpace(request.Commit)
}

func validateCheckoutTarget(target string) error {
	return requestmeta.ValidateCheckoutTarget(target)
}

func runGit(ctx context.Context, dir string, job JobConfig, repoURL string, args ...string) error {
	command := exec.CommandContext(ctx, "/usr/bin/git", args...)
	command.Dir = dir
	command.Env = gitEnvironment(job, repoURL)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("git %s failed: %s", strings.Join(args, " "), message)
	}
	return nil
}

func gitEnvironment(job JobConfig, repoURL string) []string {
	environment := []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin",
		"TMPDIR=" + os.TempDir(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=/bin/false",
		"SSH_ASKPASS=/bin/false",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	}

	if header := strings.TrimSpace(job.CheckoutAuthorizationHeader); header != "" {
		environment = append(environment,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0="+gitExtraHeaderKey(repoURL),
			"GIT_CONFIG_VALUE_0="+header,
		)
	}

	return environment
}

func gitExtraHeaderKey(repoURL string) string {
	if strings.TrimSpace(repoURL) == "" {
		return "http.extraHeader"
	}
	return "http." + repoURL + ".extraHeader"
}

func safeWorkspacePrefix(jobID string, requestID string) string {
	value := jobID + "-" + requestID + "-"
	if strings.Trim(value, "-") == "" {
		value = fmt.Sprintf("job-%d-", time.Now().UnixNano())
	}

	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			builder.WriteRune(char)
			continue
		}
		builder.WriteRune('-')
	}
	prefix := builder.String()
	if len(prefix) <= maxWorkspacePrefixLength {
		return prefix
	}
	prefix = strings.TrimRight(prefix[:maxWorkspacePrefixLength-1], "-_") + "-"
	if strings.Trim(prefix, "-_") == "" {
		return "job-"
	}
	return prefix
}

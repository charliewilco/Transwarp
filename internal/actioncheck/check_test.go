package actioncheck

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckRepositoryGitHubActions(t *testing.T) {
	if err := Check(repositoryRoot(t)); err != nil {
		t.Fatal(err)
	}
}

func TestCompositeRunScriptExtractsActionScript(t *testing.T) {
	script, err := CompositeRunScript(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"cmd/transwarp-diagnose", "cmd/transwarp-dispatch", "normalize_boolean"} {
		if !strings.Contains(script, marker) {
			t.Fatalf("expected composite script to contain %q", marker)
		}
	}
}

func TestCheckRejectsMissingActionInput(t *testing.T) {
	root := copyGitHubActionFixtures(t)
	actionPath := filepath.Join(root, "action.yml")
	data, err := os.ReadFile(actionPath)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(data), "  timeout:\n", "  timeout-missing:\n", 1)
	if modified == string(data) {
		t.Fatal("test fixture did not contain timeout input")
	}
	if err := os.WriteFile(actionPath, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Check(root)
	if err == nil || !strings.Contains(err.Error(), "action.yml missing input timeout") {
		t.Fatalf("expected missing timeout input error, got %v", err)
	}
}

func TestCheckRejectsMissingTerminalResultOutput(t *testing.T) {
	for _, output := range []string{"status", "exit-code", "error"} {
		t.Run(output, func(t *testing.T) {
			root := copyGitHubActionFixtures(t)
			actionPath := filepath.Join(root, "action.yml")
			data, err := os.ReadFile(actionPath)
			if err != nil {
				t.Fatal(err)
			}
			block := "  " + output + ":\n"
			modified := strings.Replace(string(data), block, "  "+output+"-missing:\n", 1)
			if modified == string(data) {
				t.Fatalf("test fixture did not contain %s output", output)
			}
			if err := os.WriteFile(actionPath, []byte(modified), 0o600); err != nil {
				t.Fatal(err)
			}

			err = Check(root)
			if err == nil || !strings.Contains(err.Error(), "action.yml missing output "+output) {
				t.Fatalf("expected missing %s output error, got %v", output, err)
			}
		})
	}
}

func TestCheckRejectsRequestIDDescriptionWithoutGitHubJobDefault(t *testing.T) {
	root := copyGitHubActionFixtures(t)
	actionPath := filepath.Join(root, "action.yml")
	data, err := os.ReadFile(actionPath)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(
		string(data),
		"Defaults to github.run_id-github.run_attempt-github.job when starting a dispatch; required for coordinator cancel or result lookup.",
		"Defaults to the current workflow attempt.",
		1,
	)
	if modified == string(data) {
		t.Fatal("test fixture did not contain request-id default description")
	}
	if err := os.WriteFile(actionPath, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Check(root)
	if err == nil || !strings.Contains(err.Error(), "request-id input description") {
		t.Fatalf("expected request-id description error, got %v", err)
	}
}

func TestCheckRejectsReleaseEvidenceWorkflowWithoutAppLaunchMode(t *testing.T) {
	root := copyGitHubActionFixtures(t)
	workflowPath := filepath.Join(root, "examples", "github-actions-release-evidence.yml")
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(data), "          TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE: app\n", "", 1)
	if modified == string(data) {
		t.Fatal("test fixture did not contain named tunnel launch mode")
	}
	if err := os.WriteFile(workflowPath, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Check(root)
	if err == nil || !strings.Contains(err.Error(), "TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE") {
		t.Fatalf("expected missing launch mode error, got %v", err)
	}
}

func TestCheckRejectsReleaseEvidenceWorkflowWithoutInputPreflight(t *testing.T) {
	root := copyGitHubActionFixtures(t)
	workflowPath := filepath.Join(root, "examples", "github-actions-release-evidence.yml")
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(data), "        run: go run ./cmd/transwarp-audit -check-release-collection-inputs\n", "        run: echo missing preflight\n", 1)
	if modified == string(data) {
		t.Fatal("test fixture did not contain release input preflight")
	}
	if err := os.WriteFile(workflowPath, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Check(root)
	if err == nil || !strings.Contains(err.Error(), "release evidence workflow must preflight release inputs") {
		t.Fatalf("expected missing release preflight error, got %v", err)
	}
}

func TestCheckRejectsNamedTunnelCoordinatorSmokeWithoutPublicURLRequirement(t *testing.T) {
	root := copyGitHubActionFixtures(t)
	scriptPath := filepath.Join(root, "scripts", "smoke-cloudflare-named-coordinator.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(data), "\t-require-public-url \\\n", "", 1)
	if modified == string(data) {
		t.Fatal("test fixture did not contain require-public-url flag")
	}
	if err := os.WriteFile(scriptPath, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Check(root)
	if err == nil || !strings.Contains(err.Error(), "named tunnel coordinator smoke must require public runner URLs") {
		t.Fatalf("expected missing public URL requirement error, got %v", err)
	}
}

func copyGitHubActionFixtures(t *testing.T) string {
	t.Helper()
	sourceRoot := repositoryRoot(t)
	destinationRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(destinationRoot, "examples"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(destinationRoot, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths := []string{"action.yml"}
	examples, err := filepath.Glob(filepath.Join(sourceRoot, "examples", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range examples {
		paths = append(paths, filepath.Join("examples", filepath.Base(example)))
	}
	paths = append(paths, filepath.Join("scripts", "smoke-cloudflare-named-coordinator.sh"))
	for _, path := range paths {
		source := filepath.Join(sourceRoot, path)
		destination := filepath.Join(destinationRoot, path)
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return destinationRoot
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

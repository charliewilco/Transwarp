package actioncheck

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func Check(root string) error {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	files := []string{filepath.Join(root, "action.yml")}
	examples, err := filepath.Glob(filepath.Join(root, "examples", "*.yml"))
	if err != nil {
		return err
	}
	sort.Strings(examples)
	files = append(files, examples...)
	for _, path := range files {
		if _, err := readYAML(path); err != nil {
			return err
		}
	}

	action, err := readYAML(filepath.Join(root, "action.yml"))
	if err != nil {
		return err
	}
	if value(action, "runs", "using") != "composite" {
		return errors.New("action.yml must be a composite action")
	}
	inputs, ok := mapValue(action, "inputs")
	if !ok {
		return errors.New("action.yml must define inputs")
	}
	for _, input := range []string{"job", "mode", "version", "diagnose", "allow-http", "cancel", "result", "tail", "url", "token", "coordinator-url", "coordinator-token", "access-client-id", "access-client-secret", "request-id", "build-id", "repo-url", "ref", "commit", "checkout-metadata", "machine-id", "report-url", "report-token", "min-cpu-count", "min-memory-bytes", "min-xcode-version", "require-public-url", "timeout"} {
		if _, ok := inputs[input]; !ok {
			return fmt.Errorf("action.yml missing input %s", input)
		}
	}
	requestID, ok := mapValueAny(inputs["request-id"])
	if !ok || !strings.Contains(value(requestID, "description"), "github.job") || !strings.Contains(value(requestID, "description"), "required for coordinator cancel or result lookup") {
		return errors.New("action.yml request-id input description must document defaults and explicit lookup requirements")
	}
	outputs, ok := mapValue(action, "outputs")
	if !ok {
		return errors.New("action.yml must expose dispatch outputs")
	}
	for _, output := range []string{"request-id", "build-id", "job-id", "machine-id", "public-url", "repo-url", "ref", "commit", "status", "exit-code", "error"} {
		if _, ok := outputs[output]; !ok {
			return fmt.Errorf("action.yml missing output %s", output)
		}
	}
	steps, ok := sequenceValue(action, "runs", "steps")
	if !ok || len(steps) == 0 {
		return errors.New("action.yml must define at least one step")
	}
	dispatchStep, ok := findStepWithRun(steps, "cmd/transwarp-dispatch")
	if !ok || value(dispatchStep, "id") != "dispatch" {
		return errors.New("action.yml dispatch step must have id dispatch")
	}
	actionScript := joinedRunScripts(steps)
	for _, assertion := range []scriptAssertion{
		{"action.yml must run transwarp-diagnose", []string{"cmd/transwarp-diagnose"}},
		{"action.yml must run transwarp-dispatch", []string{"cmd/transwarp-dispatch"}},
		{"action.yml must validate boolean inputs", []string{"normalize_boolean diagnose", "normalize_boolean allow-http", "normalize_boolean cancel", "normalize_boolean result", "normalize_boolean tail", "normalize_boolean checkout-metadata", "normalize_boolean require-public-url", "must be true, 1, yes, false, 0, or no"}},
		{"action.yml must support local-only jobs without checkout metadata", []string{"INPUT_CHECKOUT_METADATA", "TRANSWARP_REPO_URL=\"\"", "TRANSWARP_REF=\"\"", "TRANSWARP_COMMIT=\"\""}},
		{"action.yml must validate numeric constraint inputs", []string{"require_unsigned_integer min-cpu-count", "require_unsigned_integer min-memory-bytes"}},
		{"action.yml must validate stable identifier inputs", []string{"require_stable_identifier request-id", "require_stable_identifier job", "require_stable_identifier build-id", "require_stable_identifier machine-id"}},
		{"action.yml must validate report callback pairs", []string{"report-token is required when report-url is set", "report-url is required when report-token is set"}},
		{"action.yml must reject report callbacks outside direct mode", []string{"report-url and report-token are only supported in direct mode"}},
		{"action.yml must validate cancel inputs", []string{"build-id is required when canceling a direct runner build", "request-id is required when canceling a coordinator dispatch"}},
		{"action.yml must validate tail inputs", []string{"tail is only supported in direct mode", "build-id is required when tailing a direct runner build"}},
		{"action.yml must validate result inputs", []string{"result is only supported in coordinator mode", "cancel and result cannot both be true", "tail and result cannot both be true", "request-id is required when querying a coordinator result", "-result"}},
	} {
		if !assertion.matches(actionScript) {
			return errors.New(assertion.message)
		}
	}

	if err := checkDirectExample(root); err != nil {
		return err
	}
	if err := checkCoordinatorExample(root); err != nil {
		return err
	}
	if err := checkSelfHostedExample(root); err != nil {
		return err
	}
	if err := checkReleaseEvidenceExample(root); err != nil {
		return err
	}
	if err := checkNamedTunnelCoordinatorSmoke(root); err != nil {
		return err
	}
	return nil
}

func CompositeRunScript(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	action, err := readYAML(filepath.Join(root, "action.yml"))
	if err != nil {
		return "", err
	}
	steps, ok := sequenceValue(action, "runs", "steps")
	if !ok || len(steps) == 0 {
		return "", errors.New("action.yml must define at least one step")
	}
	for _, step := range steps {
		if run := value(step, "run"); run != "" {
			return run, nil
		}
	}
	return "", errors.New("action.yml missing composite run script")
}

type scriptAssertion struct {
	message string
	markers []string
}

func (assertion scriptAssertion) matches(script string) bool {
	for _, marker := range assertion.markers {
		if !strings.Contains(script, marker) {
			return false
		}
	}
	return true
}

func checkDirectExample(root string) error {
	steps, err := workflowSteps(filepath.Join(root, "examples", "github-actions.yml"))
	if err != nil {
		return err
	}
	step, ok := findStepUsing(steps, "charliewilco/transwarp@main")
	if !ok {
		return errors.New("direct example must use the Transwarp action")
	}
	if value(step, "id") != "transwarp" {
		return errors.New("direct example must assign a stable Transwarp step id")
	}
	with, ok := mapValue(step, "with")
	if !ok {
		return errors.New("direct example must configure Transwarp inputs")
	}
	for _, input := range []string{"job", "tail", "report-url", "report-token"} {
		if _, ok := with[input]; !ok {
			return fmt.Errorf("direct example must set %s", input)
		}
	}
	script := joinedRunScripts(steps)
	for _, marker := range []string{"### Transwarp dispatch", "$GITHUB_STEP_SUMMARY", "steps.transwarp.outputs[", "job-id", "status", "exit-code", "error"} {
		if !strings.Contains(script, marker) {
			return errors.New("direct example must summarize dispatch result")
		}
	}
	return nil
}

func checkCoordinatorExample(root string) error {
	steps, err := workflowSteps(filepath.Join(root, "examples", "github-actions-coordinator.yml"))
	if err != nil {
		return err
	}
	step, ok := findStepUsing(steps, "charliewilco/transwarp@main")
	if !ok {
		return errors.New("coordinator example must use the Transwarp action")
	}
	if value(step, "id") != "transwarp" {
		return errors.New("coordinator example must assign a stable Transwarp step id")
	}
	with, ok := mapValue(step, "with")
	if !ok {
		return errors.New("coordinator example must configure Transwarp inputs")
	}
	if with["mode"] != "coordinator" {
		return errors.New("coordinator example must use coordinator mode")
	}
	if _, ok := with["token"]; ok {
		return errors.New("coordinator example must not pass the runner token to GitHub Actions")
	}
	if _, ok := with["result"]; !ok {
		return errors.New("coordinator example must expose result lookup")
	}
	script := joinedRunScripts(steps)
	for _, marker := range []string{"### Transwarp dispatch", "$GITHUB_STEP_SUMMARY", "steps.transwarp.outputs[", "job-id", "machine-id", "public-url", "status", "exit-code", "error"} {
		if !strings.Contains(script, marker) {
			return errors.New("coordinator example must summarize selected runner")
		}
	}
	return nil
}

func checkSelfHostedExample(root string) error {
	steps, err := workflowSteps(filepath.Join(root, "examples", "github-actions-self-hosted.yml"))
	if err != nil {
		return err
	}
	if !strings.Contains(joinedRunScripts(steps), "./scripts/check-self-hosted-mac.sh") {
		return errors.New("self-hosted example must verify the Mac before building")
	}
	if _, ok := findStepUsing(steps, "actions/setup-go@v5"); !ok {
		return errors.New("self-hosted example must set up Go for receipt writing")
	}
	upload, ok := findStepUsing(steps, "actions/upload-artifact@v4")
	if !ok {
		return errors.New("self-hosted example must upload readiness evidence")
	}
	uploadWith, ok := mapValue(upload, "with")
	if !ok {
		return errors.New("self-hosted upload step must configure artifact inputs")
	}
	uploadPath := fmt.Sprint(uploadWith["path"])
	if !strings.Contains(uploadPath, "evidence/self-hosted-mac.json") {
		return errors.New("self-hosted example must upload readiness JSON")
	}
	if !strings.Contains(uploadPath, "evidence/self-hosted-readiness.log") {
		return errors.New("self-hosted example must upload readiness source log")
	}
	for _, step := range steps {
		env, ok := mapValue(step, "env")
		if ok {
			if _, exists := env["TRANSWARP_SELF_HOSTED_EVIDENCE"]; exists {
				return nil
			}
		}
	}
	return errors.New("self-hosted example must write TRANSWARP_SELF_HOSTED_EVIDENCE")
}

func checkReleaseEvidenceExample(root string) error {
	workflow, err := readYAML(filepath.Join(root, "examples", "github-actions-release-evidence.yml"))
	if err != nil {
		return err
	}
	trigger, ok := mapValueAny(workflow["on"])
	if !ok {
		trigger, ok = mapValueAny(workflow[true])
	}
	if !ok {
		return errors.New("release evidence workflow must define workflow_dispatch")
	}
	dispatch, ok := mapValueAny(trigger["workflow_dispatch"])
	if !ok {
		return errors.New("release evidence workflow must define workflow_dispatch")
	}
	inputs, ok := mapValueAny(dispatch["inputs"])
	if !ok {
		return errors.New("release evidence workflow must define workflow_dispatch inputs")
	}
	if collect, ok := mapValueAny(inputs["collect-named-tunnel"]); !ok || collect["default"] != "true" {
		return errors.New("release evidence workflow must expose collect-named-tunnel input")
	}
	for _, input := range []string{"named-tunnel-evidence", "ci-dispatch-evidence", "clean-mac-evidence"} {
		if _, ok := inputs[input]; !ok {
			return fmt.Errorf("release evidence workflow must expose %s input", input)
		}
	}
	steps, err := workflowSteps(filepath.Join(root, "examples", "github-actions-release-evidence.yml"))
	if err != nil {
		return err
	}
	releaseScript := joinedRunScripts(steps)
	if !strings.Contains(releaseScript, "./scripts/collect-release-evidence.sh") {
		return errors.New("release evidence workflow must collect release evidence")
	}
	if !strings.Contains(releaseScript, "go run ./cmd/transwarp-audit -check-release-collection-inputs") {
		return errors.New("release evidence workflow must preflight release inputs")
	}
	if !strings.Contains(releaseScript, "go run ./cmd/transwarp-audit -summary -allow-incomplete -report .build/release-evidence/transwarp-audit.json") {
		return errors.New("release evidence workflow must show Go audit summary")
	}
	if !stepsAnyEnvKey(steps, "TRANSWARP_SELF_HOSTED_EVIDENCE") {
		return errors.New("release evidence workflow must write self-hosted evidence")
	}
	preflightStep, ok := findStepWithRun(steps, "go run ./cmd/transwarp-audit -check-release-collection-inputs")
	if !ok {
		return errors.New("release evidence workflow must configure release-input preflight step")
	}
	preflightEnv, ok := mapValue(preflightStep, "env")
	if !ok {
		return errors.New("release evidence workflow must configure release-input preflight env")
	}
	collectStep, ok := findStepWithRun(steps, "./scripts/collect-release-evidence.sh")
	if !ok {
		return errors.New("release evidence workflow must configure collect-release-evidence step")
	}
	releaseEnv, ok := mapValue(collectStep, "env")
	if !ok {
		return errors.New("release evidence workflow must configure collect-release-evidence env")
	}
	for key, expected := range map[string]string{
		"TRANSWARP_COLLECT_NAMED_TUNNEL":     "${{ inputs.collect-named-tunnel }}",
		"TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE": "app",
		"TRANSWARP_NAMED_TUNNEL_EVIDENCE":    "${{ inputs.named-tunnel-evidence }}",
		"TRANSWARP_CI_DISPATCH_EVIDENCE":     "${{ inputs.ci-dispatch-evidence }}",
		"TRANSWARP_NOTARIZE_REQUESTED":       "${{ secrets.TRANSWARP_NOTARIZE }}",
		"TRANSWARP_CLEAN_MAC_EVIDENCE":       "${{ inputs.clean-mac-evidence }}",
		"SIGN_IDENTITY":                      "${{ secrets.TRANSWARP_SIGN_IDENTITY }}",
		"APPLE_KEYCHAIN_PROFILE":             "${{ secrets.APPLE_KEYCHAIN_PROFILE }}",
	} {
		if preflightEnv[key] != expected {
			return fmt.Errorf("release evidence workflow preflight must wire %s", key)
		}
		if releaseEnv[key] != expected {
			return fmt.Errorf("release evidence workflow must wire %s", key)
		}
	}
	for _, key := range []string{"APPLE_ID", "APPLE_TEAM_ID", "APPLE_APP_SPECIFIC_PASSWORD"} {
		if _, ok := preflightEnv[key]; ok {
			return fmt.Errorf("release evidence workflow preflight must not wire %s", key)
		}
		if _, ok := releaseEnv[key]; ok {
			return fmt.Errorf("release evidence workflow must not wire %s", key)
		}
	}
	uploadStep, ok := findStepUsing(steps, "actions/upload-artifact@v4")
	if !ok {
		return errors.New("release evidence workflow must upload an artifact")
	}
	uploadWith, ok := mapValue(uploadStep, "with")
	if !ok {
		return errors.New("release evidence upload step must configure artifact inputs")
	}
	uploadPath := fmt.Sprint(uploadWith["path"])
	if !strings.Contains(uploadPath, ".build/Transwarp-release.zip") {
		return errors.New("release evidence workflow must upload Transwarp-release.zip")
	}
	if !strings.Contains(uploadPath, ".build/release-evidence/") {
		return errors.New("release evidence workflow must upload release evidence directory")
	}
	return nil
}

func checkNamedTunnelCoordinatorSmoke(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "scripts", "smoke-cloudflare-named-coordinator.sh"))
	if err != nil {
		return err
	}
	script := string(data)
	for _, marker := range []string{"$DISPATCH_BIN", "-coordinator-url", "-require-public-url", "-request-id \"$REQUEST_ID\""} {
		if !strings.Contains(script, marker) {
			return errors.New("named tunnel coordinator smoke must require public runner URLs during dispatch")
		}
	}
	return nil
}

func readYAML(path string) (map[any]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document map[any]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return document, nil
}

func workflowSteps(path string) ([]map[any]any, error) {
	workflow, err := readYAML(path)
	if err != nil {
		return nil, err
	}
	jobs, ok := mapValue(workflow, "jobs")
	if !ok {
		return nil, fmt.Errorf("%s must define jobs", path)
	}
	steps := []map[any]any{}
	for _, value := range jobs {
		job, ok := mapValueAny(value)
		if !ok {
			return nil, fmt.Errorf("%s job must be a mapping", path)
		}
		jobSteps, ok := sequenceValue(job, "steps")
		if !ok {
			return nil, fmt.Errorf("%s job must define steps", path)
		}
		steps = append(steps, jobSteps...)
	}
	return steps, nil
}

func joinedRunScripts(steps []map[any]any) string {
	runs := make([]string, 0, len(steps))
	for _, step := range steps {
		if run := value(step, "run"); run != "" {
			runs = append(runs, run)
		}
	}
	return strings.Join(runs, "\n")
}

func findStepUsing(steps []map[any]any, uses string) (map[any]any, bool) {
	for _, step := range steps {
		if value(step, "uses") == uses {
			return step, true
		}
	}
	return nil, false
}

func findStepWithRun(steps []map[any]any, marker string) (map[any]any, bool) {
	for _, step := range steps {
		if strings.Contains(value(step, "run"), marker) {
			return step, true
		}
	}
	return nil, false
}

func stepsAnyEnvKey(steps []map[any]any, key string) bool {
	for _, step := range steps {
		env, ok := mapValue(step, "env")
		if ok {
			if _, exists := env[key]; exists {
				return true
			}
		}
	}
	return false
}

func value(document map[any]any, keys ...string) string {
	current := any(document)
	for _, key := range keys {
		mapping, ok := mapValueAny(current)
		if !ok {
			return ""
		}
		current = mapping[key]
	}
	if current == nil {
		return ""
	}
	return fmt.Sprint(current)
}

func mapValue(document map[any]any, key string) (map[any]any, bool) {
	return mapValueAny(document[key])
}

func mapValueAny(value any) (map[any]any, bool) {
	switch mapping := value.(type) {
	case map[any]any:
		return mapping, true
	case map[string]any:
		normalized := make(map[any]any, len(mapping))
		for key, value := range mapping {
			normalized[key] = value
		}
		return normalized, true
	default:
		return nil, false
	}
}

func sequenceValue(document map[any]any, keys ...string) ([]map[any]any, bool) {
	current := any(document)
	for _, key := range keys {
		mapping, ok := mapValueAny(current)
		if !ok {
			return nil, false
		}
		current = mapping[key]
	}
	values, ok := current.([]any)
	if !ok {
		return nil, false
	}
	steps := make([]map[any]any, 0, len(values))
	for _, value := range values {
		step, ok := mapValueAny(value)
		if !ok {
			return nil, false
		}
		steps = append(steps, step)
	}
	return steps, true
}

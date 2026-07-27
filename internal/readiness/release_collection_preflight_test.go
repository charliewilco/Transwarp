package readiness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReleaseCollectionPreflightAcceptsLocalIncompleteCollection(t *testing.T) {
	err := ValidateReleaseCollectionPreflight(ReleaseCollectionPreflightOptions{
		AllowIncomplete:    "1",
		CollectNamedTunnel: "0",
		NotarizeRequested:  "0",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateReleaseCollectionPreflightRejectsMissingStrictCloudflaredPolicy(t *testing.T) {
	err := ValidateReleaseCollectionPreflight(ReleaseCollectionPreflightOptions{
		AllowIncomplete:    "0",
		CollectNamedTunnel: "0",
		NotarizeRequested:  "0",
	})
	if err == nil || !strings.Contains(err.Error(), "TRANSWARP_EXPECTED_CLOUDFLARED_VERSION is required") {
		t.Fatalf("expected cloudflared policy error, got %v", err)
	}
}

func TestValidateReleaseCollectionPreflightRejectsMissingStrictNamedTunnelEvidence(t *testing.T) {
	err := ValidateReleaseCollectionPreflight(strictReleasePreflightOptions(t))
	if err == nil || !strings.Contains(err.Error(), "named-tunnel evidence is required") {
		t.Fatalf("expected named-tunnel evidence error, got %v", err)
	}
}

func TestValidateReleaseCollectionPreflightRejectsMissingStrictNotaryKeychainProfile(t *testing.T) {
	options := strictReleasePreflightOptions(t)
	options.AppleKeychainProfile = ""

	err := ValidateReleaseCollectionPreflight(options)
	if err == nil || !strings.Contains(err.Error(), "APPLE_KEYCHAIN_PROFILE is required for strict release notarization") {
		t.Fatalf("expected notary Keychain profile error, got %v", err)
	}
}

func TestValidateReleaseCollectionPreflightRejectsMissingStrictCIDispatchEvidence(t *testing.T) {
	dir := t.TempDir()
	namedEvidence := filepath.Join(dir, "named-tunnel-evidence.json")
	if err := os.WriteFile(namedEvidence, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := strictReleasePreflightOptions(t)
	options.NamedTunnelEvidencePath = namedEvidence

	err := ValidateReleaseCollectionPreflight(options)
	if err == nil || !strings.Contains(err.Error(), "CI dispatch evidence is required") {
		t.Fatalf("expected CI dispatch evidence error, got %v", err)
	}
}

func TestValidateReleaseCollectionPreflightRejectsInvalidGitHubRunnerForGeneratedCIReceipt(t *testing.T) {
	options := strictReleasePreflightOptions(t)
	options.CollectNamedTunnel = "1"
	options.CloudflareTunnelTokenSet = true
	options.PublicURL = "https://transwarp.example.com"
	options.GitHub = GitHubActionsEvidenceContext{
		GitHubActions: true,
		RunID:         "1234",
		RunAttempt:    "1",
		Workflow:      "Release Evidence",
		Job:           "release-evidence",
		Repository:    "charliewilco/transwarp",
		SHA:           "0123456789abcdef0123456789abcdef01234567",
		RunnerOS:      "Linux",
		RunnerArch:    "X64",
	}

	err := ValidateReleaseCollectionPreflight(options)
	if err == nil || !strings.Contains(err.Error(), "RUNNER_OS=macOS") {
		t.Fatalf("expected runner_os error, got %v", err)
	}
}

func TestValidateReleaseCollectionPreflightRejectsPartialAccessCredentials(t *testing.T) {
	err := ValidateReleaseCollectionPreflight(ReleaseCollectionPreflightOptions{
		AllowIncomplete:    "1",
		CollectNamedTunnel: "0",
		NotarizeRequested:  "0",
		AccessClientIDSet:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "TRANSWARP_ACCESS_CLIENT_ID and TRANSWARP_ACCESS_CLIENT_SECRET") {
		t.Fatalf("expected Access credential pair error, got %v", err)
	}
}

func TestReleaseCollectionPreflightFromEnvTreatsTokenValuesAsPresenceOnly(t *testing.T) {
	env := map[string]string{
		"TRANSWARP_COLLECT_ALLOW_INCOMPLETE": "yes",
		"TRANSWARP_COLLECT_NAMED_TUNNEL":     "auto",
		"TRANSWARP_NOTARIZE_REQUESTED":       "no",
		"TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN":  "secret-token",
		"TRANSWARP_PUBLIC_URL":               "https://transwarp.example.com",
		"TRANSWARP_ACCESS_CLIENT_ID":         "access-id",
		"TRANSWARP_ACCESS_CLIENT_SECRET":     "access-secret",
		"APPLE_KEYCHAIN_PROFILE":             "transwarp-notary",
	}

	options := ReleaseCollectionPreflightFromEnv(func(key string) string {
		return env[key]
	})
	if !options.CloudflareTunnelTokenSet {
		t.Fatal("expected tunnel token presence")
	}
	if !options.AccessClientIDSet || !options.AccessClientSecretSet {
		t.Fatal("expected Access credential presence")
	}
	if options.AppleKeychainProfile != "transwarp-notary" {
		t.Fatal("expected notary Keychain profile")
	}
	if err := ValidateReleaseCollectionPreflight(options); err != nil {
		t.Fatal(err)
	}
}

func strictReleasePreflightOptions(t *testing.T) ReleaseCollectionPreflightOptions {
	t.Helper()
	return ReleaseCollectionPreflightOptions{
		AllowIncomplete:            "0",
		CollectNamedTunnel:         "0",
		NotarizeRequested:          "1",
		ExpectedCloudflaredVersion: "cloudflared version smoke",
		SignIdentity:               "Developer ID Application: Smoke (TEAMID)",
		AppleKeychainProfile:       "smoke-profile",
	}
}

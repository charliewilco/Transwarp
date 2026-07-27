package readiness

import (
	"errors"
	"os"
	"strings"

	"github.com/charliewilco/transwarp/internal/endpoint"
)

type ReleaseCollectionPreflightOptions struct {
	AllowIncomplete            string
	CollectNamedTunnel         string
	NamedTunnelLaunchMode      string
	NotarizeRequested          string
	NamedTunnelEvidencePath    string
	CIDispatchEvidencePath     string
	CleanMacEvidencePath       string
	CloudflareTunnelTokenSet   bool
	PublicURL                  string
	ExpectedCloudflaredVersion string
	SignIdentity               string
	AppleKeychainProfile       string
	AccessClientIDSet          bool
	AccessClientSecretSet      bool
	GitHub                     GitHubActionsEvidenceContext
}

func ReleaseCollectionPreflightFromEnv(getenv func(string) string) ReleaseCollectionPreflightOptions {
	if getenv == nil {
		getenv = os.Getenv
	}
	return ReleaseCollectionPreflightOptions{
		AllowIncomplete:            envValue(getenv, "TRANSWARP_COLLECT_ALLOW_INCOMPLETE", "0"),
		CollectNamedTunnel:         envValue(getenv, "TRANSWARP_COLLECT_NAMED_TUNNEL", "auto"),
		NamedTunnelLaunchMode:      envValue(getenv, "TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE", "app"),
		NotarizeRequested:          envValue(getenv, "TRANSWARP_NOTARIZE_REQUESTED", "0"),
		NamedTunnelEvidencePath:    getenv("TRANSWARP_NAMED_TUNNEL_EVIDENCE"),
		CIDispatchEvidencePath:     getenv("TRANSWARP_CI_DISPATCH_EVIDENCE"),
		CleanMacEvidencePath:       getenv("TRANSWARP_CLEAN_MAC_EVIDENCE"),
		CloudflareTunnelTokenSet:   getenv("TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN") != "" || getenv("TRANSWARP_TUNNEL_TOKEN") != "",
		PublicURL:                  getenv("TRANSWARP_PUBLIC_URL"),
		ExpectedCloudflaredVersion: getenv("TRANSWARP_EXPECTED_CLOUDFLARED_VERSION"),
		SignIdentity:               getenv("SIGN_IDENTITY"),
		AppleKeychainProfile:       getenv("APPLE_KEYCHAIN_PROFILE"),
		AccessClientIDSet:          getenv("TRANSWARP_ACCESS_CLIENT_ID") != "",
		AccessClientSecretSet:      getenv("TRANSWARP_ACCESS_CLIENT_SECRET") != "",
		GitHub: GitHubActionsEvidenceContext{
			GitHubActions: getenv("GITHUB_ACTIONS") != "",
			RunID:         getenv("GITHUB_RUN_ID"),
			RunAttempt:    getenv("GITHUB_RUN_ATTEMPT"),
			Workflow:      getenv("GITHUB_WORKFLOW"),
			Job:           getenv("GITHUB_JOB"),
			Repository:    getenv("GITHUB_REPOSITORY"),
			SHA:           getenv("GITHUB_SHA"),
			RunnerOS:      getenv("RUNNER_OS"),
			RunnerArch:    getenv("RUNNER_ARCH"),
			RunnerName:    getenv("RUNNER_NAME"),
		},
	}
}

func ValidateReleaseCollectionPreflight(options ReleaseCollectionPreflightOptions) error {
	allowIncomplete, err := parseReleaseBool(options.AllowIncomplete, "TRANSWARP_COLLECT_ALLOW_INCOMPLETE")
	if err != nil {
		return err
	}
	notarizeRequested, err := parseReleaseBool(options.NotarizeRequested, "TRANSWARP_NOTARIZE_REQUESTED")
	if err != nil {
		return err
	}
	collectNamedTunnel, err := releaseCollectsNamedTunnel(options)
	if err != nil {
		return err
	}
	if options.AccessClientIDSet != options.AccessClientSecretSet {
		return errors.New("TRANSWARP_ACCESS_CLIENT_ID and TRANSWARP_ACCESS_CLIENT_SECRET must be provided together")
	}
	if collectNamedTunnel {
		launchMode := strings.TrimSpace(options.NamedTunnelLaunchMode)
		if launchMode == "" {
			launchMode = "app"
		}
		if launchMode != "app" {
			return errors.New("TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE must be app for release evidence collection")
		}
		if !options.CloudflareTunnelTokenSet {
			return errors.New("TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN is required for named-tunnel evidence")
		}
		if strings.TrimSpace(options.PublicURL) == "" {
			return errors.New("TRANSWARP_PUBLIC_URL is required for named-tunnel evidence")
		}
		if err := endpoint.ValidateBaseURL(strings.TrimSpace(options.PublicURL), "TRANSWARP_PUBLIC_URL"); err != nil {
			return err
		}
	}

	if !allowIncomplete {
		if strings.TrimSpace(options.ExpectedCloudflaredVersion) == "" {
			return errors.New("TRANSWARP_EXPECTED_CLOUDFLARED_VERSION is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1")
		}
		signIdentity := strings.TrimSpace(options.SignIdentity)
		if signIdentity == "" {
			return errors.New("SIGN_IDENTITY is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1")
		}
		if signIdentity == "-" {
			return errors.New("SIGN_IDENTITY must be a Developer ID identity for strict release evidence")
		}
		if !notarizeRequested {
			return errors.New("TRANSWARP_NOTARIZE_REQUESTED=1 is required unless TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1")
		}
		if strings.TrimSpace(options.AppleKeychainProfile) == "" {
			return errors.New("APPLE_KEYCHAIN_PROFILE is required for strict release notarization; create it with xcrun notarytool store-credentials")
		}
		if !fileExists(options.NamedTunnelEvidencePath) && !collectNamedTunnel {
			return errors.New("named-tunnel evidence is required; set TRANSWARP_COLLECT_NAMED_TUNNEL=1 with TRANSWARP_CLOUDFLARE_TUNNEL_TOKEN and TRANSWARP_PUBLIC_URL, provide TRANSWARP_NAMED_TUNNEL_EVIDENCE, or set TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1")
		}
		if !fileExists(options.CIDispatchEvidencePath) && !releaseGeneratesCIDispatchEvidence(options, collectNamedTunnel) {
			return errors.New("CI dispatch evidence is required; run inside GitHub Actions with named-tunnel collection, provide TRANSWARP_CI_DISPATCH_EVIDENCE, or set TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1")
		}
		if releaseGeneratesCIDispatchEvidence(options, collectNamedTunnel) {
			if err := validateGitHubActionsContext(options.GitHub); err != nil {
				return err
			}
		}
		if strings.TrimSpace(options.CleanMacEvidencePath) == "" {
			return errors.New("clean-Mac evidence is required; run scripts/clean-mac-validate.sh on a separate Mac, provide TRANSWARP_CLEAN_MAC_EVIDENCE, or set TRANSWARP_COLLECT_ALLOW_INCOMPLETE=1")
		}
	}
	if strings.TrimSpace(options.CleanMacEvidencePath) != "" && !fileExists(options.CleanMacEvidencePath) {
		return errors.New("clean-Mac evidence file does not exist: " + strings.TrimSpace(options.CleanMacEvidencePath))
	}
	return nil
}

func envValue(getenv func(string) string, key string, fallback string) string {
	value := getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func parseReleaseBool(value string, name string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, errors.New(name + " must be 1, true, yes, 0, false, or no")
	}
}

func releaseCollectsNamedTunnel(options ReleaseCollectionPreflightOptions) (bool, error) {
	switch strings.TrimSpace(options.CollectNamedTunnel) {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	case "", "auto":
		return options.CloudflareTunnelTokenSet && strings.TrimSpace(options.PublicURL) != "", nil
	default:
		return false, errors.New("TRANSWARP_COLLECT_NAMED_TUNNEL must be auto, 1, true, yes, 0, false, or no")
	}
}

func releaseGeneratesCIDispatchEvidence(options ReleaseCollectionPreflightOptions, collectNamedTunnel bool) bool {
	return options.GitHub.GitHubActions && collectNamedTunnel
}

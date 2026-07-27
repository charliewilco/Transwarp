package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/charliewilco/transwarp/internal/actioncheck"
	"github.com/charliewilco/transwarp/internal/readiness"
)

func main() {
	options := readiness.Options{}
	summary := false
	allowIncomplete := false
	evidenceOnly := false
	reportPath := ""
	ciDispatchEvidenceOutput := ""
	ciDispatchSourceLog := ""
	ciDispatchSourceLogName := ""
	checkReceiptPath := ""
	checkReceiptKind := ""
	selfHostedEvidenceOutput := ""
	selfHostedArchitecture := ""
	selfHostedMacOS := ""
	selfHostedDeveloperDir := ""
	selfHostedXcode := ""
	selfHostedCodeSigningIdentitiesVisible := false
	selfHostedGitHubActions := false
	selfHostedRunnerName := ""
	selfHostedRunnerOS := ""
	selfHostedSourceLog := ""
	appLaunchEvidenceOutput := ""
	appLaunchTunnelMode := ""
	appLaunchPublicURL := ""
	appLaunchMachineID := ""
	appLaunchJobID := ""
	appLaunchRequestID := ""
	appLaunchBuildID := ""
	appLaunchTunnelReady := false
	appLaunchPublicStatusAuthenticated := false
	appLaunchBuildLog := ""
	appLaunchStatusJSON := ""
	appLaunchBuildStatusJSON := ""
	appLaunchPublicDiagnoseLog := ""
	appLaunchPublicDispatchLog := ""
	appLaunchAppLog := ""
	appLaunchAppStderr := ""
	cleanMacEvidenceOutput := ""
	cleanMacArchitecture := ""
	cleanMacOS := ""
	cleanMacMachineID := ""
	cleanMacJobID := ""
	cleanMacRequestID := ""
	cleanMacBuildID := ""
	cleanMacStatusJSON := ""
	cleanMacBuildLog := ""
	cleanMacBuildStatus := ""
	cleanMacCodesignLog := ""
	cleanMacStaplerLog := ""
	cleanMacGatekeeperLog := ""
	cleanMacAppLog := ""
	cleanMacAppStderr := ""
	namedTunnelEvidenceOutput := ""
	namedTunnelLaunchMode := ""
	namedTunnelPublicURL := ""
	namedTunnelAccessProtected := false
	namedTunnelMachineID := ""
	namedTunnelJobID := ""
	namedTunnelRequestID := ""
	namedTunnelDiagnoseLog := ""
	namedTunnelDispatchLog := ""
	namedTunnelRunnerLog := ""
	namedTunnelAppLog := ""
	namedTunnelAppStderr := ""
	namedTunnelTargetsRegistered := ""
	namedTunnelTargetsAfterDeregister := ""
	namedTunnelResults := ""
	quickTunnelEvidenceOutput := ""
	quickTunnelPublicURL := ""
	quickTunnelCoordinator := false
	quickTunnelMachineID := ""
	quickTunnelJobID := ""
	quickTunnelRequestID := ""
	quickTunnelDiagnoseLog := ""
	quickTunnelDispatchLog := ""
	quickTunnelTargetsBeforeDispatch := ""
	quickTunnelTargetsAfterDeregister := ""
	quickTunnelResults := ""
	releaseCollectorSmoke := false
	releaseCollectorAudit := ""
	releaseCollectorAuditStderr := ""
	releaseCollectorSelfHostedEvidence := ""
	releaseCollectorArchive := ""
	checkReleaseCollectionInputs := false
	checkGitHubActions := false
	checkGitHubActionsRoot := "."
	writeGitHubActionScript := ""
	flag.StringVar(&options.AppPath, "app", env("TRANSWARP_APP_PATH", ".build/Transwarp.app"), "path to Transwarp.app")
	flag.StringVar(&options.ReleaseArchivePath, "release-archive", env("TRANSWARP_RELEASE_ARCHIVE", ".build/Transwarp-release.zip"), "path to Transwarp-release.zip")
	flag.StringVar(&reportPath, "report", os.Getenv("TRANSWARP_AUDIT_REPORT"), "path to an existing transwarp-audit JSON report")
	flag.BoolVar(&options.Strict, "strict", os.Getenv("TRANSWARP_AUDIT_STRICT") == "1", "fail when release evidence is missing or warning-only")
	flag.BoolVar(&summary, "summary", os.Getenv("TRANSWARP_AUDIT_SUMMARY") == "1", "print a concise text summary instead of JSON")
	flag.BoolVar(&allowIncomplete, "allow-incomplete", os.Getenv("TRANSWARP_AUDIT_ALLOW_INCOMPLETE") == "1", "exit successfully when non-strict audit gaps are warnings or missing evidence only")
	flag.BoolVar(&evidenceOnly, "evidence-only", os.Getenv("TRANSWARP_AUDIT_EVIDENCE_ONLY") == "1", "validate only the evidence receipts supplied on the command line")
	flag.StringVar(&options.SelfHostedEvidence, "self-hosted-evidence", os.Getenv("TRANSWARP_SELF_HOSTED_EVIDENCE"), "JSON receipt from scripts/check-self-hosted-mac.sh")
	flag.StringVar(&options.AppLaunchEvidence, "app-launch-evidence", os.Getenv("TRANSWARP_APP_LAUNCH_EVIDENCE"), "JSON receipt from scripts/smoke-app-launch.sh")
	flag.StringVar(&options.NamedTunnelEvidence, "named-tunnel-evidence", os.Getenv("TRANSWARP_NAMED_TUNNEL_EVIDENCE"), "JSON receipt from scripts/smoke-cloudflare-named-coordinator.sh")
	flag.StringVar(&options.CIDispatchEvidence, "ci-dispatch-evidence", os.Getenv("TRANSWARP_CI_DISPATCH_EVIDENCE"), "JSON receipt from a real CI dispatch")
	flag.StringVar(&options.CleanMacEvidence, "clean-mac-evidence", os.Getenv("TRANSWARP_CLEAN_MAC_EVIDENCE"), "JSON receipt from clean-Mac Gatekeeper validation")
	flag.StringVar(&options.ExpectedCloudflaredVersion, "expected-cloudflared-version", os.Getenv("TRANSWARP_EXPECTED_CLOUDFLARED_VERSION"), "exact cloudflared --version string expected in the release bundle")
	flag.StringVar(&ciDispatchEvidenceOutput, "write-ci-dispatch-evidence", os.Getenv("TRANSWARP_WRITE_CI_DISPATCH_EVIDENCE"), "write a CI-dispatch evidence receipt from named-tunnel evidence and GitHub Actions environment")
	flag.StringVar(&ciDispatchSourceLog, "ci-dispatch-source-log", os.Getenv("TRANSWARP_CI_DISPATCH_SOURCE_LOG"), "source log from the GitHub Actions named-tunnel smoke")
	flag.StringVar(&ciDispatchSourceLogName, "ci-dispatch-source-log-name", os.Getenv("TRANSWARP_CI_DISPATCH_SOURCE_LOG_NAME"), "relative source-log file name to store in the generated CI-dispatch receipt")
	flag.StringVar(&checkReceiptPath, "check-receipt", os.Getenv("TRANSWARP_CHECK_RECEIPT"), "validate a minimal evidence receipt shape and exit")
	flag.StringVar(&checkReceiptKind, "check-receipt-kind", os.Getenv("TRANSWARP_CHECK_RECEIPT_KIND"), "expected kind for -check-receipt")
	flag.StringVar(&selfHostedEvidenceOutput, "write-self-hosted-evidence", os.Getenv("TRANSWARP_WRITE_SELF_HOSTED_EVIDENCE"), "write a self-hosted Mac readiness evidence receipt")
	flag.StringVar(&selfHostedArchitecture, "self-hosted-architecture", os.Getenv("TRANSWARP_SELF_HOSTED_ARCHITECTURE"), "architecture reported by the self-hosted Mac")
	flag.StringVar(&selfHostedMacOS, "self-hosted-macos", os.Getenv("TRANSWARP_SELF_HOSTED_MACOS"), "macOS version reported by the self-hosted Mac")
	flag.StringVar(&selfHostedDeveloperDir, "self-hosted-developer-dir", os.Getenv("TRANSWARP_SELF_HOSTED_DEVELOPER_DIR"), "Xcode developer directory reported by the self-hosted Mac")
	flag.StringVar(&selfHostedXcode, "self-hosted-xcode", os.Getenv("TRANSWARP_SELF_HOSTED_XCODE"), "Xcode version reported by the self-hosted Mac")
	flag.BoolVar(&selfHostedCodeSigningIdentitiesVisible, "self-hosted-code-signing-identities-visible", envBool("TRANSWARP_SELF_HOSTED_CODESIGN_VISIBLE"), "whether code-signing identities were visible to the self-hosted Mac")
	flag.BoolVar(&selfHostedGitHubActions, "self-hosted-github-actions", envBool("GITHUB_ACTIONS"), "whether the self-hosted readiness check ran inside GitHub Actions")
	flag.StringVar(&selfHostedRunnerName, "self-hosted-runner-name", os.Getenv("RUNNER_NAME"), "GitHub runner name reported by the self-hosted Mac")
	flag.StringVar(&selfHostedRunnerOS, "self-hosted-runner-os", os.Getenv("RUNNER_OS"), "GitHub runner OS reported by the self-hosted Mac")
	flag.StringVar(&selfHostedSourceLog, "self-hosted-source-log", os.Getenv("TRANSWARP_SELF_HOSTED_SOURCE_LOG"), "source log from the self-hosted readiness check")
	flag.StringVar(&appLaunchEvidenceOutput, "write-app-launch-evidence", os.Getenv("TRANSWARP_WRITE_APP_LAUNCH_EVIDENCE"), "write an app-launch evidence receipt from smoke logs")
	flag.StringVar(&appLaunchTunnelMode, "app-launch-tunnel-mode", env("TRANSWARP_APP_LAUNCH_TUNNEL_MODE", "off"), "app launch tunnel mode: off or quick")
	flag.StringVar(&appLaunchPublicURL, "app-launch-public-url", os.Getenv("TRANSWARP_APP_LAUNCH_PUBLIC_URL"), "public URL used by app-launch quick tunnel smoke")
	flag.StringVar(&appLaunchMachineID, "app-launch-machine-id", os.Getenv("TRANSWARP_APP_LAUNCH_MACHINE_ID"), "machine ID used by app-launch smoke")
	flag.StringVar(&appLaunchJobID, "app-launch-job-id", os.Getenv("TRANSWARP_APP_LAUNCH_JOB_ID"), "job ID used by app-launch smoke")
	flag.StringVar(&appLaunchRequestID, "app-launch-request-id", os.Getenv("TRANSWARP_APP_LAUNCH_REQUEST_ID"), "request ID used by app-launch smoke")
	flag.StringVar(&appLaunchBuildID, "app-launch-build-id", os.Getenv("TRANSWARP_APP_LAUNCH_BUILD_ID"), "build ID accepted during app-launch smoke")
	flag.BoolVar(&appLaunchTunnelReady, "app-launch-tunnel-ready", envBool("TRANSWARP_APP_LAUNCH_TUNNEL_READY"), "whether app-launch quick tunnel was ready")
	flag.BoolVar(&appLaunchPublicStatusAuthenticated, "app-launch-public-status-authenticated", envBool("TRANSWARP_APP_LAUNCH_PUBLIC_STATUS_AUTHENTICATED"), "whether app-launch quick tunnel authenticated status")
	flag.StringVar(&appLaunchBuildLog, "app-launch-build-log", os.Getenv("TRANSWARP_APP_LAUNCH_BUILD_LOG"), "build log from app-launch smoke")
	flag.StringVar(&appLaunchStatusJSON, "app-launch-status-json", os.Getenv("TRANSWARP_APP_LAUNCH_STATUS_JSON"), "status JSON from app-launch smoke")
	flag.StringVar(&appLaunchBuildStatusJSON, "app-launch-build-status-json", os.Getenv("TRANSWARP_APP_LAUNCH_BUILD_STATUS_JSON"), "build status JSON from app-launch smoke")
	flag.StringVar(&appLaunchPublicDiagnoseLog, "app-launch-public-diagnose-log", os.Getenv("TRANSWARP_APP_LAUNCH_PUBLIC_DIAGNOSE_LOG"), "public diagnose log from app-launch quick tunnel smoke")
	flag.StringVar(&appLaunchPublicDispatchLog, "app-launch-public-dispatch-log", os.Getenv("TRANSWARP_APP_LAUNCH_PUBLIC_DISPATCH_LOG"), "public dispatch log from app-launch quick tunnel smoke")
	flag.StringVar(&appLaunchAppLog, "app-launch-app-log", os.Getenv("TRANSWARP_APP_LAUNCH_APP_LOG"), "app stdout log from app-launch smoke")
	flag.StringVar(&appLaunchAppStderr, "app-launch-app-stderr", os.Getenv("TRANSWARP_APP_LAUNCH_APP_STDERR"), "app stderr log from app-launch smoke")
	flag.StringVar(&cleanMacEvidenceOutput, "write-clean-mac-evidence", os.Getenv("TRANSWARP_WRITE_CLEAN_MAC_EVIDENCE"), "write a clean-Mac evidence receipt from validation logs")
	flag.StringVar(&cleanMacArchitecture, "clean-mac-architecture", os.Getenv("TRANSWARP_CLEAN_MAC_ARCHITECTURE"), "architecture reported by the clean Mac")
	flag.StringVar(&cleanMacOS, "clean-mac-os", os.Getenv("TRANSWARP_CLEAN_MAC_OS"), "macOS name and version reported by the clean Mac")
	flag.StringVar(&cleanMacMachineID, "clean-mac-machine-id", os.Getenv("TRANSWARP_CLEAN_MAC_MACHINE_ID"), "machine ID used by clean-Mac validation")
	flag.StringVar(&cleanMacJobID, "clean-mac-job-id", os.Getenv("TRANSWARP_CLEAN_MAC_JOB_ID"), "job ID used by clean-Mac validation")
	flag.StringVar(&cleanMacRequestID, "clean-mac-request-id", os.Getenv("TRANSWARP_CLEAN_MAC_REQUEST_ID"), "request ID used by clean-Mac validation")
	flag.StringVar(&cleanMacBuildID, "clean-mac-build-id", os.Getenv("TRANSWARP_CLEAN_MAC_BUILD_ID"), "build ID accepted during clean-Mac validation")
	flag.StringVar(&cleanMacStatusJSON, "clean-mac-status-json", os.Getenv("TRANSWARP_CLEAN_MAC_STATUS_JSON"), "status JSON from clean-Mac validation")
	flag.StringVar(&cleanMacBuildLog, "clean-mac-build-log", os.Getenv("TRANSWARP_CLEAN_MAC_BUILD_LOG"), "build log from clean-Mac validation")
	flag.StringVar(&cleanMacBuildStatus, "clean-mac-build-status-json", os.Getenv("TRANSWARP_CLEAN_MAC_BUILD_STATUS_JSON"), "build status JSON from clean-Mac validation")
	flag.StringVar(&cleanMacCodesignLog, "clean-mac-codesign-log", os.Getenv("TRANSWARP_CLEAN_MAC_CODESIGN_LOG"), "strict codesign verification log from clean-Mac validation")
	flag.StringVar(&cleanMacStaplerLog, "clean-mac-stapler-log", os.Getenv("TRANSWARP_CLEAN_MAC_STAPLER_LOG"), "notarization stapler validation log from clean-Mac validation")
	flag.StringVar(&cleanMacGatekeeperLog, "clean-mac-gatekeeper-log", os.Getenv("TRANSWARP_CLEAN_MAC_GATEKEEPER_LOG"), "Gatekeeper assessment log from clean-Mac validation")
	flag.StringVar(&cleanMacAppLog, "clean-mac-app-log", os.Getenv("TRANSWARP_CLEAN_MAC_APP_LOG"), "app stdout log from clean-Mac validation")
	flag.StringVar(&cleanMacAppStderr, "clean-mac-app-stderr", os.Getenv("TRANSWARP_CLEAN_MAC_APP_STDERR"), "app stderr log from clean-Mac validation")
	flag.StringVar(&namedTunnelEvidenceOutput, "write-named-tunnel-evidence", os.Getenv("TRANSWARP_WRITE_NAMED_TUNNEL_EVIDENCE"), "write a named-tunnel evidence receipt from smoke logs and coordinator results")
	flag.StringVar(&namedTunnelLaunchMode, "named-tunnel-launch-mode", env("TRANSWARP_NAMED_TUNNEL_LAUNCH_MODE", "app"), "named-tunnel smoke launch mode: app or runner")
	flag.StringVar(&namedTunnelPublicURL, "named-tunnel-public-url", os.Getenv("TRANSWARP_PUBLIC_URL"), "HTTPS public URL for the named Cloudflare Tunnel")
	flag.BoolVar(&namedTunnelAccessProtected, "named-tunnel-access-protected", os.Getenv("TRANSWARP_ACCESS_CLIENT_ID") != "", "whether the named tunnel was protected by Cloudflare Access service credentials")
	flag.StringVar(&namedTunnelMachineID, "named-tunnel-machine-id", os.Getenv("TRANSWARP_NAMED_TUNNEL_MACHINE_ID"), "machine ID used by the named-tunnel smoke")
	flag.StringVar(&namedTunnelJobID, "named-tunnel-job-id", os.Getenv("TRANSWARP_NAMED_TUNNEL_JOB_ID"), "job ID used by the named-tunnel smoke")
	flag.StringVar(&namedTunnelRequestID, "named-tunnel-request-id", os.Getenv("TRANSWARP_NAMED_TUNNEL_REQUEST_ID"), "request ID used by the named-tunnel smoke")
	flag.StringVar(&namedTunnelDiagnoseLog, "named-tunnel-diagnose-log", os.Getenv("TRANSWARP_NAMED_TUNNEL_DIAGNOSE_LOG"), "diagnose log from the named-tunnel smoke")
	flag.StringVar(&namedTunnelDispatchLog, "named-tunnel-dispatch-log", os.Getenv("TRANSWARP_NAMED_TUNNEL_DISPATCH_LOG"), "dispatch log from the named-tunnel smoke")
	flag.StringVar(&namedTunnelRunnerLog, "named-tunnel-runner-log", os.Getenv("TRANSWARP_NAMED_TUNNEL_RUNNER_LOG"), "app-captured runner log from the named-tunnel smoke")
	flag.StringVar(&namedTunnelAppLog, "named-tunnel-app-log", os.Getenv("TRANSWARP_NAMED_TUNNEL_APP_LOG"), "app stdout log from the named-tunnel smoke")
	flag.StringVar(&namedTunnelAppStderr, "named-tunnel-app-stderr", os.Getenv("TRANSWARP_NAMED_TUNNEL_APP_STDERR"), "app stderr log from the named-tunnel smoke")
	flag.StringVar(&namedTunnelTargetsRegistered, "named-tunnel-targets-registered", os.Getenv("TRANSWARP_NAMED_TUNNEL_TARGETS_REGISTERED"), "coordinator target snapshot before dispatch")
	flag.StringVar(&namedTunnelTargetsAfterDeregister, "named-tunnel-targets-after-deregister", os.Getenv("TRANSWARP_NAMED_TUNNEL_TARGETS_AFTER_DEREGISTER"), "coordinator target snapshot after deregistration")
	flag.StringVar(&namedTunnelResults, "named-tunnel-results", os.Getenv("TRANSWARP_NAMED_TUNNEL_RESULTS"), "coordinator results snapshot after dispatch")
	flag.StringVar(&quickTunnelEvidenceOutput, "write-quick-tunnel-evidence", os.Getenv("TRANSWARP_WRITE_QUICK_TUNNEL_EVIDENCE"), "write a quick-tunnel diagnostic receipt from smoke logs")
	flag.StringVar(&quickTunnelPublicURL, "quick-tunnel-public-url", os.Getenv("TRANSWARP_QUICK_TUNNEL_PUBLIC_URL"), "HTTPS public URL for the quick Cloudflare Tunnel")
	flag.BoolVar(&quickTunnelCoordinator, "quick-tunnel-coordinator", envBool("TRANSWARP_QUICK_TUNNEL_COORDINATOR"), "whether the quick-tunnel smoke dispatched through the coordinator")
	flag.StringVar(&quickTunnelMachineID, "quick-tunnel-machine-id", os.Getenv("TRANSWARP_QUICK_TUNNEL_MACHINE_ID"), "machine ID used by the quick-tunnel coordinator smoke")
	flag.StringVar(&quickTunnelJobID, "quick-tunnel-job-id", os.Getenv("TRANSWARP_QUICK_TUNNEL_JOB_ID"), "job ID used by the quick-tunnel coordinator smoke")
	flag.StringVar(&quickTunnelRequestID, "quick-tunnel-request-id", os.Getenv("TRANSWARP_QUICK_TUNNEL_REQUEST_ID"), "request ID used by the quick-tunnel coordinator smoke")
	flag.StringVar(&quickTunnelDiagnoseLog, "quick-tunnel-diagnose-log", os.Getenv("TRANSWARP_QUICK_TUNNEL_DIAGNOSE_LOG"), "diagnose log from the quick-tunnel smoke")
	flag.StringVar(&quickTunnelDispatchLog, "quick-tunnel-dispatch-log", os.Getenv("TRANSWARP_QUICK_TUNNEL_DISPATCH_LOG"), "dispatch log from the quick-tunnel smoke")
	flag.StringVar(&quickTunnelTargetsBeforeDispatch, "quick-tunnel-targets-before-dispatch", os.Getenv("TRANSWARP_QUICK_TUNNEL_TARGETS_BEFORE_DISPATCH"), "coordinator target snapshot before quick-tunnel dispatch")
	flag.StringVar(&quickTunnelTargetsAfterDeregister, "quick-tunnel-targets-after-deregister", os.Getenv("TRANSWARP_QUICK_TUNNEL_TARGETS_AFTER_DEREGISTER"), "coordinator target snapshot after quick-tunnel deregistration")
	flag.StringVar(&quickTunnelResults, "quick-tunnel-results", os.Getenv("TRANSWARP_QUICK_TUNNEL_RESULTS"), "coordinator results snapshot after quick-tunnel dispatch")
	flag.BoolVar(&releaseCollectorSmoke, "check-release-evidence-collector-smoke", envBool("TRANSWARP_CHECK_RELEASE_EVIDENCE_COLLECTOR_SMOKE"), "validate the local incomplete release-evidence collector smoke output")
	flag.StringVar(&releaseCollectorAudit, "release-collector-audit", os.Getenv("TRANSWARP_RELEASE_COLLECTOR_AUDIT"), "transwarp-audit JSON written by the release-evidence collector smoke")
	flag.StringVar(&releaseCollectorAuditStderr, "release-collector-audit-stderr", os.Getenv("TRANSWARP_RELEASE_COLLECTOR_AUDIT_STDERR"), "transwarp-audit stderr written by the release-evidence collector smoke")
	flag.StringVar(&releaseCollectorSelfHostedEvidence, "release-collector-self-hosted-evidence", os.Getenv("TRANSWARP_RELEASE_COLLECTOR_SELF_HOSTED_EVIDENCE"), "self-hosted evidence JSON used by the release-evidence collector smoke")
	flag.StringVar(&releaseCollectorArchive, "release-collector-archive", os.Getenv("TRANSWARP_RELEASE_COLLECTOR_ARCHIVE"), "release archive written by the release-evidence collector smoke")
	flag.BoolVar(&checkReleaseCollectionInputs, "check-release-collection-inputs", envBool("TRANSWARP_CHECK_RELEASE_COLLECTION_INPUTS"), "validate release-evidence collector inputs from the current environment")
	flag.BoolVar(&checkGitHubActions, "check-github-actions", envBool("TRANSWARP_CHECK_GITHUB_ACTIONS"), "validate action.yml and example GitHub Actions workflows")
	flag.StringVar(&checkGitHubActionsRoot, "check-github-actions-root", env("TRANSWARP_CHECK_GITHUB_ACTIONS_ROOT", "."), "repository root for -check-github-actions")
	flag.StringVar(&writeGitHubActionScript, "write-github-action-script", os.Getenv("TRANSWARP_WRITE_GITHUB_ACTION_SCRIPT"), "write the composite action run script extracted from action.yml")
	flag.Parse()

	if checkReleaseCollectionInputs {
		err := readiness.ValidateReleaseCollectionPreflight(readiness.ReleaseCollectionPreflightFromEnv(os.Getenv))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println("release collection input preflight passed")
		return
	}

	if checkReceiptPath != "" || checkReceiptKind != "" {
		if err := readiness.ValidateEvidencePaths(checkReceiptPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := readiness.ValidateReceiptShape(checkReceiptPath, checkReceiptKind); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if releaseCollectorSmoke {
		if err := readiness.ValidateEvidencePaths(releaseCollectorAudit, releaseCollectorAuditStderr, releaseCollectorSelfHostedEvidence); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := readiness.ValidateOptionalFilePaths(releaseCollectorArchive); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		err := readiness.ValidateReleaseEvidenceCollectorSmoke(readiness.ReleaseEvidenceCollectorSmokeOptions{
			AuditPath:              releaseCollectorAudit,
			AuditStderrPath:        releaseCollectorAuditStderr,
			SelfHostedEvidencePath: releaseCollectorSelfHostedEvidence,
			ReleaseArchivePath:     releaseCollectorArchive,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println("release evidence collector smoke passed")
		return
	}

	if writeGitHubActionScript != "" {
		if checkGitHubActions {
			if err := actioncheck.Check(checkGitHubActionsRoot); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		script, err := actioncheck.CompositeRunScript(checkGitHubActionsRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := os.WriteFile(writeGitHubActionScript, []byte(script), 0o700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if checkGitHubActions {
		if err := actioncheck.Check(checkGitHubActionsRoot); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if selfHostedEvidenceOutput != "" {
		if err := readiness.ValidateEvidencePaths(selfHostedSourceLog); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := readiness.ValidateOptionalFilePaths(selfHostedEvidenceOutput); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		err := readiness.WriteSelfHostedEvidence(readiness.SelfHostedEvidenceWriteOptions{
			OutputPath:                   selfHostedEvidenceOutput,
			Architecture:                 selfHostedArchitecture,
			MacOS:                        selfHostedMacOS,
			DeveloperDir:                 selfHostedDeveloperDir,
			Xcode:                        selfHostedXcode,
			CodeSigningIdentitiesVisible: selfHostedCodeSigningIdentitiesVisible,
			GitHubActions:                selfHostedGitHubActions,
			RunnerName:                   selfHostedRunnerName,
			RunnerOS:                     selfHostedRunnerOS,
			SourceLogPath:                selfHostedSourceLog,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if appLaunchEvidenceOutput != "" {
		if err := readiness.ValidateEvidencePaths(appLaunchBuildLog, appLaunchStatusJSON, appLaunchBuildStatusJSON, appLaunchAppLog, appLaunchAppStderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if appLaunchTunnelMode == "quick" {
			if err := readiness.ValidateEvidencePaths(appLaunchPublicDiagnoseLog, appLaunchPublicDispatchLog); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		if err := readiness.ValidateOptionalFilePaths(appLaunchEvidenceOutput); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		err := readiness.WriteAppLaunchEvidence(readiness.AppLaunchEvidenceWriteOptions{
			OutputPath:                appLaunchEvidenceOutput,
			AppPath:                   options.AppPath,
			TunnelMode:                appLaunchTunnelMode,
			PublicURL:                 appLaunchPublicURL,
			MachineID:                 appLaunchMachineID,
			JobID:                     appLaunchJobID,
			RequestID:                 appLaunchRequestID,
			BuildID:                   appLaunchBuildID,
			TunnelReady:               appLaunchTunnelReady,
			PublicStatusAuthenticated: appLaunchPublicStatusAuthenticated,
			BuildLogPath:              appLaunchBuildLog,
			StatusJSONPath:            appLaunchStatusJSON,
			BuildStatusJSONPath:       appLaunchBuildStatusJSON,
			PublicDiagnoseLogPath:     appLaunchPublicDiagnoseLog,
			PublicDispatchLogPath:     appLaunchPublicDispatchLog,
			AppLogPath:                appLaunchAppLog,
			AppStderrPath:             appLaunchAppStderr,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if cleanMacEvidenceOutput != "" {
		if err := readiness.ValidateEvidencePaths(cleanMacStatusJSON, cleanMacBuildLog, cleanMacBuildStatus, cleanMacCodesignLog, cleanMacStaplerLog, cleanMacGatekeeperLog, cleanMacAppLog, cleanMacAppStderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := readiness.ValidateOptionalFilePaths(cleanMacEvidenceOutput); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		err := readiness.WriteCleanMacEvidence(readiness.CleanMacEvidenceWriteOptions{
			OutputPath:        cleanMacEvidenceOutput,
			AppPath:           options.AppPath,
			Architecture:      cleanMacArchitecture,
			MacOS:             cleanMacOS,
			MachineID:         cleanMacMachineID,
			JobID:             cleanMacJobID,
			RequestID:         cleanMacRequestID,
			BuildID:           cleanMacBuildID,
			StatusJSONPath:    cleanMacStatusJSON,
			BuildLogPath:      cleanMacBuildLog,
			BuildStatusPath:   cleanMacBuildStatus,
			CodesignLogPath:   cleanMacCodesignLog,
			StaplerLogPath:    cleanMacStaplerLog,
			GatekeeperLogPath: cleanMacGatekeeperLog,
			AppLogPath:        cleanMacAppLog,
			AppStderrPath:     cleanMacAppStderr,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if namedTunnelEvidenceOutput != "" {
		if err := readiness.ValidateEvidencePaths(namedTunnelDiagnoseLog, namedTunnelDispatchLog, namedTunnelRunnerLog, namedTunnelTargetsRegistered, namedTunnelTargetsAfterDeregister, namedTunnelResults); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if namedTunnelLaunchMode == "app" {
			if err := readiness.ValidateEvidencePaths(namedTunnelAppLog, namedTunnelAppStderr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		if err := readiness.ValidateOptionalFilePaths(namedTunnelEvidenceOutput); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		err := readiness.WriteNamedTunnelEvidence(readiness.NamedTunnelEvidenceWriteOptions{
			OutputPath:                 namedTunnelEvidenceOutput,
			AppPath:                    options.AppPath,
			LaunchMode:                 namedTunnelLaunchMode,
			PublicURL:                  namedTunnelPublicURL,
			AccessProtected:            namedTunnelAccessProtected,
			MachineID:                  namedTunnelMachineID,
			JobID:                      namedTunnelJobID,
			RequestID:                  namedTunnelRequestID,
			DiagnoseLogPath:            namedTunnelDiagnoseLog,
			DispatchLogPath:            namedTunnelDispatchLog,
			RunnerLogPath:              namedTunnelRunnerLog,
			AppLogPath:                 namedTunnelAppLog,
			AppStderrPath:              namedTunnelAppStderr,
			TargetsRegisteredPath:      namedTunnelTargetsRegistered,
			TargetsAfterDeregisterPath: namedTunnelTargetsAfterDeregister,
			ResultsPath:                namedTunnelResults,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if quickTunnelEvidenceOutput != "" {
		if err := readiness.ValidateEvidencePaths(quickTunnelDiagnoseLog, quickTunnelDispatchLog); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if quickTunnelCoordinator {
			if err := readiness.ValidateEvidencePaths(quickTunnelTargetsBeforeDispatch, quickTunnelTargetsAfterDeregister, quickTunnelResults); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		if err := readiness.ValidateOptionalFilePaths(quickTunnelEvidenceOutput); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		err := readiness.WriteQuickTunnelEvidence(readiness.QuickTunnelEvidenceWriteOptions{
			OutputPath:                    quickTunnelEvidenceOutput,
			PublicURL:                     quickTunnelPublicURL,
			Coordinator:                   quickTunnelCoordinator,
			MachineID:                     quickTunnelMachineID,
			JobID:                         quickTunnelJobID,
			RequestID:                     quickTunnelRequestID,
			DiagnoseLogPath:               quickTunnelDiagnoseLog,
			DispatchLogPath:               quickTunnelDispatchLog,
			TargetsBeforeDispatchLogPath:  quickTunnelTargetsBeforeDispatch,
			TargetsAfterDeregisterLogPath: quickTunnelTargetsAfterDeregister,
			ResultsPath:                   quickTunnelResults,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if ciDispatchEvidenceOutput != "" {
		if err := readiness.ValidateEvidencePaths(options.NamedTunnelEvidence, ciDispatchSourceLog); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := readiness.ValidateOptionalFilePaths(ciDispatchEvidenceOutput); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		err := readiness.WriteCIDispatchEvidence(readiness.CIDispatchEvidenceWriteOptions{
			NamedTunnelEvidencePath: options.NamedTunnelEvidence,
			SourceLogPath:           ciDispatchSourceLog,
			SourceLogName:           ciDispatchSourceLogName,
			OutputPath:              ciDispatchEvidenceOutput,
			GitHub: readiness.GitHubActionsEvidenceContext{
				GitHubActions: os.Getenv("GITHUB_ACTIONS") != "",
				RunID:         os.Getenv("GITHUB_RUN_ID"),
				RunAttempt:    os.Getenv("GITHUB_RUN_ATTEMPT"),
				Workflow:      os.Getenv("GITHUB_WORKFLOW"),
				Job:           os.Getenv("GITHUB_JOB"),
				Repository:    os.Getenv("GITHUB_REPOSITORY"),
				SHA:           os.Getenv("GITHUB_SHA"),
				RunnerOS:      os.Getenv("RUNNER_OS"),
				RunnerArch:    os.Getenv("RUNNER_ARCH"),
				RunnerName:    os.Getenv("RUNNER_NAME"),
			},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if reportPath != "" {
		if err := readiness.ValidateEvidencePaths(reportPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		report, err := readiness.ReadReport(reportPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		writeReport(report, summary)
		exitReport(report, allowIncomplete)
	}

	if err := readiness.ValidateEvidencePaths(options.SelfHostedEvidence, options.AppLaunchEvidence, options.NamedTunnelEvidence, options.CIDispatchEvidence, options.CleanMacEvidence); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := readiness.ValidateOptionalFilePaths(options.ReleaseArchivePath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if evidenceOnly {
		report := readiness.EvidenceOnly(options)
		writeReport(report, summary)
		exitReport(report, allowIncomplete)
	}

	report := readiness.Audit(context.Background(), options)
	writeReport(report, summary)
	exitReport(report, allowIncomplete)
}

func writeReport(report readiness.Report, summary bool) {
	if summary {
		for _, line := range report.SummaryLines() {
			fmt.Println(line)
		}
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "\t")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func exitReport(report readiness.Report, allowIncomplete bool) {
	if allowIncomplete {
		os.Exit(report.ExitCodeAllowingIncomplete())
	}
	os.Exit(report.ExitCode())
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string) bool {
	switch os.Getenv(key) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

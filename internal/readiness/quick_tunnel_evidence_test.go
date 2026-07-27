package readiness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteQuickTunnelDirectEvidence(t *testing.T) {
	sourceDir := t.TempDir()
	outputDir := t.TempDir()
	diagnoseLog := filepath.Join(sourceDir, "diagnose.log")
	dispatchLog := filepath.Join(sourceDir, "dispatch.log")
	if err := os.WriteFile(diagnoseLog, []byte("diagnosis passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dispatchLog, []byte("hello from cloudflare tunnel\n[result] passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(outputDir, "quick-tunnel.json")
	err := WriteQuickTunnelEvidence(QuickTunnelEvidenceWriteOptions{
		OutputPath:      outputPath,
		PublicURL:       "https://example.trycloudflare.com",
		DiagnoseLogPath: diagnoseLog,
		DispatchLogPath: dispatchLog,
		Now:             fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	receipt := readReceiptMap(t, outputPath)
	if receipt["kind"] != "transwarp-quick-tunnel-diagnostic" {
		t.Fatalf("unexpected kind: %v", receipt["kind"])
	}
	if receipt["coordinator"] != false {
		t.Fatalf("expected direct receipt, got %+v", receipt)
	}
	if receipt["public_url"] != "https://example.trycloudflare.com" {
		t.Fatalf("unexpected public_url: %v", receipt["public_url"])
	}
	if _, err := os.Stat(filepath.Join(outputDir, "quick-tunnel-direct-diagnose.log")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "quick-tunnel-direct-dispatch.log")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteQuickTunnelCoordinatorEvidenceValidatesAcceptedBuildAndResults(t *testing.T) {
	sourceDir := t.TempDir()
	outputDir := t.TempDir()
	diagnoseLog := filepath.Join(sourceDir, "diagnose.log")
	dispatchLog := filepath.Join(sourceDir, "dispatch.log")
	targetsBefore := filepath.Join(sourceDir, "targets-before.json")
	targetsAfter := filepath.Join(sourceDir, "targets-after.json")
	results := filepath.Join(sourceDir, "results.json")
	if err := os.WriteFile(diagnoseLog, []byte("diagnosis passed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dispatchLog, []byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"echo","request_id":"request-123","machine_id":"machine-123","public_url":"https://example.trycloudflare.com"}
[result] recorded passed
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetsBefore, []byte(`[{"machine_id":"machine-123"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetsAfter, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(results, []byte(`[{"build_id":"build-123","job_id":"echo","request_id":"request-123","status":"passed"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(outputDir, "quick-tunnel.json")
	err := WriteQuickTunnelEvidence(QuickTunnelEvidenceWriteOptions{
		OutputPath:                    outputPath,
		PublicURL:                     "https://example.trycloudflare.com",
		Coordinator:                   true,
		MachineID:                     "machine-123",
		JobID:                         "echo",
		RequestID:                     "request-123",
		DiagnoseLogPath:               diagnoseLog,
		DispatchLogPath:               dispatchLog,
		TargetsBeforeDispatchLogPath:  targetsBefore,
		TargetsAfterDeregisterLogPath: targetsAfter,
		ResultsPath:                   results,
		Now:                           fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	receipt := readReceiptMap(t, outputPath)
	if receipt["coordinator"] != true {
		t.Fatalf("expected coordinator receipt, got %+v", receipt)
	}
	if receipt["build_id"] != "build-123" {
		t.Fatalf("unexpected build_id: %v", receipt["build_id"])
	}
	if receipt["job_id"] != "echo" {
		t.Fatalf("unexpected job_id: %v", receipt["job_id"])
	}
	if _, err := os.Stat(filepath.Join(outputDir, "quick-tunnel-coordinator-targets-before-dispatch.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "quick-tunnel-coordinator-targets-after-deregister.json")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteQuickTunnelCoordinatorEvidenceRejectsMismatchedResults(t *testing.T) {
	sourceDir := t.TempDir()
	diagnoseLog := filepath.Join(sourceDir, "diagnose.log")
	dispatchLog := filepath.Join(sourceDir, "dispatch.log")
	targetsBefore := filepath.Join(sourceDir, "targets-before.json")
	targetsAfter := filepath.Join(sourceDir, "targets-after.json")
	results := filepath.Join(sourceDir, "results.json")
	for _, path := range []string{diagnoseLog, targetsBefore, targetsAfter} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(dispatchLog, []byte(`{"kind":"coordinator","message":"accepted runner build","build_id":"build-123","job_id":"echo","request_id":"request-123","machine_id":"machine-123","public_url":"https://example.trycloudflare.com"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(results, []byte(`[{"build_id":"other-build","job_id":"echo","request_id":"request-123","status":"passed"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteQuickTunnelEvidence(QuickTunnelEvidenceWriteOptions{
		OutputPath:                    filepath.Join(t.TempDir(), "quick-tunnel.json"),
		PublicURL:                     "https://example.trycloudflare.com",
		Coordinator:                   true,
		MachineID:                     "machine-123",
		JobID:                         "echo",
		RequestID:                     "request-123",
		DiagnoseLogPath:               diagnoseLog,
		DispatchLogPath:               dispatchLog,
		TargetsBeforeDispatchLogPath:  targetsBefore,
		TargetsAfterDeregisterLogPath: targetsAfter,
		ResultsPath:                   results,
		Now:                           fixedNow,
	})
	if err == nil || !strings.Contains(err.Error(), "quick tunnel result build_id did not match accepted-build metadata") {
		t.Fatalf("expected mismatched result error, got %v", err)
	}
}

func readReceiptMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

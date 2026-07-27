package configwriter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charliewilco/transwarp/internal/agent"
)

func TestWriteAgentConfigWritesValidatedOwnerReadableJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "agent.json")
	config := agent.Config{
		ListenAddress:    "127.0.0.1:18189",
		MachineID:        "config-writer-smoke",
		MachineName:      "Config Writer Smoke Mac",
		SharedToken:      "runner-token",
		WorkspaceRoot:    t.TempDir(),
		PreventSleep:     false,
		RedactedValues:   []string{},
		HeartbeatSeconds: 30,
		Tunnel:           agent.TunnelConfig{Mode: "off"},
		Jobs: []agent.JobConfig{{
			ID:                      "echo",
			Label:                   "Echo Smoke",
			WorkingDirectory:        t.TempDir(),
			Checkout:                false,
			AllowedRepositories:     []string{},
			Command:                 "/bin/echo",
			Arguments:               []string{"hello"},
			Environment:             map[string]string{},
			RedactedEnvironmentKeys: []string{},
			TimeoutSeconds:          10,
		}},
	}

	if err := WriteAgentConfig(path, config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected 0600 mode, got %#o", mode)
	}
	loaded, err := agent.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MachineID != config.MachineID {
		t.Fatalf("expected machine id round trip, got %q", loaded.MachineID)
	}
}

func TestWriteAgentConfigRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	err := WriteAgentConfig(path, agent.Config{ListenAddress: "0.0.0.0:18189"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file after failed validation, got %v", statErr)
	}
}

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigValidatesLoopback(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "0.0.0.0:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected non-loopback listen_address to fail")
	}
}

func TestLoadConfigRejectsShellText(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/bin/sh -c echo nope",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected shell command text to fail")
	}
}

func TestLoadConfigRejectsShellExecutable(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/bin/sh",
			"arguments": ["-c", "echo nope"],
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected shell executable to fail")
	}
}

func TestLoadConfigRejectsUnsafeMachineID(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine/123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "machine_id") {
		t.Fatalf("expected unsafe machine_id to fail, got %v", err)
	}
}

func TestLoadConfigRejectsUnsafeJobID(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "xcode/debug",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "job_id") {
		t.Fatalf("expected unsafe job_id to fail, got %v", err)
	}
}

func TestLoadConfigRejectsInvalidEnvironmentKey(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"environment": {"API TOKEN": "secret"},
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "environment key") {
		t.Fatalf("expected invalid environment key to fail, got %v", err)
	}
}

func TestLoadConfigRejectsInvalidRedactedEnvironmentKey(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"redacted_environment_keys": ["1PASSWORD"],
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "redacted_environment_keys") {
		t.Fatalf("expected invalid redacted environment key to fail, got %v", err)
	}
}

func TestLoadConfigRejectsReservedEnvironmentKey(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"environment": {"TRANSWARP_REF": "fake"},
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "reserved TRANSWARP_ prefix") {
		t.Fatalf("expected reserved environment key to fail, got %v", err)
	}
}

func TestLoadConfigRejectsReservedRedactedEnvironmentKey(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"redacted_environment_keys": ["TRANSWARP_TOKEN"],
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "reserved TRANSWARP_ prefix") {
		t.Fatalf("expected reserved redacted environment key to fail, got %v", err)
	}
}

func TestLoadConfigRejectsMalformedCheckoutAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "missing colon", header: "Authorization Bearer local-token"},
		{name: "empty name", header: ": Bearer local-token"},
		{name: "invalid name", header: "Bad Header: local-token"},
		{name: "empty value", header: "Authorization:"},
		{name: "injected value", header: "Authorization: Bearer local-token\nInjected: yes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, `{
				"listen_address": "127.0.0.1:8188",
				"machine_id": "machine-123",
				"machine_name": "Mac",
				"shared_token": "secret",
				"tunnel": {"mode": "off"},
				"jobs": [{
					"id": "build",
					"label": "Build",
					"working_directory": "`+escapeJSON(t.TempDir())+`",
					"checkout": true,
					"allowed_repositories": ["https://github.com/example/app.git"],
					"checkout_authorization_header": "`+escapeJSON(test.header)+`",
					"command": "/usr/bin/true",
					"timeout_seconds": 10
				}]
			}`)

			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "checkout_authorization_header") {
				t.Fatalf("expected malformed checkout_authorization_header to fail, got %v", err)
			}
		})
	}
}

func TestLoadConfigAcceptsMinimalValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(dir)+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if config.Jobs[0].ID != "build" {
		t.Fatalf("unexpected job ID: %s", config.Jobs[0].ID)
	}
}

func TestLoadConfigRejectsRelativeWorkspaceRoot(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"workspace_root": "relative/workspaces",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "workspace_root") {
		t.Fatalf("expected relative workspace_root to fail, got %v", err)
	}
}

func TestLoadConfigDataAcceptsMinimalValidConfig(t *testing.T) {
	dir := t.TempDir()
	config, err := LoadConfigData([]byte(`{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "` + escapeJSON(dir) + `",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`))
	if err != nil {
		t.Fatalf("LoadConfigData returned error: %v", err)
	}
	if config.MachineID != "machine-123" {
		t.Fatalf("unexpected machine ID: %s", config.MachineID)
	}
}

func TestLoadConfigDataRejectsUnknownTopLevelField(t *testing.T) {
	_, err := LoadConfigData([]byte(`{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"sharedToken": "typo",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "` + escapeJSON(t.TempDir()) + `",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestLoadConfigDataRejectsUnknownNestedJobField(t *testing.T) {
	_, err := LoadConfigData([]byte(`{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "` + escapeJSON(t.TempDir()) + `",
			"command": "/usr/bin/true",
			"timeout_seconds": 10,
			"allowedRepository": "typo"
		}]
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestLoadConfigDataRejectsTrailingJSON(t *testing.T) {
	_, err := LoadConfigData([]byte(`{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "` + escapeJSON(t.TempDir()) + `",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	} {"listen_address":"127.0.0.1:8189"}`))
	if err == nil || !strings.Contains(err.Error(), "one JSON object") {
		t.Fatalf("expected trailing JSON error, got %v", err)
	}
}

func TestLoadConfigRejectsRelativeWorkingDirectory(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "relative/path",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected relative working_directory to fail")
	}
}

func TestLoadConfigRejectsNonHTTPCIEndpoint(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"ci_registration_url": "file:///tmp/transwarp-register.json",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected non-http CI endpoint to fail")
	}
}

func TestLoadConfigAcceptsSecureAndLoopbackCIEndpoints(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"ci_registration_url": "https://ci.example.com/transwarp/register",
		"ci_heartbeat_url": "https://ci.example.com/transwarp/heartbeat",
		"ci_deregistration_url": "https://ci.example.com/transwarp/deregister",
		"registration_token": "registration-token",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("expected https CI endpoints to pass: %v", err)
	}

	path = writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"ci_registration_url": "http://127.0.0.1:8288/transwarp/register",
		"ci_heartbeat_url": "http://localhost:8288/transwarp/heartbeat",
		"ci_deregistration_url": "http://[::1]:8288/transwarp/deregister",
		"registration_token": "registration-token",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("expected loopback http CI endpoints to pass: %v", err)
	}
}

func TestLoadConfigRequiresRegistrationTokenForCIEndpoints(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields string
	}{
		{name: "registration", fields: `"ci_registration_url": "https://ci.example.com/transwarp/register", "ci_deregistration_url": "https://ci.example.com/transwarp/deregister",`},
		{name: "heartbeat", fields: `"ci_registration_url": "https://ci.example.com/transwarp/register", "ci_heartbeat_url": "https://ci.example.com/transwarp/heartbeat", "ci_deregistration_url": "https://ci.example.com/transwarp/deregister",`},
		{name: "deregistration", fields: `"ci_registration_url": "https://ci.example.com/transwarp/register", "ci_deregistration_url": "https://ci.example.com/transwarp/deregister",`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, `{
				"listen_address": "127.0.0.1:8188",
				"machine_id": "machine-123",
				"machine_name": "Mac",
				"shared_token": "secret",
				`+test.fields+`
				"tunnel": {"mode": "off"},
				"jobs": [{
					"id": "build",
					"label": "Build",
					"working_directory": "`+escapeJSON(t.TempDir())+`",
					"command": "/usr/bin/true",
					"timeout_seconds": 10
				}]
			}`)

			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "registration_token") {
				t.Fatalf("expected missing registration_token to fail, got %v", err)
			}
		})
	}
}

func TestLoadConfigRequiresDeregistrationURLForRegistrationLifecycle(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"ci_registration_url": "https://ci.example.com/transwarp/register",
		"registration_token": "registration-token",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "ci_deregistration_url") {
		t.Fatalf("expected missing ci_deregistration_url to fail, got %v", err)
	}
}

func TestLoadConfigRequiresRegistrationURLForLifecycleEndpoints(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
	}{
		{name: "heartbeat", field: `"ci_heartbeat_url": "https://ci.example.com/transwarp/heartbeat",`},
		{name: "deregistration", field: `"ci_deregistration_url": "https://ci.example.com/transwarp/deregister",`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, `{
				"listen_address": "127.0.0.1:8188",
				"machine_id": "machine-123",
				"machine_name": "Mac",
				"shared_token": "secret",
				`+test.field+`
				"registration_token": "registration-token",
				"tunnel": {"mode": "off"},
				"jobs": [{
					"id": "build",
					"label": "Build",
					"working_directory": "`+escapeJSON(t.TempDir())+`",
					"command": "/usr/bin/true",
					"timeout_seconds": 10
				}]
			}`)

			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "ci_registration_url") {
				t.Fatalf("expected missing ci_registration_url to fail, got %v", err)
			}
		})
	}
}

func TestLoadConfigRejectsHeaderControlCharacters(t *testing.T) {
	for _, test := range []struct {
		name      string
		fields    string
		wantError string
	}{
		{
			name:      "shared token",
			fields:    `"shared_token": "secret\nInjected: yes",`,
			wantError: "shared_token",
		},
		{
			name: "registration token",
			fields: `"shared_token": "secret",
				"ci_registration_url": "https://ci.example.com/transwarp/register",
				"ci_deregistration_url": "https://ci.example.com/transwarp/deregister",
				"registration_token": "registration-token\nInjected: yes",`,
			wantError: "registration_token",
		},
		{
			name: "CI Access client ID",
			fields: `"shared_token": "secret",
				"ci_access_client_id": "access-id\nInjected: yes",
				"ci_access_client_secret": "access-secret",`,
			wantError: "ci_access_client_id",
		},
		{
			name: "CI Access client secret",
			fields: `"shared_token": "secret",
				"ci_access_client_id": "access-id",
				"ci_access_client_secret": "access-secret\nInjected: yes",`,
			wantError: "ci_access_client_secret",
		},
		{
			name: "Runner Access client ID",
			fields: `"shared_token": "secret",
				"runner_access_client_id": "access-id\nInjected: yes",
				"runner_access_client_secret": "access-secret",`,
			wantError: "runner_access_client_id",
		},
		{
			name: "Runner Access client secret",
			fields: `"shared_token": "secret",
				"runner_access_client_id": "access-id",
				"runner_access_client_secret": "access-secret\nInjected: yes",`,
			wantError: "runner_access_client_secret",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, `{
				"listen_address": "127.0.0.1:8188",
				"machine_id": "machine-123",
				"machine_name": "Mac",
				`+test.fields+`
				"tunnel": {"mode": "off"},
				"jobs": [{
					"id": "build",
					"label": "Build",
					"working_directory": "`+escapeJSON(t.TempDir())+`",
					"command": "/usr/bin/true",
					"timeout_seconds": 10
				}]
			}`)

			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected %s header value error, got %v", test.wantError, err)
			}
		})
	}
}

func TestLoadConfigRejectsRemoteHTTPCIEndpoint(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"ci_registration_url": "http://ci.example.com/transwarp/register",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "https unless it targets local loopback") {
		t.Fatalf("expected remote http CI endpoint to fail, got %v", err)
	}
}

func TestLoadConfigRejectsCIEndpointCredentials(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"ci_registration_url": "https://user:password@ci.example.com/transwarp/register",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected CI endpoint credentials to fail, got %v", err)
	}
}

func TestLoadConfigRejectsCIEndpointQueryOrFragment(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
	}{
		{name: "query", url: "https://ci.example.com/transwarp/register?token=secret"},
		{name: "fragment", url: "https://ci.example.com/transwarp/register#runner"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, `{
				"listen_address": "127.0.0.1:8188",
				"machine_id": "machine-123",
				"machine_name": "Mac",
				"shared_token": "secret",
				"ci_registration_url": "`+test.url+`",
				"tunnel": {"mode": "off"},
				"jobs": [{
					"id": "build",
					"label": "Build",
					"working_directory": "`+escapeJSON(t.TempDir())+`",
					"command": "/usr/bin/true",
					"timeout_seconds": 10
				}]
			}`)

			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "query or fragment") {
				t.Fatalf("expected CI endpoint query/fragment to fail, got %v", err)
			}
		})
	}
}

func TestLoadConfigAcceptsAllowedReportOrigins(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"allowed_report_origins": ["https://ci.example.com", "http://127.0.0.1:8288"],
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected allowed report origins to pass: %v", err)
	}
	if len(config.AllowedReportOrigins) != 2 {
		t.Fatalf("unexpected allowed report origins: %+v", config.AllowedReportOrigins)
	}
	if err := config.ValidateReportURL("https://ci.example.com/transwarp/result"); err != nil {
		t.Fatalf("expected allowed report URL to pass: %v", err)
	}
}

func TestLoadConfigRejectsAllowedReportOriginWithPath(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"allowed_report_origins": ["https://ci.example.com/transwarp/result"],
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected allowed report origin with path to fail")
	}
}

func TestLoadConfigRejectsAllowedReportOriginCredentials(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"allowed_report_origins": ["https://user:password@ci.example.com"],
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected allowed report origin credentials to fail, got %v", err)
	}
}

func TestLoadConfigRejectsRemoteHTTPAllowedReportOrigin(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"allowed_report_origins": ["http://ci.example.com"],
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "https unless it targets local loopback") {
		t.Fatalf("expected remote http allowed report origin to fail, got %v", err)
	}
}

func TestValidateReportURLRequiresConfiguredOrigin(t *testing.T) {
	config := Config{}

	if err := config.ValidateReportURL("https://ci.example.com/transwarp/result"); err == nil {
		t.Fatal("expected report URL without an allowed origin to fail")
	}
}

func TestValidateReportURLRejectsCredentials(t *testing.T) {
	config := Config{AllowedReportOrigins: []string{"https://ci.example.com"}}

	if err := config.ValidateReportURL("https://user:password@ci.example.com/transwarp/result"); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected report URL credentials to fail, got %v", err)
	}
}

func TestValidateReportURLRejectsRemoteHTTP(t *testing.T) {
	config := Config{CIRegistrationURL: "http://ci.example.com/transwarp/register"}

	if err := config.ValidateReportURL("http://ci.example.com/transwarp/result"); err == nil || !strings.Contains(err.Error(), "https unless it targets local loopback") {
		t.Fatalf("expected remote http report URL to fail, got %v", err)
	}
}

func TestValidateReportURLRejectsQueryOrFragment(t *testing.T) {
	config := Config{AllowedReportOrigins: []string{"https://ci.example.com"}}
	tests := []string{
		"https://ci.example.com/transwarp/result?token=query-secret",
		"https://ci.example.com/transwarp/result#receipt",
	}
	for _, reportURL := range tests {
		if err := config.ValidateReportURL(reportURL); err == nil || !strings.Contains(err.Error(), "query or fragment") {
			t.Fatalf("expected query or fragment rejection for %q, got %v", reportURL, err)
		}
	}
}

func TestLoadConfigRequiresCompleteCIAccessCredentials(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"ci_access_client_id": "access-id",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "ci_access_client_id") {
		t.Fatalf("expected incomplete CI Access credentials to fail, got %v", err)
	}
}

func TestLoadConfigRequiresCompleteRunnerAccessCredentials(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"runner_access_client_id": "access-id",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "runner_access_client_id") {
		t.Fatalf("expected incomplete runner Access credentials to fail, got %v", err)
	}
}

func TestLoadConfigRequiresCheckoutForCheckoutAuthorizationHeader(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"checkout_authorization_header": "Authorization: Bearer local-token",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "checkout_authorization_header requires checkout") {
		t.Fatalf("expected checkout auth on non-checkout job to fail, got %v", err)
	}
}

func TestLoadConfigRejectsCheckoutAllowedRepositoryCredentials(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"checkout": true,
			"allowed_repositories": ["https://token:secret@github.com/example/app.git"],
			"checkout_authorization_header": "Authorization: Bearer local-token",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "repo_url must not include credentials") {
		t.Fatalf("expected checkout repo credentials to fail, got %v", err)
	}
}

func TestLoadConfigRejectsCheckoutAllowedRepositoryQueryOrFragment(t *testing.T) {
	for _, test := range []struct {
		name       string
		repository string
	}{
		{name: "query", repository: "https://github.com/example/app.git?token=secret"},
		{name: "fragment", repository: "https://github.com/example/app.git#token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, `{
				"listen_address": "127.0.0.1:8188",
				"machine_id": "machine-123",
				"machine_name": "Mac",
				"shared_token": "secret",
				"tunnel": {"mode": "off"},
				"jobs": [{
					"id": "build",
					"label": "Build",
					"checkout": true,
					"allowed_repositories": ["`+test.repository+`"],
					"checkout_authorization_header": "Authorization: Bearer local-token",
					"command": "/usr/bin/true",
					"timeout_seconds": 10
				}]
			}`)

			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "repo_url must not include query or fragment") {
				t.Fatalf("expected checkout repo query/fragment to fail, got %v", err)
			}
		})
	}
}

func TestLoadConfigRejectsMultilineCheckoutAuthorizationHeader(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "off"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"checkout": true,
			"allowed_repositories": ["https://github.com/example/app.git"],
			"checkout_authorization_header": "Authorization: Bearer one\nInjected: two",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "single HTTP header value") {
		t.Fatalf("expected multiline checkout auth header to fail, got %v", err)
	}
}

func TestLoadConfigResolvesKeychainReferences(t *testing.T) {
	original := lookupKeychainSecret
	lookupKeychainSecret = func(service string, account string) (string, error) {
		return service + ":" + account, nil
	}
	defer func() {
		lookupKeychainSecret = original
	}()

	dir := t.TempDir()
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "keychain://co.charliewil.transwarp/machine-123/shared_token",
		"registration_token": "keychain://co.charliewil.transwarp/machine-123/registration_token",
		"ci_access_client_id": "access-id",
		"ci_access_client_secret": "keychain://co.charliewil.transwarp/machine-123/ci_access_client_secret",
		"runner_access_client_id": "runner-access-id",
		"runner_access_client_secret": "keychain://co.charliewil.transwarp/machine-123/runner_access_client_secret",
		"tunnel": {
			"mode": "off",
			"token": "keychain://co.charliewil.transwarp/machine-123/cloudflare_tunnel_token"
		},
		"jobs": [{
				"id": "build",
				"label": "Build",
				"working_directory": "`+escapeJSON(dir)+`",
				"checkout": true,
				"allowed_repositories": ["https://github.com/example/app.git"],
				"checkout_authorization_header": "keychain://co.charliewil.transwarp/machine-123/jobs/build/checkout_authorization_header",
				"command": "/usr/bin/true",
				"environment": {
					"MATCH_PASSWORD": "keychain://co.charliewil.transwarp/machine-123/jobs/build/MATCH_PASSWORD"
			},
			"timeout_seconds": 10
		}]
	}`)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if config.SharedToken != "co.charliewil.transwarp:machine-123/shared_token" {
		t.Fatalf("unexpected shared token: %s", config.SharedToken)
	}
	if config.CIAccessClientSecret != "co.charliewil.transwarp:machine-123/ci_access_client_secret" {
		t.Fatalf("unexpected CI Access secret: %s", config.CIAccessClientSecret)
	}
	if config.RunnerAccessClientSecret != "co.charliewil.transwarp:machine-123/runner_access_client_secret" {
		t.Fatalf("unexpected runner Access secret: %s", config.RunnerAccessClientSecret)
	}
	if config.Jobs[0].CheckoutAuthorizationHeader != "co.charliewil.transwarp:machine-123/jobs/build/checkout_authorization_header" {
		t.Fatalf("unexpected checkout auth header: %s", config.Jobs[0].CheckoutAuthorizationHeader)
	}
	if config.Jobs[0].Environment["MATCH_PASSWORD"] != "co.charliewil.transwarp:machine-123/jobs/build/MATCH_PASSWORD" {
		t.Fatalf("unexpected env secret: %s", config.Jobs[0].Environment["MATCH_PASSWORD"])
	}
}

func TestLoadConfigRequiresNamedTunnelPublicURL(t *testing.T) {
	path := writeConfig(t, `{
		"listen_address": "127.0.0.1:8188",
		"machine_id": "machine-123",
		"machine_name": "Mac",
		"shared_token": "secret",
		"tunnel": {"mode": "named", "token": "tunnel-token"},
		"jobs": [{
			"id": "build",
			"label": "Build",
			"working_directory": "`+escapeJSON(t.TempDir())+`",
			"command": "/usr/bin/true",
			"timeout_seconds": 10
		}]
	}`)

	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "tunnel public_url is required when tunnel mode is named") {
		t.Fatalf("expected named tunnel without public_url to fail, got %v", err)
	}
}

func TestLoadConfigValidatesNamedTunnelPublicURL(t *testing.T) {
	tests := []struct {
		name      string
		publicURL string
		wantError string
	}{
		{
			name:      "allows base HTTPS URL",
			publicURL: "https://transwarp.example.com",
		},
		{
			name:      "allows trailing slash",
			publicURL: "https://transwarp.example.com/",
		},
		{
			name:      "rejects HTTP",
			publicURL: "http://transwarp.example.com",
			wantError: "tunnel public_url must use https",
		},
		{
			name:      "rejects credentials",
			publicURL: "https://user:pass@transwarp.example.com",
			wantError: "tunnel public_url must not include credentials",
		},
		{
			name:      "rejects path",
			publicURL: "https://transwarp.example.com/status",
			wantError: "tunnel public_url must be a base URL",
		},
		{
			name:      "rejects query",
			publicURL: "https://transwarp.example.com?token=secret",
			wantError: "tunnel public_url must be a base URL",
		},
		{
			name:      "rejects fragment",
			publicURL: "https://transwarp.example.com#runner",
			wantError: "tunnel public_url must be a base URL",
		},
		{
			name:      "rejects missing host",
			publicURL: "https:///transwarp",
			wantError: "tunnel public_url must include a host",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, `{
				"listen_address": "127.0.0.1:8188",
				"machine_id": "machine-123",
				"machine_name": "Mac",
				"shared_token": "secret",
				"tunnel": {"mode": "named", "token": "tunnel-token", "public_url": "`+test.publicURL+`"},
				"jobs": [{
					"id": "build",
					"label": "Build",
					"working_directory": "`+escapeJSON(t.TempDir())+`",
					"command": "/usr/bin/true",
					"timeout_seconds": 10
				}]
			}`)

			_, err := LoadConfig(path)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("LoadConfig returned error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func escapeJSON(value string) string {
	escaped := ""
	for _, char := range value {
		if char == '\\' {
			escaped += "\\\\"
			continue
		}
		if char == '"' {
			escaped += "\\\""
			continue
		}
		if char == '\n' {
			escaped += "\\n"
			continue
		}
		if char == '\r' {
			escaped += "\\r"
			continue
		}
		if char == '\t' {
			escaped += "\\t"
			continue
		}
		escaped += string(char)
	}
	return escaped
}

package agent

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestDetectCapabilitiesCollectsAppleSiliconMacFacts(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS capability probes are only available on darwin")
	}

	originalRun := runCapabilityCommand
	originalCached := defaultCapabilitiesCached
	originalValue := defaultCapabilitiesValue
	defer func() {
		runCapabilityCommand = originalRun
		defaultCapabilitiesCached = originalCached
		defaultCapabilitiesValue = originalValue
	}()

	runCapabilityCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		switch command {
		case "/usr/bin/sw_vers -productVersion":
			return []byte("15.6\n"), nil
		case "/usr/sbin/sysctl -n machdep.cpu.brand_string":
			return []byte("Apple M3 Max\n"), nil
		case "/usr/sbin/sysctl -n hw.ncpu":
			return []byte("16\n"), nil
		case "/usr/sbin/sysctl -n hw.memsize":
			return []byte("68719476736\n"), nil
		case "/usr/bin/xcodebuild -version":
			return []byte("Xcode 16.4\nBuild version 16F6\n"), nil
		case "/usr/bin/xcode-select -p":
			return []byte("/Applications/Xcode.app/Contents/Developer\n"), nil
		default:
			t.Fatalf("unexpected capability command: %s", command)
			return nil, nil
		}
	}

	capabilities := DetectCapabilities(context.Background())

	if capabilities.OS != "macOS" {
		t.Fatalf("unexpected OS: %+v", capabilities)
	}
	if capabilities.Architecture == "" {
		t.Fatalf("expected architecture: %+v", capabilities)
	}
	if capabilities.OSVersion != "15.6" ||
		capabilities.CPUBrand != "Apple M3 Max" ||
		capabilities.CPUCount != 16 ||
		capabilities.MemoryBytes != 68719476736 ||
		capabilities.XcodeVersion != "Xcode 16.4 (Build version 16F6)" ||
		capabilities.DeveloperDir != "/Applications/Xcode.app/Contents/Developer" {
		t.Fatalf("unexpected capabilities: %+v", capabilities)
	}
}

func TestValidateSupportedHostAcceptsModernAppleSiliconMac(t *testing.T) {
	if err := ValidateSupportedHost(testCapabilities()); err != nil {
		t.Fatalf("expected supported host, got %v", err)
	}
}

func TestValidateSupportedHostRejectsUnsupportedHosts(t *testing.T) {
	tests := []struct {
		name         string
		capabilities Capabilities
		want         string
	}{
		{
			name: "os",
			capabilities: Capabilities{
				OS:           "linux",
				OSVersion:    "15.6",
				Architecture: "arm64",
			},
			want: "modern macOS desktops",
		},
		{
			name: "missing os",
			capabilities: Capabilities{
				OSVersion:    "15.6",
				Architecture: "arm64",
			},
			want: "OS could not be detected",
		},
		{
			name: "architecture",
			capabilities: Capabilities{
				OS:           "macOS",
				OSVersion:    "15.6",
				Architecture: "amd64",
			},
			want: "Apple Silicon Macs",
		},
		{
			name: "missing architecture",
			capabilities: Capabilities{
				OS:        "macOS",
				OSVersion: "15.6",
			},
			want: "architecture could not be detected",
		},
		{
			name: "missing version",
			capabilities: Capabilities{
				OS:           "macOS",
				Architecture: "arm64",
			},
			want: "could not be detected",
		},
		{
			name: "old macOS",
			capabilities: Capabilities{
				OS:           "macOS",
				OSVersion:    "13.6",
				Architecture: "arm64",
			},
			want: "macOS 14 or newer",
		},
		{
			name: "unparseable macOS",
			capabilities: Capabilities{
				OS:           "macOS",
				OSVersion:    "Sonoma",
				Architecture: "arm64",
			},
			want: "could not be parsed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateSupportedHost(test.capabilities)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func testCapabilities() Capabilities {
	return Capabilities{
		OS:           "macOS",
		OSVersion:    "15.6",
		Architecture: "arm64",
		CPUBrand:     "Apple M3 Max",
		CPUCount:     16,
		MemoryBytes:  68719476736,
		XcodeVersion: "Xcode 16.4 (Build version 16F6)",
		DeveloperDir: "/Applications/Xcode.app/Contents/Developer",
	}
}

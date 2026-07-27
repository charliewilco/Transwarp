package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Capabilities struct {
	OS           string `json:"os"`
	OSVersion    string `json:"os_version,omitempty"`
	Architecture string `json:"architecture"`
	CPUBrand     string `json:"cpu_brand,omitempty"`
	CPUCount     int    `json:"cpu_count,omitempty"`
	MemoryBytes  uint64 `json:"memory_bytes,omitempty"`
	XcodeVersion string `json:"xcode_version,omitempty"`
	DeveloperDir string `json:"developer_dir,omitempty"`
}

const minimumSupportedMacOSMajor = 14

var (
	defaultCapabilitiesMu     sync.Mutex
	defaultCapabilitiesCached bool
	defaultCapabilitiesValue  Capabilities
	runCapabilityCommand      = runCapabilityCommandImpl
)

func ensureCapabilities(config Config) Config {
	if !config.Capabilities.Empty() {
		return config
	}
	config.Capabilities = DefaultCapabilities()
	return config
}

func DefaultCapabilities() Capabilities {
	defaultCapabilitiesMu.Lock()
	defer defaultCapabilitiesMu.Unlock()

	if !defaultCapabilitiesCached {
		defaultCapabilitiesValue = DetectCapabilities(context.Background())
		defaultCapabilitiesCached = true
	}
	return defaultCapabilitiesValue
}

func DetectCapabilities(ctx context.Context) Capabilities {
	capabilities := Capabilities{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		CPUCount:     runtime.NumCPU(),
	}
	if runtime.GOOS == "darwin" {
		capabilities.OS = "macOS"
		capabilities.OSVersion = commandOutput(ctx, "/usr/bin/sw_vers", "-productVersion")
		capabilities.CPUBrand = commandOutput(ctx, "/usr/sbin/sysctl", "-n", "machdep.cpu.brand_string")
		capabilities.CPUCount = intValue(commandOutput(ctx, "/usr/sbin/sysctl", "-n", "hw.ncpu"), capabilities.CPUCount)
		capabilities.MemoryBytes = uintValue(commandOutput(ctx, "/usr/sbin/sysctl", "-n", "hw.memsize"))
		capabilities.XcodeVersion = xcodeVersion(commandOutput(ctx, "/usr/bin/xcodebuild", "-version"))
		capabilities.DeveloperDir = commandOutput(ctx, "/usr/bin/xcode-select", "-p")
	}
	return capabilities
}

func ValidateSupportedHost(capabilities Capabilities) error {
	if capabilities.OS == "" {
		return errors.New("host OS could not be detected; Transwarp targets modern macOS desktops")
	}
	if !strings.EqualFold(capabilities.OS, "macOS") && !strings.EqualFold(capabilities.OS, "darwin") {
		return fmt.Errorf("host OS %q is unsupported; Transwarp targets modern macOS desktops", capabilities.OS)
	}
	if capabilities.Architecture == "" {
		return errors.New("host architecture could not be detected; Transwarp targets Apple Silicon Macs")
	}
	if capabilities.Architecture != "arm64" {
		return fmt.Errorf("host architecture %q is unsupported; Transwarp targets Apple Silicon Macs", capabilities.Architecture)
	}
	if capabilities.OSVersion == "" {
		return fmt.Errorf("host macOS version could not be detected; Transwarp requires macOS %d or newer", minimumSupportedMacOSMajor)
	}
	major, ok := macOSMajorVersion(capabilities.OSVersion)
	if !ok {
		return fmt.Errorf("host macOS version %q could not be parsed; Transwarp requires macOS %d or newer", capabilities.OSVersion, minimumSupportedMacOSMajor)
	}
	if major < minimumSupportedMacOSMajor {
		return fmt.Errorf("host macOS version %q is unsupported; Transwarp requires macOS %d or newer", capabilities.OSVersion, minimumSupportedMacOSMajor)
	}
	return nil
}

func (capabilities Capabilities) Empty() bool {
	return strings.TrimSpace(capabilities.OS) == "" &&
		strings.TrimSpace(capabilities.OSVersion) == "" &&
		strings.TrimSpace(capabilities.Architecture) == "" &&
		strings.TrimSpace(capabilities.CPUBrand) == "" &&
		capabilities.CPUCount == 0 &&
		capabilities.MemoryBytes == 0 &&
		strings.TrimSpace(capabilities.XcodeVersion) == "" &&
		strings.TrimSpace(capabilities.DeveloperDir) == ""
}

func commandOutput(ctx context.Context, name string, args ...string) string {
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	output, err := runCapabilityCommand(commandCtx, name, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func runCapabilityCommandImpl(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func xcodeVersion(output string) string {
	lines := nonEmptyLines(output)
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return lines[0]
	default:
		return lines[0] + " (" + lines[1] + ")"
	}
}

func nonEmptyLines(output string) []string {
	lines := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func intValue(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func uintValue(value string) uint64 {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func macOSMajorVersion(version string) (int, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return 0, false
	}
	majorText, _, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return 0, false
	}
	return major, true
}

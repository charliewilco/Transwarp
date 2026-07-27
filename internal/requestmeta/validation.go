package requestmeta

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxRequestIDLength = 256
	maxMachineIDLength = 128
	maxJobIDLength     = 128
	maxBuildIDLength   = 128
	maxRepoURLLength   = 2048
)

func Validate(requestID string, repoURL string, ref string, commit string) error {
	if err := ValidateRequestID(requestID); err != nil {
		return err
	}
	if err := ValidateRepoURL(repoURL); err != nil {
		return err
	}
	if strings.TrimSpace(ref) != "" {
		if err := ValidateCheckoutTarget(ref); err != nil {
			return fmt.Errorf("ref %q is not safe: %w", ref, err)
		}
	}
	if strings.TrimSpace(commit) != "" {
		if err := ValidateCheckoutTarget(commit); err != nil {
			return fmt.Errorf("commit %q is not safe: %w", commit, err)
		}
	}
	return nil
}

func ValidateRequestID(requestID string) error {
	return validateStableIdentifier("request_id", requestID, maxRequestIDLength)
}

func ValidateMachineID(machineID string) error {
	return validateStableIdentifier("machine_id", machineID, maxMachineIDLength)
}

func ValidateJobID(jobID string) error {
	return validateStableIdentifier("job_id", jobID, maxJobIDLength)
}

func ValidateBuildID(buildID string) error {
	return validateStableIdentifier("build_id", buildID, maxBuildIDLength)
}

func validateStableIdentifier(field string, value string, maxLength int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > maxLength {
		return fmt.Errorf("%s is too long", field)
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			continue
		case char >= 'A' && char <= 'Z':
			continue
		case char >= '0' && char <= '9':
			continue
		case char == '-' || char == '_' || char == '.':
			continue
		default:
			return fmt.Errorf("%s must contain only letters, numbers, dots, underscores, or hyphens", field)
		}
	}
	return nil
}

func ValidateRepoURL(repoURL string) error {
	if repoURL == "" {
		return nil
	}
	if len(repoURL) > maxRepoURLLength {
		return errors.New("repo_url is too long")
	}
	for _, char := range repoURL {
		if char < 0x20 || char == 0x7f {
			return errors.New("repo_url must not contain control characters")
		}
	}
	parsed, err := url.Parse(repoURL)
	if err == nil && parsed.User != nil {
		return errors.New("repo_url must not include credentials; configure checkout_authorization_header instead")
	}
	if err == nil && (parsed.RawQuery != "" || parsed.Fragment != "") {
		return errors.New("repo_url must not include query or fragment; configure checkout_authorization_header instead")
	}
	return nil
}

func ValidateCheckoutTarget(target string) error {
	if target == "" {
		return nil
	}
	if len(target) > maxRequestIDLength {
		return errors.New("checkout target is too long")
	}
	if strings.HasPrefix(target, "-") {
		return errors.New("checkout target must not start with '-'")
	}
	if strings.Contains(target, "..") ||
		strings.Contains(target, "//") ||
		strings.Contains(target, "@{") ||
		strings.HasSuffix(target, "/") ||
		strings.HasSuffix(target, ".") ||
		strings.HasSuffix(target, ".lock") {
		return fmt.Errorf("checkout target %q is not a safe ref or commit", target)
	}
	for _, char := range target {
		if char <= 0x20 || char == 0x7f {
			return fmt.Errorf("checkout target %q contains whitespace or control characters", target)
		}
		if strings.ContainsRune(`\:~^?*[`, char) {
			return fmt.Errorf("checkout target %q contains unsupported character %q", target, char)
		}
	}
	return nil
}

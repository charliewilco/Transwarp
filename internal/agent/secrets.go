package agent

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

var lookupKeychainSecret = lookupKeychainSecretWithSecurity

const allowedKeychainService = "co.charliewil.transwarp"

func resolveSecrets(config Config) (Config, error) {
	var err error
	if config.SharedToken, err = resolveSecretValue(config.SharedToken); err != nil {
		return Config{}, fmt.Errorf("shared_token: %w", err)
	}
	if config.RegistrationToken, err = resolveSecretValue(config.RegistrationToken); err != nil {
		return Config{}, fmt.Errorf("registration_token: %w", err)
	}
	if config.CIAccessClientSecret, err = resolveSecretValue(config.CIAccessClientSecret); err != nil {
		return Config{}, fmt.Errorf("ci_access_client_secret: %w", err)
	}
	if config.RunnerAccessClientSecret, err = resolveSecretValue(config.RunnerAccessClientSecret); err != nil {
		return Config{}, fmt.Errorf("runner_access_client_secret: %w", err)
	}
	if config.Tunnel.Token, err = resolveSecretValue(config.Tunnel.Token); err != nil {
		return Config{}, fmt.Errorf("tunnel.token: %w", err)
	}
	for index, value := range config.RedactedValues {
		if config.RedactedValues[index], err = resolveSecretValue(value); err != nil {
			return Config{}, fmt.Errorf("redacted_values[%d]: %w", index, err)
		}
	}

	for jobIndex := range config.Jobs {
		if config.Jobs[jobIndex].CheckoutAuthorizationHeader, err = resolveSecretValue(config.Jobs[jobIndex].CheckoutAuthorizationHeader); err != nil {
			return Config{}, fmt.Errorf("job %q checkout_authorization_header: %w", config.Jobs[jobIndex].ID, err)
		}
		for key, value := range config.Jobs[jobIndex].Environment {
			resolved, err := resolveSecretValue(value)
			if err != nil {
				return Config{}, fmt.Errorf("job %q environment %q: %w", config.Jobs[jobIndex].ID, key, err)
			}
			config.Jobs[jobIndex].Environment[key] = resolved
		}
	}

	return config, nil
}

func resolveSecretValue(value string) (string, error) {
	reference, ok := parseKeychainReference(value)
	if !ok {
		if strings.HasPrefix(strings.TrimSpace(value), "keychain:") {
			return "", fmt.Errorf("invalid keychain secret reference")
		}
		return value, nil
	}
	if reference.service != allowedKeychainService {
		return "", fmt.Errorf("unsupported keychain service %q", reference.service)
	}
	return lookupKeychainSecret(reference.service, reference.account)
}

type keychainReference struct {
	service string
	account string
}

func parseKeychainReference(value string) (keychainReference, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "keychain" || parsed.Host == "" {
		return keychainReference{}, false
	}
	if parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return keychainReference{}, false
	}
	account := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if account == "" {
		return keychainReference{}, false
	}
	unescapedAccount, err := url.PathUnescape(account)
	if err != nil {
		return keychainReference{}, false
	}
	return keychainReference{service: parsed.Hostname(), account: unescapedAccount}, true
}

func lookupKeychainSecretWithSecurity(service string, account string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("keychain secret references require macOS")
	}

	output, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("read keychain item %s/%s: %w", service, account, err)
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

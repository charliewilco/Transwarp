package endpoint

import (
	"fmt"
	"net/url"
	"strings"
)

func ValidateBaseURL(rawURL string, field string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", field, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s must include a host", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be a base URL without a path, query, or fragment", field)
	}
	if parsed.Scheme == "http" && !IsLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("%s must use https unless it targets local loopback", field)
	}
	return nil
}

func IsLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

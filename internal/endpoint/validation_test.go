package endpoint

import (
	"strings"
	"testing"
)

func TestValidateBaseURLAllowsHTTPSAndLoopbackHTTP(t *testing.T) {
	for _, rawURL := range []string{
		"https://transwarp.example.com",
		"https://transwarp.example.com/",
		"http://127.0.0.1:8188",
		"http://localhost:8188",
		"http://[::1]:8188",
	} {
		if err := ValidateBaseURL(rawURL, "base URL"); err != nil {
			t.Fatalf("expected %s to be accepted: %v", rawURL, err)
		}
	}
}

func TestValidateBaseURLRejectsUnsafeEndpoints(t *testing.T) {
	tests := []struct {
		rawURL    string
		wantError string
	}{
		{
			rawURL:    "file:///tmp/transwarp",
			wantError: "must use http or https",
		},
		{
			rawURL:    "https:///transwarp",
			wantError: "must include a host",
		},
		{
			rawURL:    "https://user:password@transwarp.example.com",
			wantError: "must not include credentials",
		},
		{
			rawURL:    "http://transwarp.example.com",
			wantError: "must use https",
		},
		{
			rawURL:    "https://transwarp.example.com/status",
			wantError: "without a path",
		},
		{
			rawURL:    "https://transwarp.example.com?token=secret",
			wantError: "without a path",
		},
		{
			rawURL:    "https://transwarp.example.com#runner",
			wantError: "without a path",
		},
	}

	for _, test := range tests {
		t.Run(test.rawURL, func(t *testing.T) {
			err := ValidateBaseURL(test.rawURL, "base URL")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

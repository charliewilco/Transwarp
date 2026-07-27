package headerutil

import (
	"strings"
	"testing"
)

func TestValidateValueAllowsPrintableHeaderValue(t *testing.T) {
	if err := ValidateValue("Bearer token-123", "token"); err != nil {
		t.Fatalf("ValidateValue returned error: %v", err)
	}
}

func TestValidateValueRejectsControlCharacters(t *testing.T) {
	for _, value := range []string{"token\nInjected: yes", "token\rInjected: yes", "token\x00", "token\x7f"} {
		err := ValidateValue(value, "token")
		if err == nil || !strings.Contains(err.Error(), "single HTTP header value") {
			t.Fatalf("expected header value error for %q, got %v", value, err)
		}
	}
}

func TestValidateLineAllowsSingleHeaderLine(t *testing.T) {
	if err := ValidateLine("Authorization: Bearer token-123", "checkout_authorization_header"); err != nil {
		t.Fatalf("ValidateLine returned error: %v", err)
	}
}

func TestValidateLineRejectsMalformedHeaderLines(t *testing.T) {
	tests := []string{
		"Authorization Bearer token-123",
		": Bearer token-123",
		"Bad Header: token-123",
		"Authorization:",
		"Authorization: token\nInjected: yes",
	}
	for _, line := range tests {
		if err := ValidateLine(line, "checkout_authorization_header"); err == nil {
			t.Fatalf("expected ValidateLine to reject %q", line)
		}
	}
}

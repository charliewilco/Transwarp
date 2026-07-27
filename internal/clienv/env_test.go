package clienv

import (
	"strings"
	"testing"
)

func TestNonNegativeIntParsesEnvironment(t *testing.T) {
	t.Setenv("TRANSWARP_TEST_INT", "12")

	value, err := NonNegativeInt("TRANSWARP_TEST_INT", 0)
	if err != nil {
		t.Fatalf("NonNegativeInt returned error: %v", err)
	}
	if value != 12 {
		t.Fatalf("expected parsed value, got %d", value)
	}
}

func TestNonNegativeIntUsesFallbackWhenUnset(t *testing.T) {
	value, err := NonNegativeInt("TRANSWARP_TEST_INT_UNSET", 7)
	if err != nil {
		t.Fatalf("NonNegativeInt returned error: %v", err)
	}
	if value != 7 {
		t.Fatalf("expected fallback, got %d", value)
	}
}

func TestNonNegativeIntRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "text", value: "twelve", want: "must be an integer"},
		{name: "negative", value: "-1", want: "must not be negative"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TRANSWARP_TEST_INT", test.value)

			_, err := NonNegativeInt("TRANSWARP_TEST_INT", 0)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestUint64ParsesEnvironment(t *testing.T) {
	t.Setenv("TRANSWARP_TEST_UINT", "34359738368")

	value, err := Uint64("TRANSWARP_TEST_UINT", 0)
	if err != nil {
		t.Fatalf("Uint64 returned error: %v", err)
	}
	if value != 34359738368 {
		t.Fatalf("expected parsed value, got %d", value)
	}
}

func TestUint64RejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "text", value: "lots"},
		{name: "negative", value: "-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TRANSWARP_TEST_UINT", test.value)

			_, err := Uint64("TRANSWARP_TEST_UINT", 0)
			if err == nil || !strings.Contains(err.Error(), "must be an unsigned integer") {
				t.Fatalf("expected unsigned integer error, got %v", err)
			}
		})
	}
}

func TestBoolParsesEnvironment(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "false", value: "false", want: false},
		{name: "zero", value: "0", want: false},
		{name: "no", value: "no", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TRANSWARP_TEST_BOOL", test.value)

			value, err := Bool("TRANSWARP_TEST_BOOL", !test.want)
			if err != nil {
				t.Fatalf("Bool returned error: %v", err)
			}
			if value != test.want {
				t.Fatalf("expected %t, got %t", test.want, value)
			}
		})
	}
}

func TestBoolUsesFallbackWhenUnset(t *testing.T) {
	value, err := Bool("TRANSWARP_TEST_BOOL_UNSET", true)
	if err != nil {
		t.Fatalf("Bool returned error: %v", err)
	}
	if !value {
		t.Fatal("expected fallback")
	}
}

func TestBoolRejectsInvalidValues(t *testing.T) {
	t.Setenv("TRANSWARP_TEST_BOOL", "maybe")

	_, err := Bool("TRANSWARP_TEST_BOOL", false)
	if err == nil || !strings.Contains(err.Error(), "must be true") {
		t.Fatalf("expected boolean error, got %v", err)
	}
}

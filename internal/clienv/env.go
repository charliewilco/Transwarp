package clienv

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func NonNegativeInt(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", name, raw)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must not be negative, got %d", name, value)
	}
	return value, nil
}

func Uint64(name string, fallback uint64) (uint64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned integer, got %q", name, raw)
	}
	return value, nil
}

func Bool(name string, fallback bool) (bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	switch strings.TrimSpace(raw) {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true, 1, yes, false, 0, or no, got %q", name, raw)
	}
}

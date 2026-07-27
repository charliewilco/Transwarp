package readiness

import (
	"errors"
	"fmt"
	"os"
)

func ValidateReceiptShape(path string, expectedKind string) error {
	if path == "" {
		return errors.New("evidence path is required")
	}
	if expectedKind == "" {
		return errors.New("expected evidence kind is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var receipt map[string]any
	if err := decodeSingleJSON(data, &receipt, false); err != nil {
		return err
	}
	if receipt["kind"] != expectedKind {
		return fmt.Errorf("kind must be %s", expectedKind)
	}
	if receipt["schema_version"] != float64(1) {
		return errors.New("schema_version must be 1")
	}
	if receipt["status"] != "pass" {
		return errors.New("status must be pass")
	}
	return nil
}

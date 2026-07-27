package headerutil

import "fmt"

func ValidateLine(line string, field string) error {
	name, value, ok := splitHeaderLine(line)
	if !ok {
		return fmt.Errorf("%s must use Header-Name: value format", field)
	}
	if !validHeaderName(name) {
		return fmt.Errorf("%s header name is invalid", field)
	}
	if value == "" {
		return fmt.Errorf("%s header value is required", field)
	}
	return ValidateValue(value, field)
}

func ValidateValue(value string, field string) error {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return fmt.Errorf("%s must be a single HTTP header value", field)
		}
	}
	return nil
}

func splitHeaderLine(line string) (string, string, bool) {
	for index, char := range line {
		if char == ':' {
			return line[:index], trimHeaderWhitespace(line[index+1:]), true
		}
	}
	return "", "", false
}

func trimHeaderWhitespace(value string) string {
	start := 0
	for start < len(value) && (value[start] == ' ' || value[start] == '\t') {
		start++
	}
	end := len(value)
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t') {
		end--
	}
	return value[start:end]
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if !validHeaderNameChar(char) {
			return false
		}
	}
	return true
}

func validHeaderNameChar(char rune) bool {
	if char >= 'A' && char <= 'Z' {
		return true
	}
	if char >= 'a' && char <= 'z' {
		return true
	}
	if char >= '0' && char <= '9' {
		return true
	}
	switch char {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}

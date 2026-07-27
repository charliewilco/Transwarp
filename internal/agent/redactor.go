package agent

import (
	"regexp"
	"sort"
	"strings"
)

type Redactor struct {
	values        []string
	sensitiveKeys []string
}

func NewRedactor(config Config) Redactor {
	values := append([]string{}, config.RedactedValues...)
	keys := []string{}
	values = append(
		values,
		config.SharedToken,
		config.RegistrationToken,
		config.CIAccessClientID,
		config.CIAccessClientSecret,
		config.RunnerAccessClientID,
		config.RunnerAccessClientSecret,
		config.Tunnel.Token,
	)

	for _, job := range config.Jobs {
		values = append(values, job.CheckoutAuthorizationHeader)
		sensitiveKeys := sensitiveKeySet(job.RedactedEnvironmentKeys)
		for key := range sensitiveKeys {
			keys = append(keys, key)
		}
		for key, value := range job.Environment {
			if isSensitiveEnvironmentKey(key) || sensitiveKeys[key] {
				keys = append(keys, key)
				values = append(values, value)
			}
		}
	}

	return Redactor{
		values:        uniqueRedactionValues(values),
		sensitiveKeys: uniqueRedactionKeys(keys),
	}
}

func (redactor Redactor) Redact(message string) string {
	redacted := message
	for _, value := range redactor.values {
		redacted = strings.ReplaceAll(redacted, value, "[redacted]")
	}
	for _, key := range redactor.sensitiveKeys {
		redacted = redactSensitiveAssignment(redacted, key)
	}
	return redacted
}

func (redactor Redactor) WithValues(values ...string) Redactor {
	combined := append([]string{}, redactor.values...)
	combined = append(combined, values...)
	return Redactor{
		values:        uniqueRedactionValues(combined),
		sensitiveKeys: append([]string{}, redactor.sensitiveKeys...),
	}
}

func uniqueRedactionValues(values []string) []string {
	seen := map[string]bool{}
	unique := []string{}

	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) < 4 || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}

	sort.SliceStable(unique, func(i, j int) bool {
		return len(unique[i]) > len(unique[j])
	})
	return unique
}

func uniqueRedactionKeys(keys []string) []string {
	seen := map[string]bool{}
	unique := []string{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, key)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		return len(unique[i]) > len(unique[j])
	})
	return unique
}

func redactSensitiveAssignment(message string, key string) string {
	pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_])(["']?)(` + regexp.QuoteMeta(key) + `)(["']?)(\s*[:=]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^ \t\r\n,;}]+)`)
	return pattern.ReplaceAllStringFunc(message, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) != 7 {
			return match
		}
		value := parts[6]
		switch {
		case strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`):
			value = `"[redacted]"`
		case strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`):
			value = `'[redacted]'`
		default:
			value = "[redacted]"
		}
		return parts[1] + parts[2] + parts[3] + parts[4] + parts[5] + value
	})
}

func sensitiveKeySet(keys []string) map[string]bool {
	set := map[string]bool{}
	for _, key := range keys {
		set[key] = true
	}
	return set
}

func isSensitiveEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range sensitiveEnvironmentSubstringMarkers {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	for _, segment := range environmentKeySegments(upper) {
		if sensitiveEnvironmentSegmentMarkers[segment] {
			return true
		}
	}
	return false
}

var sensitiveEnvironmentSubstringMarkers = []string{
	"TOKEN",
	"SECRET",
	"PASSWORD",
	"PRIVATE_KEY",
	"API_KEY",
	"ACCESS_KEY",
	"SECRET_KEY",
}

var sensitiveEnvironmentSegmentMarkers = map[string]bool{
	"AUTH":          true,
	"AUTHORIZATION": true,
	"CREDENTIAL":    true,
	"CREDENTIALS":   true,
	"KEYCHAIN":      true,
	"CERT":          true,
	"CERTIFICATE":   true,
	"P12":           true,
	"PKCS12":        true,
	"PROFILE":       true,
	"IDENTITY":      true,
	"NOTARY":        true,
	"NOTARIZATION":  true,
	"PASSPHRASE":    true,
	"PASSCODE":      true,
}

func environmentKeySegments(key string) []string {
	return strings.FieldsFunc(key, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
}

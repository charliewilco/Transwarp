package agent

import "testing"

func TestParseKeychainReference(t *testing.T) {
	reference, ok := parseKeychainReference("keychain://co.charliewil.transwarp/machine-123/shared_token")
	if !ok {
		t.Fatal("expected reference to parse")
	}
	if reference.service != "co.charliewil.transwarp" {
		t.Fatalf("unexpected service: %s", reference.service)
	}
	if reference.account != "machine-123/shared_token" {
		t.Fatalf("unexpected account: %s", reference.account)
	}
}

func TestParseKeychainReferenceRejectsAmbiguousURLParts(t *testing.T) {
	cases := []string{
		"keychain://user@co.charliewil.transwarp/machine-123/shared_token",
		"keychain://co.charliewil.transwarp:443/machine-123/shared_token",
		"keychain://co.charliewil.transwarp/machine-123/shared_token?copy=true",
		"keychain://co.charliewil.transwarp/machine-123/shared_token#fragment",
	}
	for _, value := range cases {
		if _, ok := parseKeychainReference(value); ok {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestResolveSecretsUsesKeychainLookup(t *testing.T) {
	original := lookupKeychainSecret
	lookupKeychainSecret = func(service string, account string) (string, error) {
		return service + ":" + account, nil
	}
	defer func() {
		lookupKeychainSecret = original
	}()

	config, err := resolveSecrets(Config{
		SharedToken:              "keychain://co.charliewil.transwarp/machine-123/shared_token",
		RegistrationToken:        "keychain://co.charliewil.transwarp/machine-123/registration_token",
		CIAccessClientSecret:     "keychain://co.charliewil.transwarp/machine-123/ci_access_client_secret",
		RunnerAccessClientSecret: "keychain://co.charliewil.transwarp/machine-123/runner_access_client_secret",
		RedactedValues: []string{
			"keychain://co.charliewil.transwarp/machine-123/redacted_values/0",
			"plain-redaction",
		},
		Tunnel: TunnelConfig{
			Token: "keychain://co.charliewil.transwarp/machine-123/cloudflare_tunnel_token",
		},
		Jobs: []JobConfig{{
			ID: "build",
			Environment: map[string]string{
				"MATCH_PASSWORD": "keychain://co.charliewil.transwarp/machine-123/jobs/build/MATCH_PASSWORD",
				"PLAIN":          "value",
			},
		}},
	})
	if err != nil {
		t.Fatalf("resolveSecrets returned error: %v", err)
	}

	if config.SharedToken != "co.charliewil.transwarp:machine-123/shared_token" {
		t.Fatalf("unexpected shared token: %s", config.SharedToken)
	}
	if config.RegistrationToken != "co.charliewil.transwarp:machine-123/registration_token" {
		t.Fatalf("unexpected registration token: %s", config.RegistrationToken)
	}
	if config.CIAccessClientSecret != "co.charliewil.transwarp:machine-123/ci_access_client_secret" {
		t.Fatalf("unexpected CI Access secret: %s", config.CIAccessClientSecret)
	}
	if config.RunnerAccessClientSecret != "co.charliewil.transwarp:machine-123/runner_access_client_secret" {
		t.Fatalf("unexpected runner Access secret: %s", config.RunnerAccessClientSecret)
	}
	if config.RedactedValues[0] != "co.charliewil.transwarp:machine-123/redacted_values/0" {
		t.Fatalf("unexpected redacted value: %s", config.RedactedValues[0])
	}
	if config.RedactedValues[1] != "plain-redaction" {
		t.Fatalf("unexpected plain redacted value: %s", config.RedactedValues[1])
	}
	if config.Tunnel.Token != "co.charliewil.transwarp:machine-123/cloudflare_tunnel_token" {
		t.Fatalf("unexpected tunnel token: %s", config.Tunnel.Token)
	}
	if config.Jobs[0].Environment["MATCH_PASSWORD"] != "co.charliewil.transwarp:machine-123/jobs/build/MATCH_PASSWORD" {
		t.Fatalf("unexpected env secret: %s", config.Jobs[0].Environment["MATCH_PASSWORD"])
	}
	if config.Jobs[0].Environment["PLAIN"] != "value" {
		t.Fatalf("unexpected plain env: %s", config.Jobs[0].Environment["PLAIN"])
	}
}

func TestResolveSecretsRejectsUnsupportedService(t *testing.T) {
	_, err := resolveSecrets(Config{
		SharedToken: "keychain://example.com/machine-123/shared_token",
	})
	if err == nil {
		t.Fatal("expected unsupported service to fail")
	}
}

func TestResolveSecretsRejectsMalformedKeychainReference(t *testing.T) {
	_, err := resolveSecrets(Config{
		SharedToken: "keychain://co.charliewil.transwarp/machine-123/shared_token?copy=true",
	})
	if err == nil {
		t.Fatal("expected malformed keychain reference to fail")
	}
}

func TestResolveSecretsRejectsMalformedRedactedValueKeychainReference(t *testing.T) {
	_, err := resolveSecrets(Config{
		RedactedValues: []string{"keychain://co.charliewil.transwarp/machine-123/redacted_values/0?copy=true"},
	})
	if err == nil {
		t.Fatal("expected malformed redacted value keychain reference to fail")
	}
}

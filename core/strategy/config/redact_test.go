package config

import (
	"strings"
	"testing"
)

func TestRedactionLeavesOrdinaryKeysAlone(t *testing.T) {
	for _, key := range []string{"password", "API_KEY", "client_secret", "auth-token", "db.pwd"} {
		if !isSecretKey(key) {
			t.Fatalf("%q should be treated as a credential", key)
		}
	}
	for _, key := range []string{"host", "port", "keyboard", "public_url", "timeout"} {
		if isSecretKey(key) {
			t.Fatalf("%q should not be treated as a credential", key)
		}
	}
}

func TestConfigRedactsCredentialValues(t *testing.T) {
	source := strings.Join([]string{
		"db:",
		"  user: appuser",
		"  password: hunter2",
		"  api_key: sk-live-9f3c",
		"  auth_token: t0ps3cret",
	}, "\n")

	joined := joinedText(chunksOf(t, "/p/app.yaml", source))

	for _, secret := range []string{"hunter2", "sk-live-9f3c", "t0ps3cret"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("secret %q reached the index:\n%s", secret, joined)
		}
	}
	for _, key := range []string{"password", "api_key", "auth_token"} {
		if !strings.Contains(joined, key) {
			t.Fatalf("key %q should stay searchable:\n%s", key, joined)
		}
	}
	if !strings.Contains(joined, "user = appuser") {
		t.Fatalf("ordinary settings should be untouched:\n%s", joined)
	}
}

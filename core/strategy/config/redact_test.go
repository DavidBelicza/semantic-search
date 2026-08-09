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

func TestRedactURLCredentialHidesOnlyThePassword(t *testing.T) {
	cases := []struct{ in, want string }{
		{"postgres://appuser:S3cret@db01:5432/app", "postgres://appuser:[redacted]@db01:5432/app"},
		{"redis://:OnlyPass@cache:6379/0", "redis://:[redacted]@cache:6379/0"},
		{"jdbc:postgresql://db01/app", "jdbc:postgresql://db01/app"},
		{"https://example.com/path", "https://example.com/path"},
		{"not a url at all", "not a url at all"},
		{"", ""},
	}

	for _, testCase := range cases {
		if got := redactURLCredential(testCase.in); got != testCase.want {
			t.Fatalf("%q: got %q, want %q", testCase.in, got, testCase.want)
		}
	}
}

func TestConfigRedactsACredentialInsideAConnectionString(t *testing.T) {
	joined := joinedText(chunksOf(t, "/p/app.yaml", "database_url: postgres://u:TOPSECRET@db01/app\n"))

	if strings.Contains(joined, "TOPSECRET") {
		t.Fatalf("the DSN credential reached the index: %s", joined)
	}
	if !strings.Contains(joined, "postgres://u:[redacted]@db01/app") {
		t.Fatalf("want the host kept and the password redacted: %s", joined)
	}
}

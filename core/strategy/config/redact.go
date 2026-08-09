package config

import "regexp"

// Indexing sends chunk text to the embedding endpoint and stores it, so credential values are
// replaced before they leave the parser. The key stays indexed; only the secret is withheld.
// The pattern is deliberately conservative, matching well-known names rather than anything
// containing "key", so ordinary settings are not blanked out.
const redactedValue = "[redacted]"

var secretKeyPattern = regexp.MustCompile(
	`(?i)(password|passwd|pwd|secret|token|credential|api[-_.]?key|access[-_.]?key|private[-_.]?key|client[-_.]?secret|auth[-_.]?token)`,
)

func isSecretKey(key string) bool {
	return secretKeyPattern.MatchString(key)
}

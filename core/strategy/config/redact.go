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

// credentialInURL matches the password of a "scheme://user:password@host" value. Connection
// strings carry a credential in the value rather than under a telling key, so the key pattern
// alone would let them through.
var credentialInURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/?#\s@]*):[^@/?#\s]+@`)

// redactURLCredential hides the password inside a connection string, keeping the scheme, user,
// and host searchable.
func redactURLCredential(value string) string {
	return credentialInURL.ReplaceAllString(value, "${1}:"+redactedValue+"@")
}

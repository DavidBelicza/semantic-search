package textproc

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashText returns the hex-encoded SHA-256 of text, used for chunk content hashes.
func HashText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

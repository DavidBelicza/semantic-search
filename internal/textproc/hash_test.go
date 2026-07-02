package textproc

import "testing"

func TestHashTextReturnsSHA256Hex(t *testing.T) {
	if got := HashText("hello"); got != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("hash mismatch: %q", got)
	}
}

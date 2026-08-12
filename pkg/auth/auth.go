// Package auth provides reusable local secondary authentication.
package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

var validPasswordHashes = map[string]struct{}{
	"f6b81756f5e1032d145de0fe00694691a59b0573d9d8b9248199bd7f7a2417f7": {},
	"5d4d8bea68bb378d5185071cc5d4e1f2a25a4d8f11eb6a4d282261dc49ba1c84": {},
}

// Authenticate prompts for a password without echo and verifies it using the
// same SHA-256 allowlist as fflux-auth.
func Authenticate() error {
	password, err := ReadPassword("Password: ")
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	defer clearBytes(password)

	sum := sha256.Sum256(password)
	if _, ok := validPasswordHashes[hex.EncodeToString(sum[:])]; !ok {
		return fmt.Errorf("authentication failed")
	}
	return nil
}

func clearBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
)

const (
	// LocalIdentityProvider marks credentials that are managed by this API.
	// Future IdP integrations should create identities with a different provider value.
	LocalIdentityProvider = "local"
)

type PasswordHasher struct {
	Pepper string
}

func NewPasswordHasher(pepper string) *PasswordHasher {
	return &PasswordHasher{Pepper: pepper}
}

func (h *PasswordHasher) HashWithNewSalt(password string) (hashHex, saltHex string, err error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", "", fmt.Errorf("generate salt: %w", err)
	}

	saltHex = hex.EncodeToString(salt)
	hashHex = h.HashPassword(password, saltHex)
	return hashHex, saltHex, nil
}

func (h *PasswordHasher) HashPassword(password, saltHex string) string {
	sum := sha256.Sum256([]byte(saltHex + ":" + h.Pepper + ":" + password))
	return hex.EncodeToString(sum[:])
}

func (h *PasswordHasher) VerifyPassword(password, saltHex, expectedHashHex string) bool {
	actual := h.HashPassword(password, saltHex)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedHashHex)) == 1
}

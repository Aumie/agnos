// Package auth holds password hashing and token issuance/verification.
// These are pure, local operations (no DB, no network), so unlike
// Repository/HISClient/EventPublisher elsewhere in this codebase, they're
// called directly rather than hidden behind a mockable interface — there's
// nothing here worth mocking.
package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword bcrypt-hashes password for storage. Never store the
// plaintext.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// ComparePassword reports whether password matches hash, returning a
// non-nil error if it doesn't (bcrypt.ErrMismatchedHashAndPassword) or if
// the hash is malformed.
func ComparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

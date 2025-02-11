package jwt

import (
	"crypto/rand"
)

// GenerateSigningKey generates a cryptographically secure random signing key.
func GenerateSigningKey() ([]byte, error) {
	key := make([]byte, SigningKeyLength)

	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}

	return key, nil
}

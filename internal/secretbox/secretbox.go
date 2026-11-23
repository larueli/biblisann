// Package secretbox seals small secrets (authenticator seeds, third-party API tokens) at rest with
// AES-256-GCM under the instance key BIBLISANN_SECRET_KEY. The nonce is prepended to the ciphertext.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// KeySize is the required key length in bytes.
const KeySize = 32

// ErrKey is returned for a key of the wrong length.
var ErrKey = errors.New("secretbox: the key must be 32 bytes")

func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, ErrKey
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// Seal encrypts and authenticates plain.
func Seal(key, plain []byte) ([]byte, error) {
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plain, nil), nil
}

// Open reverses Seal; tampered or foreign data fails.
func Open(key, sealed []byte) ([]byte, error) {
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < g.NonceSize() {
		return nil, errors.New("secretbox: value too short")
	}
	return g.Open(nil, sealed[:g.NonceSize()], sealed[g.NonceSize():], nil)
}

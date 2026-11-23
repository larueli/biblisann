package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP minimum profile with a modest memory footprint, priority 5).
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// Password policy limits. The upper bound stops hashing-cost denial of service.
const (
	MinPasswordRunes = 10
	MaxPasswordBytes = 256
)

// ErrWeakPassword is returned when a password breaks the policy; the message says why.
type ErrWeakPassword struct{ Reason string }

func (e ErrWeakPassword) Error() string { return "password too weak: " + e.Reason }

// commonPasswords is a tiny deny-list for the most common choices that still pass the length
// rule. Length and uniqueness matter more than composition rules (NIST SP 800-63B).
var commonPasswords = map[string]struct{}{
	"password12": {}, "1234567890": {}, "0123456789": {}, "qwertyuiop": {}, "azertyuiop": {},
	"password123": {}, "iloveyou12": {}, "administrator": {}, "biblisann1": {}, "changeme123": {},
	"letmein1234": {}, "motdepasse": {}, "motdepasse1": {}, "1q2w3e4r5t": {}, "abcdefghij": {},
}

// CheckPassword applies the policy. email and name are rejected as passwords.
func CheckPassword(pw, email, name string) error {
	switch {
	case len(pw) > MaxPasswordBytes:
		return ErrWeakPassword{fmt.Sprintf("at most %d bytes", MaxPasswordBytes)}
	case utf8.RuneCountInString(pw) < MinPasswordRunes:
		return ErrWeakPassword{fmt.Sprintf("at least %d characters", MinPasswordRunes)}
	}
	low := strings.ToLower(pw)
	if _, bad := commonPasswords[low]; bad {
		return ErrWeakPassword{"too common"}
	}
	if low == strings.ToLower(email) || (name != "" && low == strings.ToLower(name)) {
		return ErrWeakPassword{"must not be your email or name"}
	}
	if strings.Count(low, string([]rune(low)[0])) == utf8.RuneCountInString(low) {
		return ErrWeakPassword{"must not be one repeated character"}
	}
	return nil
}

// HashPassword returns a PHC-formatted Argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoryKiB, argonTime,
		argonThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

var errBadHash = errors.New("auth: malformed password hash")

// VerifyPassword checks pw against an encoded hash in constant time.
func VerifyPassword(pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false, errBadHash
	}
	// Refuse parameters far above ours: a poisoned row must not exhaust memory.
	if mem > 1<<20 || t > 10 || p > 16 || mem == 0 || t == 0 || p == 0 {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// Package auth implements sign-in, sessions, API keys, invitations and the
// visibility rules that turn a caller into a store.Viewer.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (OWASP 2024 minimum: m=19 MiB, t=2, p=1; we use 64 MiB).
const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

// ErrBadPassword is returned for a wrong password or an unusable hash.
var ErrBadPassword = errors.New("wrong e-mail or password")

// MinPassword is the one password-length rule for every form (setup, join, change, reset).
const MinPassword = 12

// HashPassword returns an encoded argon2id hash.
func HashPassword(pw string) (string, error) {
	if len(pw) < MinPassword {
		return "", fmt.Errorf("the password must have at least %d characters", MinPassword)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword verifies a password in constant time.
func CheckPassword(encoded, pw string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return ErrBadPassword
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return ErrBadPassword
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return ErrBadPassword
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrBadPassword
	}
	return nil
}

// Token returns a random URL-safe token with a prefix.
func Token(prefix string, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

// Digest is the stored form of an API key or invitation token.
func Digest(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// ID returns a short random id with a prefix (usr_, key_, inv_, ing_ …).
func ID(prefix string) string { return Token(prefix, 9) }

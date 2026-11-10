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

// Password rules: at least 10 characters, nothing else about composition.
// The upper bound only keeps a hostile request from making us hash megabytes.
const (
	MinPasswordLength = 10
	maxPasswordBytes  = 1024
)

// argon2id parameters for new hashes (64 MiB, 3 passes, 1 lane). They are
// stored in the PHC string, so they can change without breaking old hashes.
const (
	argonMemoryKiB = 64 * 1024
	argonTime      = 3
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// Upper bounds accepted when verifying a stored hash, so a corrupted row can
// not make a login allocate gigabytes.
const (
	maxArgonMemoryKiB = 512 * 1024
	maxArgonTime      = 12
	maxArgonThreads   = 16
)

// CheckPasswordRules returns a message for a person when pw is not allowed,
// or "" when it is fine.
func CheckPasswordRules(pw string) string {
	switch {
	case len(pw) > maxPasswordBytes:
		return "Use a password shorter than 1024 bytes."
	case utf8.RuneCountInString(pw) < MinPasswordLength:
		return "Use at least 10 characters."
	}
	return ""
}

// HashPassword returns an argon2id hash of pw in PHC string format:
// $argon2id$v=19$m=65536,t=3,p=1$<salt>$<hash> (unpadded base64).
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

var errBadHash = errors.New("auth: malformed password hash")

// VerifyPassword reports whether pw matches the PHC hash, comparing in
// constant time. A malformed hash is an error, never a match.
func VerifyPassword(pw, phc string) (bool, error) {
	parts := strings.Split(phc, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil ||
		m == 0 || t == 0 || p == 0 || m > maxArgonMemoryKiB || t > maxArgonTime || p > maxArgonThreads {
		return false, errBadHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return false, errBadHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is verified against when a login names an unknown email, so
// "no such user" costs the same time as "wrong password".
var dummyHash = func() string {
	h, err := HashPassword("dummy password for timing")
	if err != nil {
		panic(err)
	}
	return h
}()

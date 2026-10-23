package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id with OWASP's minimum: 19 MiB, 2 passes, 1 lane. Enough to make
// guessing slow without straining a small Hub pod.
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

// MinPasswordLength is the shortest password a local user may set.
const MinPasswordLength = 12

var errBadHash = errors.New("unreadable password hash")

// hashPassword returns a PHC string: $argon2id$v=19$m=…,t=…,p=…$salt$key.
func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("make a salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// checkPassword compares in constant time, with the parameters stored in
// the hash, so they can be raised later without breaking old passwords.
func checkPassword(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	var mem, iters uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iters, &threads); err != nil {
		return false, errBadHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, iters, mem, threads, uint32(len(want))) //nolint:gosec // the key length comes from our own hash
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// randomString is n random bytes, URL-safe.
func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newSecret makes a token or credential for a cluster: "<cluster>.<random>".
// The cluster part finds the record; only the hash of the whole is stored.
func newSecret(clusterID string) (secret, hash string, err error) {
	r, err := randomString(32)
	if err != nil {
		return "", "", err
	}
	secret = clusterID + "." + r
	return secret, hashSecret(secret), nil
}

// splitSecret returns the cluster a token or credential claims to be for.
func splitSecret(secret string) (clusterID string, ok bool) {
	id, rest, ok := strings.Cut(secret, ".")
	return id, ok && id != "" && rest != ""
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// sameHash compares a stored hash with a presented secret, in constant time.
func sameHash(stored []byte, secret string) bool {
	return len(stored) > 0 && subtle.ConstantTimeCompare(stored, []byte(hashSecret(secret))) == 1
}

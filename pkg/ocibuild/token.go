package ocibuild

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
)

// TokenBytes is the number of random bytes in a build token before hex encoding.
const TokenBytes = 32

// NewToken returns a new build token: TokenBytes from crypto/rand, hex encoded.
func NewToken() (string, error) {
	var b [TokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate build token: %w", err)
	}

	return hex.EncodeToString(b[:]), nil
}

// HashToken returns the lowercase hex SHA-256 of token. Only the hash is stored in Version status, so a status
// reader cannot use it to call the broker.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokenMatches reports whether token hashes to wantHash. The comparison runs in constant time over the hashes.
func TokenMatches(token, wantHash string) bool {
	got := HashToken(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(wantHash)) == 1
}

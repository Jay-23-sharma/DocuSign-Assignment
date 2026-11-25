package auth

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashPassword(t *testing.T) {
	const password = "  fixture password  "
	var previousSalt []byte
	for range 2 {
		parts := strings.Split(hashPassword(password), "$")
		if len(parts) != 6 || strings.Join(parts[:4], "$") != "$argon2id$v=19$m=19456,t=2,p=1" {
			t.Fatal("unexpected Argon2id encoding")
		}
		salt, err := base64.RawStdEncoding.DecodeString(parts[4])
		if err != nil || len(salt) != 16 || bytes.Equal(salt, previousSalt) {
			t.Fatal("expected a fresh 16-byte salt")
		}
		previousSalt = salt
		hash, err := base64.RawStdEncoding.DecodeString(parts[5])
		want := argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
		if err != nil || !bytes.Equal(hash, want) {
			t.Fatal("hash does not verify the exact password")
		}
	}
}

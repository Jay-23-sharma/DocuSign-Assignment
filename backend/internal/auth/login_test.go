package auth

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestVerifyPassword(t *testing.T) {
	const password = "  a sender password  "
	hash := hashPassword(password)
	if !verifyPassword(hash, password) || verifyPassword(hash, strings.TrimSpace(password)) {
		t.Fatal("password whitespace handling")
	}
	if !verifyPassword(dummyHash, "dummy verifier fixture") {
		t.Fatal("dummy verifier is invalid")
	}
	salt := []byte("another-fixture-salt")
	enc := base64.RawStdEncoding.EncodeToString
	other := "$argon2id$v=19$m=8192,t=1,p=2$" + enc(salt) + "$" + enc(argon2.IDKey([]byte(password), salt, 1, 8192, 2, 24))
	if !verifyPassword(other, password) {
		t.Fatal("encoded parameters were not honored")
	}
	for _, bad := range []string{"", strings.Replace(hash, "v=19", "v=10", 1), strings.Replace(hash, "p=1", "p=0", 1),
		strings.Replace(hash, "t=2", "t=0", 1), strings.Replace(hash, "19456", "999999", 1), hash + "!", hash + "$extra"} {
		if verifyPassword(bad, password) {
			t.Fatal("accepted malformed hash")
		}
	}
}

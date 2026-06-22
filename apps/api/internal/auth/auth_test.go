package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("HashPassword prefix = %q, want argon2id", hash)
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Fatal("argon2id password did not verify")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("wrong argon2id password verified")
	}
	if PasswordNeedsRehash(hash) {
		t.Fatal("fresh argon2id hash should not need rehash")
	}
	old := strings.Replace(hash, "$m=131072,", "$m=65536,", 1)
	if !PasswordNeedsRehash(old) {
		t.Fatal("old argon2id params should need rehash")
	}
}

func TestHashToken(t *testing.T) {
	hash := HashToken("secret-token")
	if !strings.HasPrefix(hash, "blake3:") {
		t.Fatalf("HashToken prefix = %q, want blake3", hash)
	}
	if strings.Contains(hash, "secret-token") {
		t.Fatal("HashToken returned token material")
	}
}

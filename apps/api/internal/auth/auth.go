package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/argon2"
)

const (
	argonMemory  = 128 * 1024
	argonTime    = 2
	argonThreads = 8
	argonKeyLen  = 32
	saltLen      = 16
)

func HashPassword(plaintext string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(plaintext), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	), nil
}

func CheckPassword(hash, plaintext string) bool {
	if !strings.HasPrefix(hash, "$argon2id$") {
		return false
	}
	ok, _ := checkArgon2id(hash, plaintext)
	return ok
}

func PasswordNeedsRehash(hash string) bool {
	if !strings.HasPrefix(hash, "$argon2id$") {
		return true
	}
	_, memory, time, threads, err := parseArgon2id(hash)
	return err != nil || memory != argonMemory || time != argonTime || threads != argonThreads
}

func checkArgon2id(encoded, plaintext string) (bool, error) {
	parts, memory, time, threads, err := parseArgon2id(encoded)
	if err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(plaintext), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func parseArgon2id(encoded string) ([]string, uint32, uint32, uint8, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return nil, 0, 0, 0, fmt.Errorf("invalid argon2id hash")
	}
	var memory uint32
	var time uint32
	var threads uint8
	for _, param := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(param, "=")
		if !ok {
			return nil, 0, 0, 0, fmt.Errorf("invalid argon2id parameters")
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return nil, 0, 0, 0, err
		}
		switch k {
		case "m":
			memory = uint32(n)
		case "t":
			time = uint32(n)
		case "p":
			threads = uint8(n)
		}
	}
	if memory == 0 || time == 0 || threads == 0 {
		return nil, 0, 0, 0, fmt.Errorf("missing argon2id parameters")
	}
	return parts, memory, time, threads, nil
}

func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(token string) string {
	sum := blake3.Sum256([]byte(token))
	return "blake3:" + hex.EncodeToString(sum[:])
}

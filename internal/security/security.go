package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

func Token(bytes int) string {
	value := make([]byte, bytes)
	_, _ = rand.Read(value)
	return base64.RawURLEncoding.EncodeToString(value)
}

func HashOpaque(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func HashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	value := argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=19456,t=2,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(value))
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory uint64
	var iterations, parallel uint64
	for _, setting := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(setting, "=", 2)
		if len(pair) != 2 {
			return false
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return false
		}
		switch pair[0] {
		case "m":
			memory = value
		case "t":
			iterations = value
		case "p":
			parallel = value
		}
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || memory == 0 || iterations == 0 || parallel == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallel), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func ValidPassword(value string) bool {
	if len(value) < 12 || len(value) > 256 {
		return false
	}
	hasLetter, hasNumber := false, false
	for _, r := range value {
		hasLetter = hasLetter || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
		hasNumber = hasNumber || r >= '0' && r <= '9'
	}
	return hasLetter && hasNumber
}

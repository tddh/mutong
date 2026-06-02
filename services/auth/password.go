package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2Memory      = 64 * 1024
	argon2Iterations  = 3
	argon2Parallelism = 4
	saltLength        = 16
	keyLength         = 32
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, keyLength)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		19, argon2Memory, argon2Iterations, argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))

	return encoded, nil
}

func VerifyPassword(password, encodedHash string) bool {
	params, salt, hash, err := decodeHash(encodedHash)
	if err != nil {
		return false
	}

	otherHash := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, params.keyLength)

	return subtle.ConstantTimeCompare(hash, otherHash) == 1
}

type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	keyLength   uint32
}

func decodeHash(encodedHash string) (*argon2Params, []byte, []byte, error) {
	vals := strings.Split(encodedHash, "$")
	if len(vals) != 6 {
		return nil, nil, nil, fmt.Errorf("invalid hash format")
	}

	if vals[1] != "argon2id" {
		return nil, nil, nil, fmt.Errorf("unsupported algorithm: %s", vals[1])
	}

	var version int
	if _, err := fmt.Sscanf(vals[2], "v=%d", &version); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid version: %w", err)
	}
	if version != 19 {
		return nil, nil, nil, fmt.Errorf("unsupported version: %d", version)
	}

	p := &argon2Params{}
	if _, err := fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(vals[4])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid salt: %w", err)
	}

	hash, err := base64.RawStdEncoding.DecodeString(vals[5])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid hash: %w", err)
	}
	if len(hash) < 16 {
		return nil, nil, nil, fmt.Errorf("hash too short")
	}

	p.keyLength = uint32(len(hash)) //nolint:gosec
	return p, salt, hash, nil
}

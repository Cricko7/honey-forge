package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const passwordPrefix = "$argon2id$v=19$m=65536,t=3,p=4$"

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating password salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)

	return passwordPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func CheckPassword(hash, password string) (bool, error) {
	if strings.HasPrefix(hash, "$2") {
		err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) || errors.Is(err, bcrypt.ErrPasswordTooLong) {
			return false, nil
		}

		return err == nil, err
	}

	if !strings.HasPrefix(hash, passwordPrefix) {
		return false, errors.New("unsupported stored password hash")
	}

	parts := strings.Split(strings.TrimPrefix(hash, passwordPrefix), "$")
	if len(parts) != 2 {
		return false, errors.New("invalid stored password hash")
	}

	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[0])
	want, hashErr := base64.RawStdEncoding.DecodeString(parts[1])
	if saltErr != nil || hashErr != nil || len(salt) != 16 || len(want) != 32 {
		return false, errors.New("invalid stored password hash")
	}

	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)

	return subtle.ConstantTimeCompare(actual, want) == 1, nil
}

func DummyPasswordCheck(password string) {
	// Same Argon2id cost as real credentials, without a per-process secret.
	salt := make([]byte, 16)
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	subtle.ConstantTimeCompare(actual, make([]byte, 32))
}

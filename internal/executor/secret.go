package executor

import (
	"errors"
	"os"
	"strings"
)

func ReadSecretFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("HMAC secret must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("HMAC secret permissions must not allow group or other access")
	}
	secret, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	secret = []byte(strings.TrimSpace(string(secret)))
	if len(secret) < 32 {
		return nil, errors.New("HMAC secret must contain at least 32 bytes")
	}
	return secret, nil
}

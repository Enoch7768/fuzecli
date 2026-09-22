package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func TokenFromEnvironment() string {
	return strings.TrimSpace(os.Getenv("FUZECLI_API_TOKEN"))
}

func RequireExternalToken(addr, token string) error {
	if !isLoopbackAddress(addr) && strings.TrimSpace(token) == "" {
		return errors.New("non-loopback API addresses require FUZECLI_API_TOKEN")
	}
	return nil
}

func isLoopbackAddress(addr string) bool {
	host := strings.TrimSpace(addr)
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = strings.Trim(host[:i], "[]")
	}
	host = strings.ToLower(host)
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
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
	addr = strings.TrimSpace(addr)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = strings.Trim(strings.TrimSpace(addr), "[]")
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

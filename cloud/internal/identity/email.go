package identity

import (
	"errors"
	"net/mail"
	"strings"
)

var ErrEmailNotAllowed = errors.New("email not allowed")

func NormalizeQQEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		return "", ErrEmailNotAllowed
	}
	normalized := strings.ToLower(address.Address)
	local, domain, found := strings.Cut(normalized, "@")
	if !found || local == "" || domain != "qq.com" || strings.Contains(local, "@") {
		return "", ErrEmailNotAllowed
	}
	return normalized, nil
}

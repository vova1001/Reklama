package service

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

var cryptoRand io.Reader = rand.Reader

// randomDigits — n случайных цифр без перекоса (отбрасываем байты ≥ 250).
func randomDigits(r io.Reader, n int) (string, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, n*2)
	for len(out) < n {
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < 250 && len(out) < n {
				out = append(out, '0'+b%10)
			}
		}
	}
	return string(out), nil
}

// randomToken — 32 случайных байта в base64url (для сессий, state и PKCE).
func randomToken(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", errors.Join(errors.New("случайные байты"), err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

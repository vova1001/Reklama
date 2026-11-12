package service

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // TOTP RFC 6238 по умолчанию — HMAC-SHA1 (так работают Google Authenticator и аналоги)
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238): шаг 30 с, 6 цифр, HMAC-SHA1 — совместимо с Google Authenticator, Яндекс Ключом, 1Password и др.
const (
	totpPeriod = 30
	totpDigits = 6
	totpSkew   = 1 // принимаются коды соседних шагов (±30 с) — расхождение часов телефона
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// totpStep — номер шага для момента t.
func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpCode — код для шага (RFC 4226, динамическое усечение).
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// totpMatch — шаг, для которого код подходит (now ± totpSkew); 0, false — не подходит. Сравнение — за
// постоянное время, проверяются все шаги окна.
func totpMatch(secret []byte, code string, now time.Time) (int64, bool) {
	cur := totpStep(now)
	var found int64
	ok := false
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		if hmac.Equal([]byte(totpCode(secret, cur+d)), []byte(code)) && !ok {
			found, ok = cur+d, true
		}
	}
	return found, ok
}

// totpURI — ссылка otpauth:// для приложения (QR-код): издатель и учётная запись.
func totpURI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	v := url.Values{}
	v.Set("secret", b32.EncodeToString(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// normalizeOTP — код из поля: без пробелов и дефисов, нижний регистр.
func normalizeOTP(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer(" ", "", "-", "", " ", "").Replace(s)
}

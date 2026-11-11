// Package secretbox — шифрование токенов площадок AES-256-GCM (реализует service.TokenCipher).
//
// Шифротекст: версия (1 байт) | id ключа (4 байта: начало SHA-256 ключа) | nonce (12 байт) | данные+тег.
// Дополнительные данные (AAD) связывают шифротекст с владельцем: подменить токен одного пользователя на токен
// другого в базе нельзя — расшифровка не пройдёт. Ключи: текущий (TOKEN_ENC_KEY) шифрует и расшифровывает,
// прежние (TOKEN_ENC_KEY_OLD) — только расшифровывают (смена ключа без потери доступов: при обновлении токена
// он перешифровывается текущим).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const version = 1

// ErrDecrypt — шифротекст повреждён, чужой (AAD) или ключа нет.
var ErrDecrypt = errors.New("secretbox: не удалось расшифровать")

type key struct {
	id   [4]byte
	aead cipher.AEAD
}

// Box — шифрование текущим ключом, расшифровка текущим и прежними.
type Box struct {
	cur  key
	old  []key
	rand io.Reader
}

// New — ключи в base64 (стандартный или URL) или hex, ровно 32 байта.
func New(current string, old ...string) (*Box, error) {
	k, err := parseKey(current)
	if err != nil {
		return nil, fmt.Errorf("TOKEN_ENC_KEY: %w", err)
	}
	b := &Box{cur: k, rand: rand.Reader}
	for _, o := range old {
		if strings.TrimSpace(o) == "" {
			continue
		}
		ok, err := parseKey(o)
		if err != nil {
			return nil, fmt.Errorf("TOKEN_ENC_KEY_OLD: %w", err)
		}
		b.old = append(b.old, ok)
	}
	return b, nil
}

// ParseKey проверяет ключ (для настроек).
func ParseKey(s string) error {
	_, err := parseKey(s)
	return err
}

func parseKey(s string) (key, error) {
	raw, err := decodeKey(strings.TrimSpace(s))
	if err != nil {
		return key{}, err
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return key{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return key{}, err
	}
	h := sha256.Sum256(raw)
	var k key
	copy(k.id[:], h[:4])
	k.aead = aead
	return k, nil
}

func decodeKey(s string) ([]byte, error) {
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, errors.New("нужен ключ 32 байта в base64 или hex (openssl rand -base64 32)")
}

// Seal шифрует plain текущим ключом; aad — с чем связан шифротекст.
func (b *Box) Seal(plain, aad []byte) ([]byte, error) {
	nonce := make([]byte, b.cur.aead.NonceSize())
	if _, err := io.ReadFull(b.rand, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+4+len(nonce)+len(plain)+b.cur.aead.Overhead())
	out = append(out, version)
	out = append(out, b.cur.id[:]...)
	out = append(out, nonce...)
	return b.cur.aead.Seal(out, nonce, plain, aad), nil
}

// Open расшифровывает ключом, которым зашифровано (текущим или прежним).
func (b *Box) Open(sealed, aad []byte) ([]byte, error) {
	ns := b.cur.aead.NonceSize()
	if len(sealed) < 1+4+ns+b.cur.aead.Overhead() || sealed[0] != version {
		return nil, ErrDecrypt
	}
	var id [4]byte
	copy(id[:], sealed[1:5])
	for _, k := range append([]key{b.cur}, b.old...) {
		if k.id != id {
			continue
		}
		plain, err := k.aead.Open(nil, sealed[5:5+ns], sealed[5+ns:], aad)
		if err != nil {
			return nil, ErrDecrypt
		}
		return plain, nil
	}
	return nil, ErrDecrypt
}

// Current — зашифровано текущим ключом (иначе стоит перешифровать).
func (b *Box) Current(sealed []byte) bool {
	return len(sealed) >= 5 && sealed[0] == version && [4]byte(sealed[1:5]) == b.cur.id
}

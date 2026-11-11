package secretbox

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpen(t *testing.T) {
	k := newKey(t)
	b, err := New(base64.StdEncoding.EncodeToString(k))
	if err != nil {
		t.Fatal(err)
	}
	aad := []byte("social:1:vk:42")
	ct, err := b.Seal([]byte("token-secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("token-secret")) {
		t.Fatal("открытый текст в шифротексте")
	}
	got, err := b.Open(ct, aad)
	if err != nil || string(got) != "token-secret" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if !b.Current(ct) {
		t.Fatal("должен быть текущим ключом")
	}
	// другой владелец (AAD) — не расшифровать
	if _, err := b.Open(ct, []byte("social:2:vk:42")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("чужой AAD: %v", err)
	}
	// испорченный шифротекст
	bad := append([]byte{}, ct...)
	bad[len(bad)-1] ^= 1
	if _, err := b.Open(bad, aad); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("испорчен: %v", err)
	}
	if _, err := b.Open([]byte{1, 2}, aad); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("короткий: %v", err)
	}
	// два шифрования одного текста различаются (случайный nonce)
	ct2, _ := b.Seal([]byte("token-secret"), aad)
	if bytes.Equal(ct, ct2) {
		t.Fatal("nonce повторился")
	}
}

func TestRotation(t *testing.T) {
	oldK, newK := newKey(t), newKey(t)
	oldBox, _ := New(hex.EncodeToString(oldK))
	ct, _ := oldBox.Seal([]byte("x"), nil)

	b, err := New(base64.RawURLEncoding.EncodeToString(newK), base64.StdEncoding.EncodeToString(oldK))
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.Open(ct, nil)
	if err != nil || string(got) != "x" {
		t.Fatalf("старым ключом: %q %v", got, err)
	}
	if b.Current(ct) {
		t.Fatal("зашифровано старым — не текущий")
	}
	// без старого ключа — не открыть
	only, _ := New(base64.StdEncoding.EncodeToString(newK))
	if _, err := only.Open(ct, nil); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("без старого ключа: %v", err)
	}
}

func TestBadKeys(t *testing.T) {
	for _, k := range []string{"", "short", base64.StdEncoding.EncodeToString(make([]byte, 16)), "zz" + hex.EncodeToString(make([]byte, 31))} {
		if _, err := New(k); err == nil {
			t.Errorf("ключ %q принят", k)
		}
		if ParseKey(k) == nil {
			t.Errorf("ParseKey %q принят", k)
		}
	}
	good := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := New(good, "bad"); err == nil {
		t.Error("плохой старый ключ принят")
	}
	if _, err := New(good, "", " "); err != nil {
		t.Errorf("пустые старые ключи: %v", err)
	}
}

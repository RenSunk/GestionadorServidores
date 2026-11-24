package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	box, err := LoadOrCreate("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	enc, err := box.Encrypt([]byte("-----BEGIN OPENSSH PRIVATE KEY-----"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := box.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "-----BEGIN OPENSSH PRIVATE KEY-----" {
		t.Fatalf("got %q", got)
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	box, _ := LoadOrCreate("", t.TempDir())
	enc, _ := box.Encrypt([]byte("hola"))

	tampered := []byte(enc)
	tampered[len(tampered)-3] ^= 1
	if _, err := box.Decrypt(string(tampered)); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("esperaba ErrInvalidCiphertext, got %v", err)
	}
}

func TestLoadOrCreateReusesKey(t *testing.T) {
	dir := t.TempDir()
	first, _ := LoadOrCreate("", dir)
	enc, _ := first.Encrypt([]byte("persistente"))

	second, err := LoadOrCreate("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Decrypt(enc); err != nil {
		t.Fatalf("la segunda carga debería reusar la misma clave: %v", err)
	}

	info, _ := os.Stat(filepath.Join(dir, "secret.key"))
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permisos de secret.key = %o, esperaba 600", perm)
	}
}

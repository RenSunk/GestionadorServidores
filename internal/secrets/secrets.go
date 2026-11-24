// Package secrets cifra los datos sensibles que se guardan en la base de datos
// (llaves SSH, tokens de Git, variables de entorno) con AES-256-GCM.
//
// La clave maestra se genera automáticamente la primera vez y se guarda en
// DataDir/secret.key. Si se pierde, los secretos guardados NO se pueden recuperar.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const keySize = 32 // AES-256

var ErrInvalidCiphertext = errors.New("secrets: texto cifrado inválido o manipulado")

type Box struct {
	aead cipher.AEAD
}

func New(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("secrets: la clave debe tener %d bytes, tiene %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// LoadOrCreate usa envKey (base64) si viene definida; si no, lee
// dataDir/secret.key o la genera si no existe.
func LoadOrCreate(envKey, dataDir string) (*Box, error) {
	if envKey != "" {
		return fromBase64(envKey, "GESTIONADOR_SECRET_KEY")
	}

	path := filepath.Join(dataDir, "secret.key")
	data, err := os.ReadFile(path)
	if err == nil {
		return fromBase64(strings.TrimSpace(string(data)), path)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("leer %s: %w", path, err)
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	// O_EXCL: si otro proceso la creó al mismo tiempo, fallamos en vez de sobrescribirla.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("crear %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		return nil, fmt.Errorf("escribir %s: %w", path, err)
	}
	slog.Warn("se generó una nueva clave maestra; haz un respaldo, sin ella no se pueden descifrar los secretos", "archivo", path)
	return New(key)
}

func fromBase64(s, source string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("clave maestra en %s no es base64 válido: %w", source, err)
	}
	return New(key)
}

// Encrypt devuelve base64(nonce || ciphertext), listo para guardar como TEXT.
func (b *Box) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (b *Box) Decrypt(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < b.aead.NonceSize() {
		return nil, ErrInvalidCiphertext
	}
	nonce, ciphertext := data[:b.aead.NonceSize()], data[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	return plaintext, nil
}

// Package secret encrypts small values (API tokens) at rest with AES-256-GCM.
// The 32-byte instance key comes from the environment (base64) or from
// <data>/secret.key, which is created on first use with mode 0600.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const keySize = 32

// ErrDecrypt is returned for any value that cannot be opened: wrong key,
// tampered bytes, or a different additional-data binding.
var ErrDecrypt = errors.New("secret: cannot decrypt")

// Box seals and opens values with one key. Safe for concurrent use.
type Box struct{ aead cipher.AEAD }

// New builds a Box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("secret: key must be %d bytes, got %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead}, nil
}

// Load returns the instance key as a Box. envKey (base64, standard or URL
// alphabet, from INVENTORY_SECRET_KEY) wins when non-empty; otherwise the key
// is read from <dataDir>/secret.key, created if missing.
func Load(dataDir, envKey string) (*Box, error) {
	if s := strings.TrimSpace(envKey); s != "" {
		key, err := decodeKey(s)
		if err != nil {
			return nil, errors.New("secret: INVENTORY_SECRET_KEY must be 32 bytes of base64")
		}
		return New(key)
	}
	if dataDir == "" {
		return nil, errors.New("secret: no data directory and no INVENTORY_SECRET_KEY")
	}
	path := filepath.Join(dataDir, "secret.key")
	key, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		switch {
		case errors.Is(err, fs.ErrExist): // another process won the race
			if key, err = os.ReadFile(path); err != nil {
				return nil, err
			}
		case err != nil:
			return nil, err
		default:
			_, werr := f.Write(key)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				os.Remove(path)
				return nil, werr
			}
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("secret: %s must hold exactly %d bytes", path, keySize)
	}
	return New(key)
}

func decodeKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == keySize {
			return b, nil
		}
	}
	return nil, errors.New("bad key")
}

// Seal encrypts plaintext with a fresh random nonce. aad is authenticated but
// not stored: Open must be given the same bytes, which ties the value to the
// row it belongs to. The result is nonce || ciphertext.
func (b *Box) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open reverses Seal. Any failure returns ErrDecrypt.
func (b *Box) Open(sealed, aad []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n+b.aead.Overhead() {
		return nil, ErrDecrypt
	}
	out, err := b.aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return out, nil
}

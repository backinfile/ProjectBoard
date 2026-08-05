package providers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

type Vault struct{ aead cipher.AEAD }

func OpenVault(dataDir string) (*Vault, error) {
	file := filepath.Join(dataDir, "secrets", "master.key")
	key, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return nil, err
		}
		if err = os.WriteFile(file, key, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid ProjectBoard master key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}
func (v *Vault) Seal(plain string) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := v.aead.Seal(nonce, nonce, []byte(plain), []byte("projectboard-provider-secret-v1"))
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}
func (v *Vault) Open(encoded string) (string, error) {
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	size := v.aead.NonceSize()
	if len(sealed) < size {
		return "", fmt.Errorf("invalid encrypted secret")
	}
	plain, err := v.aead.Open(nil, sealed[:size], sealed[size:], []byte("projectboard-provider-secret-v1"))
	return string(plain), err
}

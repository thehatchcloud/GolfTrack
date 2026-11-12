package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// EnvSecretsKey names the variable holding the key that encrypts backup
// credentials at rest. The server operator sets it once per deployment;
// administrators then enter every credential on the settings page (#172).
//
// It must stay the same across deploys: stored credentials cannot be read
// with a different key, and the page asks for them to be entered again.
const EnvSecretsKey = "GOLFTRACK_SECRETS_KEY"

// KeySize is the key length: AES-256.
const KeySize = 32

var (
	// ErrNoKey means GOLFTRACK_SECRETS_KEY is not set.
	ErrNoKey = errors.New(EnvSecretsKey + " is not set")

	// errDecrypt means a stored secret could not be decrypted: the key
	// changed, or the file was altered.
	errDecrypt = errors.New("stored backup credential could not be decrypted")
)

// sealedPrefix marks an encrypted value in the stored file. The version lets
// the format change later without guessing what an old value is.
const sealedPrefix = "enc:v1:"

// Sealer encrypts and decrypts secret fields with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// LoadKey reads GOLFTRACK_SECRETS_KEY: 32 random bytes, base64-encoded, as
// `openssl rand -base64 32` prints them. It returns ErrNoKey when the
// variable is unset, and a descriptive error when it is set but unusable.
func LoadKey() (*Sealer, error) {
	raw := strings.TrimSpace(os.Getenv(EnvSecretsKey))
	if raw == "" {
		return nil, ErrNoKey
	}
	key, err := decodeKey(raw)
	if err != nil {
		return nil, err
	}
	return NewSealer(key)
}

func decodeKey(raw string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(raw); err == nil {
			if len(key) != KeySize {
				return nil, fmt.Errorf("%s must decode to %d bytes, got %d (generate one with `openssl rand -base64 32`)", EnvSecretsKey, KeySize, len(key))
			}
			return key, nil
		}
	}
	return nil, fmt.Errorf("%s is not valid base64 (generate one with `openssl rand -base64 32`)", EnvSecretsKey)
}

// NewSealer builds a Sealer from a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("backup secrets key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts one value with a fresh random nonce. The field name is bound
// in as associated data, so a value cannot be moved to another field of the
// file and still decrypt.
func (s *Sealer) Seal(field, plaintext string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := s.aead.Seal(nonce, nonce, []byte(plaintext), []byte(field))
	return sealedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value Seal produced for the same field.
func (s *Sealer) Open(field, sealed string) (string, error) {
	if !strings.HasPrefix(sealed, sealedPrefix) {
		return "", errDecrypt
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, sealedPrefix))
	if err != nil || len(data) < s.aead.NonceSize() {
		return "", errDecrypt
	}
	nonce, ct := data[:s.aead.NonceSize()], data[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, ct, []byte(field))
	if err != nil {
		return "", errDecrypt
	}
	return string(plain), nil
}

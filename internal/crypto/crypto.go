// Package crypto: chiffrement des secrets (AES-256-GCM), hash des jetons, mots de passe (argon2id).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Box chiffre/déchiffre des secrets avec la clé maître.
type Box struct {
	gcm cipher.AEAD
	key []byte
}

func NewBox(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("clé maître: 32 octets requis")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Box{gcm: g, key: append([]byte(nil), key...)}, nil
}

// Derive renvoie une clé de 32 octets propre à un usage (HMAC-SHA256 de l'étiquette avec la clé maître):
// la clé maître elle-même ne sert jamais directement à signer.
func (b *Box) Derive(label string) []byte {
	m := hmac.New(sha256.New, b.key)
	m.Write([]byte("leassh/derive/v1/" + label))
	return m.Sum(nil)
}

// Seal renvoie nonce || ciphertext.
func (b *Box) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return b.gcm.Seal(nonce, nonce, plain, nil), nil
}

func (b *Box) Open(blob []byte) ([]byte, error) {
	n := b.gcm.NonceSize()
	if len(blob) < n+b.gcm.Overhead() {
		return nil, errors.New("blob chiffré trop court")
	}
	out, err := b.gcm.Open(nil, blob[:n], blob[n:], nil)
	if err != nil {
		return nil, fmt.Errorf("déchiffrement: %w", err)
	}
	return out, nil
}

// TokenPrefix est le préfixe des clés MCP.
const TokenPrefix = "lsh_"

// NewToken génère `lsh_` + 32 octets aléatoires encodés en base62 (43 caractères).
func NewToken() (string, error) {
	max := new(big.Int).Lsh(big.NewInt(1), 256)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("aléa: %w", err)
	}
	out := n.Text(62) // alphabet 0-9a-zA-Z, au plus 43 caractères pour 256 bits
	for len(out) < 43 {
		out = "0" + out
	}
	return TokenPrefix + out, nil
}

// HashToken renvoie le SHA-256 hexadécimal du jeton complet.
func HashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// TokenEqual compare deux hash en temps constant.
func TokenEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// Prefix: 8 premiers caractères affichés pour identification.
func Prefix(tok string) string {
	if len(tok) < 8 {
		return tok
	}
	return tok[:8]
}

const (
	argonTime    = 2
	argonMem     = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("sel: %w", err)
	}
	k := argon2.IDKey([]byte(pw), salt, argonTime, argonMem, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMem, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(k)), nil
}

func CheckPassword(pw, encoded string) bool {
	p := strings.Split(encoded, "$")
	if len(p) != 6 || p[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var th uint8
	if _, err := fmt.Sscanf(p[3], "m=%d,t=%d,p=%d", &m, &t, &th); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(p[4])
	want, err2 := base64.RawStdEncoding.DecodeString(p[5])
	if err1 != nil || err2 != nil {
		return false
	}
	if len(want) == 0 || len(want) > math.MaxInt32 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, th, uint32(len(want))) //nolint:gosec // borné ci-dessus
	return subtle.ConstantTimeCompare(got, want) == 1
}

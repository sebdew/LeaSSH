package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestBoxRoundTrip(t *testing.T) {
	b, err := NewBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := b.Seal([]byte("secret"))
	if bytes.Contains(c, []byte("secret")) {
		t.Fatal("le chiffré contient le clair")
	}
	p, err := b.Open(c)
	if err != nil || string(p) != "secret" {
		t.Fatalf("open: %q %v", p, err)
	}
	c[len(c)-1] ^= 1
	if _, err := b.Open(c); err == nil {
		t.Fatal("altération non détectée")
	}
	if _, err := NewBox([]byte("x")); err == nil {
		t.Fatal("clé courte acceptée")
	}
}

func TestToken(t *testing.T) {
	a, _ := NewToken()
	b, _ := NewToken()
	if a == b || !strings.HasPrefix(a, "lsh_") || len(a) != 4+43 {
		t.Fatalf("jeton inattendu: %q", a)
	}
	if !TokenEqual(HashToken(a), HashToken(a)) || TokenEqual(HashToken(a), HashToken(b)) {
		t.Fatal("comparaison de hash")
	}
	if len(Prefix(a)) != 8 {
		t.Fatal("préfixe")
	}
}

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("correct horse", h) || CheckPassword("wrong", h) || CheckPassword("x", "garbage") {
		t.Fatal("vérification mot de passe")
	}
}

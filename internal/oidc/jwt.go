// Package oidc: client OpenID Connect (Authorization Code + PKCE) et vérification stricte des ID tokens.
// Implémenté avec la seule bibliothèque standard: aucune exécution de commande, aucune dépendance supplémentaire.
package oidc

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	_ "crypto/sha256" // enregistre SHA-256/384/512 pour crypto.Hash.New
	_ "crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Algorithmes de signature acceptés. « none » et HMAC sont volontairement exclus
// (un secret partagé ne doit jamais servir à vérifier un jeton signé par le fournisseur).
var allowedAlgs = map[string]bool{
	"RS256": true, "RS384": true, "RS512": true,
	"PS256": true, "PS384": true, "PS512": true,
	"ES256": true, "ES384": true, "ES512": true,
	"EdDSA": true,
}

// jwk: clé publique d'un JWKS (RFC 7517).
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type parsedKey struct {
	kid string
	alg string // vide = non contraint
	pub crypto.PublicKey
}

func b64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func (k jwk) parse() (parsedKey, error) {
	pk := parsedKey{kid: k.Kid, alg: k.Alg}
	if k.Use != "" && k.Use != "sig" {
		return pk, errors.New("clé non destinée à la signature")
	}
	switch k.Kty {
	case "RSA":
		n, err1 := b64(k.N)
		e, err2 := b64(k.E)
		if err1 != nil || err2 != nil || len(n) < 256 || len(e) == 0 || len(e) > 4 { // 2048 bits minimum
			return pk, errors.New("clé RSA invalide ou trop courte")
		}
		pk.pub = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	case "EC":
		var c elliptic.Curve
		switch k.Crv {
		case "P-256":
			c = elliptic.P256()
		case "P-384":
			c = elliptic.P384()
		case "P-521":
			c = elliptic.P521()
		default:
			return pk, fmt.Errorf("courbe %q non prise en charge", k.Crv)
		}
		x, err1 := b64(k.X)
		y, err2 := b64(k.Y)
		if err1 != nil || err2 != nil {
			return pk, errors.New("clé EC invalide")
		}
		pub := &ecdsa.PublicKey{Curve: c, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !c.IsOnCurve(pub.X, pub.Y) {
			return pk, errors.New("point EC hors courbe")
		}
		pk.pub = pub
	case "OKP":
		if k.Crv != "Ed25519" {
			return pk, fmt.Errorf("courbe %q non prise en charge", k.Crv)
		}
		x, err := b64(k.X)
		if err != nil || len(x) != ed25519.PublicKeySize {
			return pk, errors.New("clé Ed25519 invalide")
		}
		pk.pub = ed25519.PublicKey(x)
	default:
		return pk, fmt.Errorf("type de clé %q non pris en charge", k.Kty)
	}
	return pk, nil
}

// compatible: la clé peut-elle vérifier cet algorithme ?
func (k parsedKey) compatible(alg string) bool {
	if k.alg != "" && k.alg != alg {
		return false
	}
	switch k.pub.(type) {
	case *rsa.PublicKey:
		return strings.HasPrefix(alg, "RS") || strings.HasPrefix(alg, "PS")
	case *ecdsa.PublicKey:
		return strings.HasPrefix(alg, "ES")
	case ed25519.PublicKey:
		return alg == "EdDSA"
	}
	return false
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// splitJWT sépare et décode un JWT compact; la signature n'est pas encore vérifiée.
func splitJWT(raw string) (hdr jwtHeader, payload, sig []byte, signed string, err error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return hdr, nil, nil, "", errors.New("jeton mal formé")
	}
	h, e1 := b64(parts[0])
	payload, e2 := b64(parts[1])
	sig, e3 := b64(parts[2])
	if e1 != nil || e2 != nil || e3 != nil {
		return hdr, nil, nil, "", errors.New("jeton mal formé")
	}
	if err := json.Unmarshal(h, &hdr); err != nil {
		return hdr, nil, nil, "", errors.New("en-tête de jeton invalide")
	}
	return hdr, payload, sig, parts[0] + "." + parts[1], nil
}

func hashFor(alg string) crypto.Hash {
	switch alg[len(alg)-3:] {
	case "384":
		return crypto.SHA384
	case "512":
		return crypto.SHA512
	}
	return crypto.SHA256
}

func (k parsedKey) verify(alg, signed string, sig []byte) bool {
	switch pub := k.pub.(type) {
	case *rsa.PublicKey:
		h := hashFor(alg)
		hh := h.New()
		hh.Write([]byte(signed))
		sum := hh.Sum(nil)
		if strings.HasPrefix(alg, "PS") {
			return rsa.VerifyPSS(pub, h, sum, sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}) == nil
		}
		return rsa.VerifyPKCS1v15(pub, h, sum, sig) == nil
	case *ecdsa.PublicKey:
		size := (pub.Curve.Params().BitSize + 7) / 8
		if len(sig) != 2*size {
			return false
		}
		hh := hashFor(alg).New()
		hh.Write([]byte(signed))
		return ecdsa.Verify(pub, hh.Sum(nil), new(big.Int).SetBytes(sig[:size]), new(big.Int).SetBytes(sig[size:]))
	case ed25519.PublicKey:
		return ed25519.Verify(pub, []byte(signed), sig)
	}
	return false
}

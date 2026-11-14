package catalog

import (
	"errors"
	"regexp"
)

// ErrInvalidSlug: un identifiant lisible (commande, groupe, serveur) suit un format strict.
var ErrInvalidSlug = errors.New("slug invalide: minuscules, chiffres, . _ - (64 caractères max)")

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

// CheckSlug renvoie ErrInvalidSlug si slug n'a pas le format attendu.
func CheckSlug(slug string) error {
	if !slugRe.MatchString(slug) {
		return ErrInvalidSlug
	}
	return nil
}

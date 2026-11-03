// Package web embarque l'interface construite (web/dist) dans le binaire.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist renvoie le système de fichiers de l'interface (racine = contenu de dist).
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}

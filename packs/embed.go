// Package packsdata embarque les packs de commandes fournis avec Leassh.
package packsdata

import "embed"

//go:embed *.yaml
var FS embed.FS

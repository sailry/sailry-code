// Package product owns the identity of the independently released Sailry Code application.
package product

import (
	_ "embed"

	"github.com/sailry/sailry-harness/sdk/product"
)

const (
	ID         = "code"
	Version    = "0.1.0"
	repository = "sailry/sailry-code"
)

//go:embed update-public-key.txt
var publicKeys string

func Desktop() product.Desktop {
	return product.Desktop{
		ID: ID, Name: "Sailry Code", Version: Version,
		Website: "https://sailry.dev", Feedback: "https://github.com/" + repository + "/issues",
		DefaultProfile: "~/.sailry", DefaultEntry: "page:conversation",
		Release: product.Release{
			Source:    "https://api.github.com/repos/" + repository + "/releases",
			Downloads: "https://github.com/" + repository + "/releases/download", PublicKeys: publicKeys,
			Package: product.Package{ID: "ai.sailry.desktop", Name: "Sailry", Executable: "sailry-desktop"},
		},
		HostRelease: product.CodeHost(),
	}
}

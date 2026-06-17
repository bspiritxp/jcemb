package main

import (
	_ "embed"
	"os"

	"github.com/bspiritxp/jcemb/cmd"
)

//go:embed manifest.json
var manifestJSON []byte

func main() {
	cmd.SetManifest(manifestJSON)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

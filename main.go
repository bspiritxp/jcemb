package main

import (
	"os"

	"github.com/bspiritxp/jcemb/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

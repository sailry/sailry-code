package main

import (
	"log"
	"os"

	"github.com/sailry/sailry-code/internal/assembly/codedesktop"
	"github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-code/internal/product"
	"github.com/sailry/sailry-harness/sdk/desktop"
)

func main() {
	if err := desktop.Run(os.Args[1:], desktop.Options{
		Identity: product.Desktop(),
		Assembly: codedesktop.Assembly(),
		Host:     codehost.Assembly(),
	}); err != nil {
		log.Fatal(err)
	}
}

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
	if err := run(os.Args[1:], desktop.Run); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, start func([]string, desktop.Options) error) error {
	return start(args, desktop.Options{
		Identity:   product.Desktop(),
		Assembly:   codedesktop.Assembly(),
		Host:       codehost.Assembly(),
		NativeName: "Sailry Code",
	})
}

package main

import (
	"fmt"
	"os"

	"github.com/sailry/sailry-code/internal/assembly/codehost"
	"github.com/sailry/sailry-code/internal/product"
	"github.com/sailry/sailry-harness/sdk/host"
)

func main() {
	code, err := host.Run(os.Args[1:], host.Options{
		Identity: product.Desktop(),
		Assembly: codehost.Assembly(),
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "sailry-host: "+err.Error())
		code = 1
	}
	os.Exit(code)
}

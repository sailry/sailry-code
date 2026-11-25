package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sailry/sailry-code/internal/product"
	"github.com/sailry/sailry-harness/sdk/release"
)

func main() {
	version := flag.String("version", "", "product version without v")
	output := flag.String("output", "", "output directory")
	flag.Parse()
	if *version != product.Version || *output == "" {
		fail("provide the Code build version and an output directory")
	}
	archives := map[string]string{}
	for _, arg := range flag.Args() {
		target, file, ok := strings.Cut(arg, "=")
		if !ok || target == "" || file == "" || archives[target] != "" {
			fail("expected one target=archive per architecture")
		}
		archives[target] = file
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("SAILRY_UPDATE_SIGNING_KEY")))
	if err != nil {
		fail("invalid signing key encoding")
	}
	if err := release.PublishDesktop(product.Desktop().Release, seed, *version, *output, archives); err != nil {
		fail(err.Error())
	}
}

func fail(message string) { fmt.Fprintln(os.Stderr, "update-feed:", message); os.Exit(1) }

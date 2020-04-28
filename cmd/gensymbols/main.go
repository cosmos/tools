package main

import (
	"fmt"
	"os"

)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage:\n\tgensymbols directory\n")
		os.Exit(1)
	}

	extractor, err := NewPackageExtractor(os.Args[1])
	if err != nil {
		panic(err)
	}

	pkgs, err := extractor.Extract()
	if err != nil {
		panic(err)
	}

	walker := NewWalker(pkgs)
	pkgstypes, err := walker.Extract()
	if err != nil {
		panic(err)
	}

	printer := NewPrinter(pkgstypes, os.Stdout)
	printer.Print()
}

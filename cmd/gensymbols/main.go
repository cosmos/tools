package main

import (
	"os"

)

func main() {
	if len(os.Args) < 2 {
		panic("")
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

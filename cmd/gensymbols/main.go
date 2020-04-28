package main

import (
	"log"
	"os"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("gensymbols: ")
	if len(os.Args) < 2 {
		log.Fatal("Usage:\n\tgensymbols directory\n")
	}

	check := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}

	extractor, err := NewPackageExtractor(os.Args[1])
	check(err)

	pkgs, err := extractor.Extract()
	check(err)

	walker := NewWalker(pkgs)
	pkgstypes, err := walker.Extract()
	check(err)

	printer := NewPrinter(pkgstypes, os.Stdout)
	printer.Print()
}

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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

	dir, err := absPath(os.Args[1])
	check(err)

	pkgs, err := PackageExtractor{dir: dir}.Extract()
	check(err)

	walker := NewWalker(pkgs)
	pkgstypes, err := walker.Extract()
	check(err)

	printer := NewPrinter(pkgstypes, os.Stdout)
	printer.Print()
}

func absPath(dir string) (string, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("invalid directory %s", dir)
	}
	return filepath.Abs(dir)
}

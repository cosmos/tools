package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

var recursive = flag.Bool("r", false, "set this flag if you want to run it recursively")

func main() {
	flag.Parse()

	log.SetFlags(0)
	log.SetPrefix("gensymbols: ")
	validateArgs()

	check := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}

	dir, err := absPath(flag.Arg(0))
	check(err)

	pkgs, err := ExtractPackageNames(dir, *recursive)
	check(err)

	printer := NewPrinter(os.Stdout)
	for _, pkg := range pkgs {
		pkgstypes, err := Extract(pkg)
		check(err)

		printer.Print(pkgstypes)
	}
}

func absPath(dir string) (string, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("invalid directory %s", dir)
	}

	return filepath.Abs(dir)
}

func validateArgs() {
	if len(flag.Args()) != 1 {
		log.Fatal("Usage:\n\tgensymbols [flags] directory\n\t-r\truns the command recursively")
	}
}

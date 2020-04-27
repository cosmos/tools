package parser

import (
	"go/build"

	"golang.org/x/tools/go/packages"
)

type Walker struct {
	packages []Pkg
	context  build.Context
}

func NewWalker(pkgs []Pkg) Walker {
	return Walker{
		packages: pkgs,
		context:  build.Default,
	}
}

func (w Walker) Extract() ([]*packages.Package, error) {
	var foundPackages []*packages.Package
	for _, pkg := range w.packages {
		dir, err := packages.Load(&packages.Config{
			Mode: packages.LoadAllSyntax,
		}, pkg.Dir)
		if err != nil {
			return nil, err
		}

		foundPackages = append(foundPackages, dir...)
	}

	return foundPackages, nil
}

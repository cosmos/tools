package main

import (
	"os"

	"golang.org/x/tools/go/packages"
)

func Extract(pkg Pkg) ([]*packages.Package, error) {
	var foundPackages []*packages.Package
	err := os.Chdir(pkg.Dir)
	if err != nil {
		return nil, err
	}

	dir, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedTypes,
	}, pkg.Dir)
	if err != nil {
		return nil, err
	}

	foundPackages = append(foundPackages, dir...)

	return foundPackages, nil
}

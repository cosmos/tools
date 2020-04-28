package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type PackageExtractor struct {
	dir string
}

func NewPackageExtractor(dir string) (PackageExtractor, error) {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return PackageExtractor{}, fmt.Errorf("invalid directory %s", dir)
	}

	return PackageExtractor{dir: dir}, nil
}

// extract returns all the packages that are detected in the directory.
func (e PackageExtractor) Extract() ([]Pkg, error) {
	cmd := exec.Command("go", "list", "-json",  e.dir)
	cmd.Dir = e.dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error extracting packages: %s", err)
	}
	dec := json.NewDecoder(bytes.NewReader(out))

	var foundPkgs []Pkg
	for {
		var p Pkg
		err := dec.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		foundPkgs = append(foundPkgs, p)
	}

	return foundPkgs, nil
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// extract returns all the packages that are detected in the directory.
func ExtractPackageNames(dir string) ([]Pkg, error) {
	cmd := exec.Command("go", "list", "-json", dir)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(err.Error(), "exit status 1") {
			return nil, fmt.Errorf("no go files found")
		}

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

package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrinter(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	packages, err := Extract(Pkg{
		ImportPath: "github.com/cosmos/api-generator/parser",
		Dir:        dir,
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)

	printer := NewPrinter(buf)
	printer.Print(packages)

	require.Equal(t, `pkg github.com/cosmos/tools/cmd/gensymbols, Dir string
pkg github.com/cosmos/tools/cmd/gensymbols, ImportPath string
pkg github.com/cosmos/tools/cmd/gensymbols, func Extract(main.Pkg) ([]*packages.Package, error)
pkg github.com/cosmos/tools/cmd/gensymbols, func ExtractPackageNames(string, bool) ([]main.Pkg, error)
pkg github.com/cosmos/tools/cmd/gensymbols, func NewPrinter(io.Writer) main.Printer
pkg github.com/cosmos/tools/cmd/gensymbols, method (main.Printer) Features() []string
pkg github.com/cosmos/tools/cmd/gensymbols, method (main.Printer) Print([]*packages.Package)
pkg github.com/cosmos/tools/cmd/gensymbols, type Pkg struct
pkg github.com/cosmos/tools/cmd/gensymbols, type Printer struct
pkg github.com/cosmos/tools/cmd/gensymbols, type Walker struct
`, buf.String())
}

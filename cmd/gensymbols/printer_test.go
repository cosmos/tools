package main

import (
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

	printer := NewPrinter(os.Stdout)
	printer.Print(packages)
}

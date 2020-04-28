package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrinter(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err)

	walker := NewWalker([]Pkg{
		{
			ImportPath: "github.com/cosmos/api-generator/parser",
			Dir:        dir,
		},
	})

	packages, err := walker.Extract()
	require.NoError(t, err)

	printer := NewPrinter(packages, os.Stdout)
	printer.Print()
}

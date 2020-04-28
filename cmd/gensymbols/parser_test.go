package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackageExtractor_ErrorOnFolderUnexisting(t *testing.T) {
	_, err := NewPackageExtractor("unexistingFolder/blah/blah")
	require.Error(t, err)
}

func TestPackageExtractor_HappyPath(t *testing.T) {
	extractor, err := NewPackageExtractor(".")
	require.NoError(t, err)

	packages, err := extractor.Extract()
	require.NoError(t, err)

	require.Len(t, packages, 1)
}

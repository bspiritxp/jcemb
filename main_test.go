package main

import (
	"testing"

	"github.com/bspiritxp/jcemb/cmd"
	"github.com/stretchr/testify/require"
)

func TestManifestIsEmbeddedIntoBinary(t *testing.T) {
	require.NotEmpty(t, manifestJSON)

	manifest, err := cmd.ParseManifest(manifestJSON)
	require.NoError(t, err)
	require.Equal(t, "jcemb", manifest.Name)
	require.NotEmpty(t, manifest.Author)
	require.NotEmpty(t, manifest.Version)
}

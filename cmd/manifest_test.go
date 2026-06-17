package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadManifestRequiresToolMetadata(t *testing.T) {
	manifest, err := LoadManifest()
	require.NoError(t, err)
	require.Equal(t, "jcemb", manifest.Name)
	require.NotEmpty(t, manifest.Description)
	require.NotEmpty(t, manifest.Author)
	require.NotEmpty(t, manifest.Version)
}

package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	manifest, loadErr := LoadManifest()
	require.NoError(t, loadErr)
	require.NotEmpty(t, manifest.Version)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Equal(t, manifest.Version+"\n", buf.String())
}

func TestVersionCommandUsesInjectedManifest(t *testing.T) {
	previous := embeddedManifest
	t.Cleanup(func() {
		embeddedManifest = previous
	})

	SetManifest([]byte(`{
		"name": "jcemb-test",
		"description": "test manifest",
		"author": "test",
		"version": "9.8.7"
	}`))

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Equal(t, "9.8.7\n", buf.String())
}

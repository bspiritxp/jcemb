package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewEmbedCmd(t *testing.T) {
	cmd := NewEmbedCmd()

	require.NotNil(t, cmd)
	require.Equal(t, "embed [path]", cmd.Use)

	flags := cmd.Flags()
	require.NotNil(t, flags.Lookup("type"))
	require.NotNil(t, flags.Lookup("concurccy"))
	require.NotNil(t, flags.Lookup("provider"))
	require.NotNil(t, flags.Lookup("model"))
	require.NotNil(t, flags.Lookup("recursive"))
	require.NotNil(t, flags.Lookup("force"))
}

func TestEmbedHelpShowsFlags(t *testing.T) {
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"embed", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	require.Contains(t, output, "--type")
	require.Contains(t, output, "--concurccy")
	require.Contains(t, output, "--provider")
	require.Contains(t, output, "--model")
	require.Contains(t, output, "--recursive")
	require.Contains(t, output, "--force")
}

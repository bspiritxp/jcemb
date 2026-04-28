package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewScanCmd(t *testing.T) {
	cmd := NewScanCmd()

	require.NotNil(t, cmd)
	require.Equal(t, "scan [path]", cmd.Use)

	flags := cmd.Flags()
	require.NotNil(t, flags.Lookup("type"))
	require.NotNil(t, flags.Lookup("concurccy"))
	require.NotNil(t, flags.Lookup("provider"))
	require.NotNil(t, flags.Lookup("model"))
	require.NotNil(t, flags.Lookup("recursive"))
	require.NotNil(t, flags.Lookup("force"))
}

func TestScanHelpShowsFlags(t *testing.T) {
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"scan", "--help"})

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

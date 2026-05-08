package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewQueryCmd(t *testing.T) {
	cmd := NewQueryCmd()

	require.NotNil(t, cmd)
	require.Equal(t, "query <query-text>", cmd.Use)

	flags := cmd.Flags()
	require.NotNil(t, flags.Lookup("tags"))
	require.NotNil(t, flags.Lookup("file-type"))
	require.NotNil(t, flags.Lookup("limit"))
	require.NotNil(t, flags.Lookup("path"))
	require.NotNil(t, flags.Lookup("format"))
	require.NotNil(t, flags.Lookup("json"))
	require.NotNil(t, flags.Lookup("unique"))
	require.NotNil(t, flags.Lookup("full"))
	require.NotNil(t, flags.Lookup("threshold-alpha"))
	require.NotNil(t, flags.Lookup("threshold-delta"))
	require.NotNil(t, flags.Lookup("mmr-lambda"))
	require.NotNil(t, flags.Lookup("search-window"))
	require.NotNil(t, flags.Lookup("rerank"))
}

func TestQueryHelpShowsFlags(t *testing.T) {
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"query", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	require.Contains(t, output, "--tags")
	require.Contains(t, output, "--file-type")
	require.Contains(t, output, "-t, --file-type")
	require.Contains(t, output, "--limit")
	require.Contains(t, output, "--path")
	require.Contains(t, output, "optional indexed file or directory path to restrict results")
	require.NotContains(t, output, `default "."`)
	require.Contains(t, output, "--format")
	require.Contains(t, output, "text, json, table, tsv, or tsv-z")
	require.Contains(t, output, "--json")
	require.Contains(t, output, "--unique")
	require.Contains(t, output, "--full")
	require.Contains(t, output, "--threshold-alpha")
	require.Contains(t, output, "--threshold-delta")
	require.Contains(t, output, "--mmr-lambda")
	require.Contains(t, output, "--search-window")
	require.Contains(t, output, "--rerank")
}

func TestQueryMissingTextReturnsClearError(t *testing.T) {
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"query"})

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "query text is required")
}

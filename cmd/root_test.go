package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRootCmd(t *testing.T) {
	cmd := NewRootCmd()

	require.NotNil(t, cmd)
	require.Equal(t, "jcemb", cmd.Use)
	require.Len(t, cmd.Commands(), 7)
}

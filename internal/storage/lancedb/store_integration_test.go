//go:build integration

package lancedb

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegrationGate(t *testing.T) {
	require.Equal(t, "1", os.Getenv("INTEGRATION"), "lancedb integration coverage is controlled by build tag plus env gate")
}

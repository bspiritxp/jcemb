package markdown

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeanPoolNormalized(t *testing.T) {
	pooled, err := meanPoolNormalized([][]float32{{1, 0}, {0, 1}})
	require.NoError(t, err)
	require.Len(t, pooled, 2)
	require.InDelta(t, 0.70710677, pooled[0], 1e-6)
	require.InDelta(t, 0.70710677, pooled[1], 1e-6)
}

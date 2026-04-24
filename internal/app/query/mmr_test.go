package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMMRSelectLambdaOneIsScoreOrder(t *testing.T) {
	t.Parallel()

	candidates := []testResultSpec{
		{id: "a", relPath: "docs/a.md", score: 0.99, vector: []float32{1, 0, 0}},
		{id: "b", relPath: "docs/b.md", score: 0.97, vector: []float32{0, 1, 0}},
		{id: "c", relPath: "docs/c.md", score: 0.94, vector: []float32{0, 0, 1}},
		{id: "d", relPath: "docs/d.md", score: 0.9, vector: []float32{1, 1, 0}},
	}

	results := mmrSelect([]float32{1, 0, 0}, buildSearchResults(candidates...), 4, 1.0)

	require.Equal(t, []string{"a", "b", "c", "d"}, resultIDs(results))
	require.Equal(t, []int{1, 2, 3, 4}, resultRanks(results))
	require.Equal(t, []float64{0.99, 0.97, 0.94, 0.9}, resultScores(results))
}

func TestMMRSelectSuppressesNearDuplicate(t *testing.T) {
	t.Parallel()

	results := mmrSelect(
		[]float32{1, 0.3},
		buildSearchResults(
			testResultSpec{id: "a", relPath: "docs/a.md", score: 0.95, vector: []float32{1, 0}},
			testResultSpec{id: "a-prime", relPath: "docs/a-2.md", score: 0.94, vector: []float32{0.9, -0.1}},
			testResultSpec{id: "b", relPath: "docs/b.md", score: 0.85, vector: []float32{0, 1}},
		),
		2,
		0.5,
	)

	require.Equal(t, []string{"a", "b"}, resultIDs(results))
	require.Equal(t, []int{1, 2}, resultRanks(results))
	require.Equal(t, []float64{0.95, 0.85}, resultScores(results))
}

func TestMMRSelectLambdaZeroIsMaxDiversity(t *testing.T) {
	t.Parallel()

	results := mmrSelect(
		[]float32{1, 0},
		buildSearchResults(
			testResultSpec{id: "a", relPath: "docs/a.md", score: 0.99, vector: []float32{1, 0}},
			testResultSpec{id: "b", relPath: "docs/b.md", score: 0.98, vector: []float32{0.95, 0.05}},
			testResultSpec{id: "c", relPath: "docs/c.md", score: 0.8, vector: []float32{0, 1}},
		),
		3,
		0.0,
	)

	require.Equal(t, []string{"a", "c", "b"}, resultIDs(results))
	require.Equal(t, []int{1, 2, 3}, resultRanks(results))
	require.Equal(t, []float64{0.99, 0.8, 0.98}, resultScores(results))
}

func TestCosineSimilarityHandlesEdgeCases(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0.0, cosineSimilarity(nil, nil))
	require.Equal(t, 0.0, cosineSimilarity([]float32{1, 0}, nil))
	require.Equal(t, 0.0, cosineSimilarity([]float32{0, 0}, []float32{1, 0}))
	require.Equal(t, 0.0, cosineSimilarity([]float32{1, 0}, []float32{1, 0, 0}))
	require.InDelta(t, 1.0, cosineSimilarity([]float32{1, 2}, []float32{1, 2}), 1e-6)
	require.InDelta(t, 0.0, cosineSimilarity([]float32{1, 0}, []float32{0, 1}), 1e-6)
}

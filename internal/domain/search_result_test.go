package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchResultSortOrdersByScoreThenPathThenChunkIndex(t *testing.T) {
	results := []SearchResult{
		{
			Chunk: Chunk{
				ID:       "chunk-3",
				Metadata: ChunkMetadata{RelPath: "docs/z.md", ChunkIndex: 1},
			},
			Score: 0.8,
		},
		{
			Chunk: Chunk{
				ID:       "chunk-2",
				Metadata: ChunkMetadata{RelPath: "docs/a.md", ChunkIndex: 2},
			},
			Score: 0.9,
		},
		{
			Chunk: Chunk{
				ID:       "chunk-1",
				Metadata: ChunkMetadata{RelPath: "docs/a.md", ChunkIndex: 0},
			},
			Score: 0.9,
		},
	}

	sorted := SortSearchResults(results)

	require.Equal(t, []string{"chunk-1", "chunk-2", "chunk-3"}, []string{sorted[0].Chunk.ID, sorted[1].Chunk.ID, sorted[2].Chunk.ID})
	require.Equal(t, 1, sorted[0].Rank)
	require.Equal(t, 2, sorted[1].Rank)
	require.Equal(t, 3, sorted[2].Rank)
	require.Equal(t, "chunk-3", results[0].Chunk.ID)
}

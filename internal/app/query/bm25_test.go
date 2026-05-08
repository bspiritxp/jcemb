package query

import (
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestApplyBM25RerankBoostsLexicalMatch(t *testing.T) {
	results := []domain.SearchResult{
		newBM25Result("generic", "docs/generic.md", "general vector search notes", 0.99),
		newBM25Result("target", "docs/bm25.md", "bm25 rerank lexical exact match", 0.90),
	}

	reranked := applyBM25Rerank("bm25 rerank", results)

	require.Equal(t, "target", reranked[0].Chunk.ID)
	require.Equal(t, 1, reranked[0].Rank)
}

func TestTokenizeBM25HandlesCJKRunes(t *testing.T) {
	require.Equal(t, []string{"搜", "图", "bm25"}, tokenizeBM25("搜图 BM25"))
}

func newBM25Result(id, relPath, content string, score float64) domain.SearchResult {
	return domain.SearchResult{
		Rank:  1,
		Score: score,
		Chunk: domain.Chunk{
			ID:      id,
			Content: content,
			Metadata: domain.ChunkMetadata{
				RelPath: relPath,
			},
		},
	}
}

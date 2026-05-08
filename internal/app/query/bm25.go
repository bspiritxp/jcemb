package query

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/bspiritxp/jcemb/internal/domain"
)

const (
	bm25K1             = 1.5
	bm25B              = 0.75
	bm25SemanticWeight = 0.7
)

func applyBM25Rerank(queryText string, results []domain.SearchResult) []domain.SearchResult {
	if len(results) == 0 {
		return results
	}
	queryTokens := tokenizeBM25(queryText)
	if len(queryTokens) == 0 {
		return results
	}

	docTokens := make([][]string, len(results))
	docFreq := map[string]int{}
	totalLen := 0
	for i, result := range results {
		tokens := tokenizeBM25(rerankDocument(result))
		docTokens[i] = tokens
		totalLen += len(tokens)
		seen := map[string]struct{}{}
		for _, token := range tokens {
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			docFreq[token]++
		}
	}
	if totalLen == 0 {
		return results
	}

	avgLen := float64(totalLen) / float64(len(docTokens))
	bm25Scores := make([]float64, len(results))
	for i, tokens := range docTokens {
		bm25Scores[i] = bm25Score(queryTokens, tokens, docFreq, len(results), avgLen)
	}
	semanticScores := make([]float64, len(results))
	for i, result := range results {
		semanticScores[i] = math.Max(result.Score, 0)
	}
	semanticNorm := normalizeScores(semanticScores)
	bm25Norm := normalizeScores(bm25Scores)

	reranked := append([]domain.SearchResult(nil), results...)
	for i := range reranked {
		reranked[i].Score = bm25SemanticWeight*semanticNorm[i] + (1-bm25SemanticWeight)*bm25Norm[i]
	}
	sort.Sort(domain.SearchResults(reranked))
	for i := range reranked {
		reranked[i].Rank = i + 1
	}
	return reranked
}

func bm25Score(queryTokens []string, docTokens []string, docFreq map[string]int, docCount int, avgLen float64) float64 {
	if len(docTokens) == 0 || avgLen <= 0 {
		return 0
	}
	freq := map[string]int{}
	for _, token := range docTokens {
		freq[token]++
	}
	score := 0.0
	docLen := float64(len(docTokens))
	for _, token := range uniqueTokens(queryTokens) {
		tf := float64(freq[token])
		if tf == 0 {
			continue
		}
		df := float64(docFreq[token])
		idf := math.Log(1 + (float64(docCount)-df+0.5)/(df+0.5))
		denom := tf + bm25K1*(1-bm25B+bm25B*(docLen/avgLen))
		score += idf * ((tf * (bm25K1 + 1)) / denom)
	}
	return score
}

func normalizeScores(scores []float64) []float64 {
	normalized := make([]float64, len(scores))
	maxScore := 0.0
	for _, score := range scores {
		if score > maxScore {
			maxScore = score
		}
	}
	if maxScore <= 0 {
		return normalized
	}
	for i, score := range scores {
		normalized[i] = score / maxScore
	}
	return normalized
}

func rerankDocument(result domain.SearchResult) string {
	parts := []string{
		result.Chunk.Metadata.RelPath,
		strings.Join(result.Chunk.Metadata.TitlePath, " "),
		strings.Join(result.Chunk.Metadata.Tags, " "),
		result.Chunk.Content,
	}
	return strings.Join(parts, " ")
}

func tokenizeBM25(text string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(current.String()))
		current.Reset()
	}
	for _, r := range text {
		if isCJK(r) {
			flush()
			tokens = append(tokens, string(r))
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) ||
		(r >= 0x2B820 && r <= 0x2CEAF)
}

func uniqueTokens(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	seen := map[string]struct{}{}
	for _, token := range tokens {
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

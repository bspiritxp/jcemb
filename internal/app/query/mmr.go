package query

import (
	"math"

	"github.com/bspiritxp/jcemb/internal/domain"
)

const defaultMMRLambda = 0.5

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}

	var dot, normA, normB float64
	for index := range a {
		av := float64(a[index])
		bv := float64(b[index])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func mmrSelect(queryVec []float32, candidates []domain.SearchResult, k int, lambda float64) []domain.SearchResult {
	truncated := truncateAndRerank(candidates, k)
	if lambda >= 1.0 || len(candidates) <= 1 || k <= 0 {
		return truncated
	}

	remaining := append([]domain.SearchResult(nil), candidates...)
	selected := make([]domain.SearchResult, 0, minInt(k, len(remaining)))

	firstIndex := 0
	firstScore := cosineSimilarity(queryVec, remaining[0].Vector)
	for index := 1; index < len(remaining); index++ {
		relevance := cosineSimilarity(queryVec, remaining[index].Vector)
		if relevance > firstScore {
			firstIndex = index
			firstScore = relevance
		}
	}
	selected = append(selected, remaining[firstIndex])
	remaining = append(remaining[:firstIndex], remaining[firstIndex+1:]...)

	for len(selected) < k && len(remaining) > 0 {
		bestIndex := 0
		bestScore := mmrScore(queryVec, remaining[0], selected, lambda)
		for index := 1; index < len(remaining); index++ {
			score := mmrScore(queryVec, remaining[index], selected, lambda)
			if score > bestScore {
				bestIndex = index
				bestScore = score
			}
		}

		selected = append(selected, remaining[bestIndex])
		remaining = append(remaining[:bestIndex], remaining[bestIndex+1:]...)
	}

	for index := range selected {
		selected[index].Rank = index + 1
	}

	return selected
}

func mmrScore(queryVec []float32, candidate domain.SearchResult, selected []domain.SearchResult, lambda float64) float64 {
	relevance := cosineSimilarity(queryVec, candidate.Vector)
	diversity := 0.0
	for _, entry := range selected {
		similarity := cosineSimilarity(candidate.Vector, entry.Vector)
		if similarity > diversity {
			diversity = similarity
		}
	}
	return lambda*relevance - (1-lambda)*diversity
}

func truncateAndRerank(results []domain.SearchResult, k int) []domain.SearchResult {
	limited := append([]domain.SearchResult(nil), results...)
	if k > 0 && len(limited) > k {
		limited = limited[:k]
	}
	for index := range limited {
		limited[index].Rank = index + 1
	}
	return limited
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

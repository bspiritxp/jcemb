package tagextractor

import (
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSemanticTags(t *testing.T) {
	cfg := domain.TagExtractorConfig{MaxTags: 8, MinTagLen: 2, MaxTagLen: 32}

	t.Run("nil input", func(t *testing.T) {
		got := NormalizeSemanticTags(nil, cfg)
		require.Empty(t, got)
	})

	t.Run("empty input", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{}, cfg)
		require.Empty(t, got)
	})

	t.Run("trims lowercases dedupes and preserves order", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{"  Go  ", "Vector", "go", "Semantic"}, cfg)
		require.Equal(t, []string{"go", "vector", "semantic"}, got)
	})

	t.Run("keeps chinese tags", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{"  向量  ", "语义"}, cfg)
		require.Equal(t, []string{"向量", "语义"}, got)
	})

	t.Run("rejects urls control chars numbers and punctuation", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{
			"http://evil",
			"https://evil.example",
			"abc://evil",
			"bad\nvalue",
			"bad\tvalue",
			"12345",
			"!!!",
			"ok-tag",
		}, cfg)
		require.Equal(t, []string{"ok-tag"}, got)
	})

	t.Run("drops too short and too long values", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{"a", "ab", "abcdefghijklmnopqrstuvwxyz123456789", "valid"}, cfg)
		require.Equal(t, []string{"ab", "valid"}, got)
	})

	t.Run("truncates to max tags after dedupe", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{"one", "two", "one", "three", "four", "five"}, domain.TagExtractorConfig{MaxTags: 3, MinTagLen: 1, MaxTagLen: 32})
		require.Equal(t, []string{"one", "two", "three"}, got)
	})

	t.Run("max tags does not reorder first occurrences", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{"Beta", "Alpha", "beta", "Gamma"}, domain.TagExtractorConfig{MaxTags: 2, MinTagLen: 1, MaxTagLen: 32})
		require.Equal(t, []string{"beta", "alpha"}, got)
	})

	t.Run("allows mixed alphanumeric and punctuation when not pure punctuation", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{"c++", "go1", "v2-tag"}, cfg)
		require.Equal(t, []string{"c++", "go1", "v2-tag"}, got)
	})

	t.Run("handles zero configured max tags as unlimited", func(t *testing.T) {
		got := NormalizeSemanticTags([]string{"one", "two", "three"}, domain.TagExtractorConfig{MaxTags: 0, MinTagLen: 1, MaxTagLen: 32})
		require.Equal(t, []string{"one", "two", "three"}, got)
	})
}

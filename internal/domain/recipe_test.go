package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbedRecipeIdentifierAndHashAreStable(t *testing.T) {
	recipeA := EmbedRecipe{
		Type:    "markdown",
		Version: "v1",
		Provider: ProviderConfig{
			Name:    "ollama",
			Version: "2026.04",
			Options: map[string]string{"timeout": "30s", "endpoint": "http://localhost:11434"},
		},
		Model: ModelSpec{
			Provider:   "ollama",
			Name:       "bge-m3",
			Version:    "latest",
			Dimensions: 1024,
			Options:    map[string]string{"encoding": "float32", "truncate": "true"},
		},
		Splitter: SplitterSpec{
			Name:    "markdown",
			Version: "v1",
			Options: map[string]string{"max_tokens": "512", "overlap": "64"},
		},
		Flags: map[string]bool{"force": true, "reconcile": true},
	}

	recipeB := EmbedRecipe{
		Type:    "markdown",
		Version: "v1",
		Provider: ProviderConfig{
			Name:    "ollama",
			Version: "2026.04",
			Options: map[string]string{"endpoint": "http://localhost:11434", "timeout": "30s"},
		},
		Model: ModelSpec{
			Provider:   "ollama",
			Name:       "bge-m3",
			Version:    "latest",
			Dimensions: 1024,
			Options:    map[string]string{"truncate": "true", "encoding": "float32"},
		},
		Splitter: SplitterSpec{
			Name:    "markdown",
			Version: "v1",
			Options: map[string]string{"overlap": "64", "max_tokens": "512"},
		},
		Flags: map[string]bool{"reconcile": true, "force": true},
	}

	require.Equal(t, recipeA.Identifier(), recipeB.Identifier())
	require.Equal(t, recipeA.Hash(), recipeB.Hash())
	require.Contains(t, recipeA.Identifier(), "provider.name=ollama")
	require.Contains(t, recipeA.Identifier(), "model.name=bge-m3")
	require.Contains(t, recipeA.Identifier(), "splitter.name=markdown")
	require.Len(t, recipeA.Hash(), 64)
}

func TestEmbedRecipeHashIncludesTagExtractorRecipeSpec(t *testing.T) {
	base := EmbedRecipe{
		Type:    "markdown",
		Version: "v1",
		Provider: ProviderConfig{
			Name:    "ollama",
			Version: "2026.04",
			Options: map[string]string{"endpoint": "http://localhost:11434"},
		},
		Model: ModelSpec{
			Provider:   "ollama",
			Name:       "bge-m3",
			Version:    "latest",
			Dimensions: 1024,
			Options:    map[string]string{"encoding": "float32"},
		},
		Splitter: SplitterSpec{
			Name:    "markdown",
			Version: "v1",
			Options: map[string]string{"max_tokens": "512"},
		},
		Flags: map[string]bool{"force": true},
	}

	t.Run("nil extractor keeps old hash", func(t *testing.T) {
		recipe := base
		recipe.TagExtractor = nil

		require.Equal(t, base.Identifier(), recipe.Identifier())
		require.Equal(t, base.Hash(), recipe.Hash())
	})

	t.Run("tag extractor changes durable identity", func(t *testing.T) {
		recipe := base
		recipe.TagExtractor = &TagExtractorRecipeSpec{
			Provider:      "ollama",
			Model:         "llama3.1",
			MaxTags:       8,
			MinTagLen:     2,
			MaxTagLen:     24,
			SkipIfHasYAML: true,
		}

		identifier := recipe.Identifier()
		require.Contains(t, identifier, "tag_extractor={provider=ollama,model=llama3.1,max_tags=8,min_tag_len=2,max_tag_len=24,skip_if_has_yaml=true}")
		require.NotContains(t, identifier, "timeout")
		require.NotContains(t, identifier, "base_url")
		require.NotContains(t, identifier, "api_key")
		require.Len(t, recipe.Hash(), 64)
		require.NotEqual(t, base.Hash(), recipe.Hash())
	})

	t.Run("field differences change hash", func(t *testing.T) {
		left := base
		left.TagExtractor = &TagExtractorRecipeSpec{
			Provider:      "ollama",
			Model:         "llama3.1",
			MaxTags:       8,
			MinTagLen:     2,
			MaxTagLen:     24,
			SkipIfHasYAML: true,
		}

		right := base
		right.TagExtractor = &TagExtractorRecipeSpec{
			Provider:      "ollama",
			Model:         "llama3.1",
			MaxTags:       9,
			MinTagLen:     2,
			MaxTagLen:     24,
			SkipIfHasYAML: true,
		}

		require.NotEqual(t, left.Identifier(), right.Identifier())
		require.NotEqual(t, left.Hash(), right.Hash())
	})
}

func TestEmbedRecipeRejectsInvalidRecipe(t *testing.T) {
	tests := map[string]EmbedRecipe{
		"missing provider": {
			Type:    "markdown",
			Version: "v1",
			Model:   ModelSpec{Name: "bge-m3"},
			Splitter: SplitterSpec{
				Name: "markdown",
			},
		},
		"provider model mismatch": {
			Type:    "markdown",
			Version: "v1",
			Provider: ProviderConfig{
				Name: "ollama",
			},
			Model: ModelSpec{
				Provider: "openai",
				Name:     "bge-m3",
			},
			Splitter: SplitterSpec{Name: "markdown"},
		},
		"negative dimensions": {
			Type:    "markdown",
			Version: "v1",
			Provider: ProviderConfig{
				Name: "ollama",
			},
			Model: ModelSpec{
				Provider:   "ollama",
				Name:       "bge-m3",
				Dimensions: -1,
			},
			Splitter: SplitterSpec{Name: "markdown"},
		},
	}

	for name, recipe := range tests {
		t.Run(name, func(t *testing.T) {
			err := recipe.Validate()
			require.Error(t, err)
			require.Empty(t, recipe.Identifier())
			require.Empty(t, recipe.Hash())
		})
	}
}

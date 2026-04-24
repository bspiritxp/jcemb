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

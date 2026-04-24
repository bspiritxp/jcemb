package index

import (
	"strings"

	"github.com/bspiritxp/jcemb/internal/domain"
)

type InvalidationReason string

const (
	InvalidationMissingState     InvalidationReason = "missing_state"
	InvalidationFileHashChanged  InvalidationReason = "file_hash_changed"
	InvalidationRecipeChanged    InvalidationReason = "recipe_hash_changed"
	InvalidationProviderChanged  InvalidationReason = "provider_changed"
	InvalidationModelChanged     InvalidationReason = "model_changed"
	InvalidationSplitterChanged  InvalidationReason = "splitter_changed"
	InvalidationVectorDimChanged InvalidationReason = "vector_dim_changed"
	InvalidationDBVersionChanged InvalidationReason = "db_version_changed"
)

func FileNeedsReindex(state domain.FileState, currentFileHash string, recipe domain.EmbedRecipe) ([]InvalidationReason, bool) {
	reasons := make([]InvalidationReason, 0, 2)
	if strings.TrimSpace(state.RelPath) == "" {
		reasons = append(reasons, InvalidationMissingState)
	}
	if strings.TrimSpace(state.FileHash) != strings.TrimSpace(currentFileHash) {
		reasons = append(reasons, InvalidationFileHashChanged)
	}
	if strings.TrimSpace(state.RecipeHash) != recipe.Hash() {
		reasons = append(reasons, InvalidationRecipeChanged)
	}
	return reasons, len(reasons) > 0
}

func ConfigNeedsRebuild(stored domain.StoreConfig, current domain.StoreConfig) ([]InvalidationReason, bool) {
	reasons := make([]InvalidationReason, 0, 5)
	if strings.TrimSpace(stored.Provider) != strings.TrimSpace(current.Provider) {
		reasons = append(reasons, InvalidationProviderChanged)
	}
	if strings.TrimSpace(stored.Model) != strings.TrimSpace(current.Model) {
		reasons = append(reasons, InvalidationModelChanged)
	}
	if strings.TrimSpace(stored.Splitter) != strings.TrimSpace(current.Splitter) {
		reasons = append(reasons, InvalidationSplitterChanged)
	}
	if stored.VectorDim != current.VectorDim {
		reasons = append(reasons, InvalidationVectorDimChanged)
	}
	if strings.TrimSpace(stored.DBVersion) != strings.TrimSpace(current.DBVersion) {
		reasons = append(reasons, InvalidationDBVersionChanged)
	}
	return reasons, len(reasons) > 0
}

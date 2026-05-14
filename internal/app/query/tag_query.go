package query

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/scanprovider/image"
)

const minQueryTagRunes = 10

type queryTagState struct {
	tags         []string
	useTagFusion bool
	vectors      map[tagVectorConfigKey][]float32
}

type tagVectorConfigKey struct {
	Provider  string
	Model     string
	VectorDim int
}

func (s *Service) prepareQueryTags(ctx context.Context, request Request) ([]string, bool, error) {
	if request.NoTag || request.TagWeight <= 0 {
		return nil, false, nil
	}
	if normalizeFileType(request.FileType) == "image" || s.isImagePathQuery(request.Text) {
		return nil, false, nil
	}
	queryText := strings.TrimSpace(request.Text)
	if utf8.RuneCountInString(queryText) < minQueryTagRunes {
		return nil, false, nil
	}

	tags, err := s.extractQueryTags(ctx, queryText, request.TagExtractor)
	if err != nil || len(tags) == 0 {
		return nil, false, nil
	}
	return append([]string(nil), tags...), true, nil
}

func (s *Service) resolveScopeQueryTagVector(ctx context.Context, config domain.StoreConfig, request Request, state *queryTagState) ([]float32, bool, error) {
	if state == nil || !state.useTagFusion || len(state.tags) == 0 {
		return nil, false, nil
	}
	key := tagVectorCompatibilityKey(config)
	if vector, ok := state.vectors[key]; ok {
		return append([]float32(nil), vector...), true, nil
	}

	vector, err := s.embedTagsForQuery(ctx, config, request, state.tags)
	if err != nil {
		return nil, false, err
	}
	if len(vector) == 0 {
		return nil, false, nil
	}
	if state.vectors == nil {
		state.vectors = make(map[tagVectorConfigKey][]float32)
	}
	state.vectors[key] = append([]float32(nil), vector...)
	return append([]float32(nil), state.vectors[key]...), true, nil
}

func (s *Service) extractQueryTags(ctx context.Context, queryText string, config domain.TagExtractorConfig) ([]string, error) {
	config = cloneTagExtractorConfig(config)
	if strings.TrimSpace(config.Provider) == "" {
		return nil, fmt.Errorf("query: tag extractor provider is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, fmt.Errorf("query: tag extractor model is required")
	}
	if config.MaxTags <= 0 {
		config.MaxTags = domain.DefaultTagExtractorMaxTags
	}
	if config.MinTagLen <= 0 {
		config.MinTagLen = domain.DefaultTagExtractorMinTagLen
	}
	if config.MaxTagLen <= 0 {
		config.MaxTagLen = domain.DefaultTagExtractorMaxTagLen
	}
	if config.Timeout <= 0 {
		config.Timeout = domain.DefaultTagExtractorTimeout
	}

	factory, err := s.deps.GetTagExtractor(config.Provider)
	if err != nil {
		return nil, err
	}
	extractor, err := factory(config)
	if err != nil {
		return nil, err
	}

	result, err := extractor.Extract(ctx, domain.TagExtractRequest{
		Document: domain.Document{Content: queryText},
		Config:   config,
	})
	if err != nil {
		return nil, err
	}
	return append([]string(nil), result.Tags...), nil
}

func (s *Service) embedTagsForQuery(ctx context.Context, config domain.StoreConfig, request Request, tags []string) ([]float32, error) {
	providerFactory, err := s.deps.GetProvider(config.Provider)
	if err != nil {
		return nil, fmt.Errorf("query: provider %q is not available: %w", config.Provider, err)
	}

	providerConfig := domain.ProviderConfig{Name: config.Provider}
	if request.Provider == config.Provider {
		providerConfig.Options = cloneStringMap(request.ProviderOptions)
	}

	provider, err := providerFactory(providerConfig)
	if err != nil {
		return nil, fmt.Errorf("query: initialize provider %q: %w", config.Provider, err)
	}

	embedder, err := provider.NewEmbedder(domain.ModelSpec{Provider: config.Provider, Name: config.Model})
	if err != nil {
		return nil, fmt.Errorf("query: initialize model %q for provider %q: %w", config.Model, config.Provider, err)
	}

	inputs := make([]domain.EmbedInput, 0, len(tags))
	for index, tag := range tags {
		inputs = append(inputs, domain.EmbedInput{ChunkID: fmt.Sprintf("%s-tag-%d", queryChunkID, index), Text: tag})
	}
	embeddings, err := embedder.Embed(ctx, domain.EmbedRequest{
		Recipe: domain.EmbedRecipe{
			Type:    defaultRecipeType,
			Version: defaultRecipeVersion,
			Provider: domain.ProviderConfig{
				Name: config.Provider,
			},
			Model: domain.ModelSpec{
				Provider: config.Provider,
				Name:     config.Model,
			},
		},
		Purpose: domain.EmbedPurposeQuery,
		Inputs:  inputs,
	})
	if err != nil {
		return nil, err
	}
	if len(embeddings) != len(tags) {
		return nil, fmt.Errorf("query: provider returned %d embeddings for %d tags", len(embeddings), len(tags))
	}

	vectors := make([][]float32, 0, len(embeddings))
	for _, embedding := range embeddings {
		vector := append([]float32(nil), embedding.Vector...)
		if len(vector) != config.VectorDim {
			return nil, fmt.Errorf("query: provider vector dimension mismatch: expected=%d actual=%d", config.VectorDim, len(vector))
		}
		vectors = append(vectors, vector)
	}
	return meanPoolNormalized(vectors)
}

func (s *Service) isImagePathQuery(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	info, err := s.deps.Stat(trimmed)
	if err != nil || info.IsDir() {
		return false
	}
	return image.SupportedExtension(filepath.Ext(trimmed))
}

func tagVectorCompatibilityKey(config domain.StoreConfig) tagVectorConfigKey {
	return tagVectorConfigKey{Provider: config.Provider, Model: config.Model, VectorDim: config.VectorDim}
}

func meanPoolNormalized(vectors [][]float32) ([]float32, error) {
	if len(vectors) == 0 {
		return nil, nil
	}
	dim := len(vectors[0])
	if dim == 0 {
		return nil, nil
	}
	pooled := make([]float32, dim)
	for _, vector := range vectors {
		if len(vector) != dim {
			return nil, fmt.Errorf("query: tag vector dimension mismatch: expected=%d actual=%d", dim, len(vector))
		}
		for index, value := range vector {
			pooled[index] += value
		}
	}
	count := float32(len(vectors))
	var sumSquares float64
	for index := range pooled {
		pooled[index] /= count
		sumSquares += float64(pooled[index] * pooled[index])
	}
	if sumSquares == 0 {
		return pooled, nil
	}
	scale := float32(1 / math.Sqrt(sumSquares))
	for index := range pooled {
		pooled[index] *= scale
	}
	return pooled, nil
}

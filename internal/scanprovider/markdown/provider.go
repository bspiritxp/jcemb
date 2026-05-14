package markdown

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/bspiritxp/jcemb/internal/domain"
	internalfs "github.com/bspiritxp/jcemb/internal/fs"
	"github.com/bspiritxp/jcemb/internal/metadata"
	splitmarkdown "github.com/bspiritxp/jcemb/internal/splitter/markdown"
)

const (
	FileType       = "markdown"
	legacyFileType = "md"
	Name           = "markdown"
	Version        = "v1"
)

type Provider struct{}

func New() domain.ScanProvider {
	return Provider{}
}

func (Provider) FileType() string {
	return FileType
}

func (Provider) Extensions() []string {
	return []string{".md"}
}

func (Provider) Recipe(config domain.ScanProviderConfig) domain.EmbedRecipe {
	recipe := domain.EmbedRecipe{
		Type:    FileType,
		Version: Version,
		Provider: domain.ProviderConfig{
			Name:    config.Provider,
			Options: cloneStringMap(config.ProviderOptions),
		},
		Model: domain.ModelSpec{
			Provider: config.Provider,
			Name:     config.Model,
		},
		Splitter: domain.SplitterSpec{
			Name:    Name,
			Version: Version,
			Options: map[string]string{
				splitmarkdown.ShortFileMaxCharsOption: strconv.Itoa(splitmarkdown.DefaultShortFileMaxChars),
			},
		},
		Flags: map[string]bool{
			"recursive": config.Recursive,
			"force":     config.Force,
		},
	}
	if spec := recipeTagExtractorSpec(config.TagExtractor); spec != nil {
		recipe.TagExtractor = spec
	}
	return recipe
}

func (Provider) BuildRecords(ctx context.Context, request domain.ScanProviderRequest) (domain.ScanProviderResult, error) {
	providerFactory, err := request.GetProvider(request.Config.Provider)
	if err != nil {
		return domain.ScanProviderResult{}, err
	}
	provider, err := providerFactory(domain.ProviderConfig{Name: request.Config.Provider, Options: cloneStringMap(request.Config.ProviderOptions)})
	if err != nil {
		return domain.ScanProviderResult{}, err
	}
	embedder, err := provider.NewEmbedder(domain.ModelSpec{Provider: request.Config.Provider, Name: request.Config.Model})
	if err != nil {
		return domain.ScanProviderResult{}, err
	}
	splitterFactory, err := request.GetSplitter(Name)
	if err != nil {
		return domain.ScanProviderResult{}, err
	}
	splitter, err := splitterFactory(request.Recipe.Splitter)
	if err != nil {
		return domain.ScanProviderResult{}, err
	}

	sourceDocument, err := metadata.LoadFile(internalfs.File{
		RootDir:  request.File.RootDir,
		FilePath: request.File.FilePath,
		RelPath:  request.File.RelPath,
		FileName: request.File.FileName,
		DocType:  legacyFileType,
		ModTime:  request.File.ModTime,
	})
	if err != nil {
		return domain.ScanProviderResult{}, err
	}
	document := sourceDocument.Metadata.DomainDocument(sourceDocument.Content)
	document.DocType = FileType

	chunks, err := splitter.Split(ctx, document)
	if err != nil {
		return domain.ScanProviderResult{}, err
	}

	semanticTags, tagVector, err := buildSemanticTagData(ctx, request, document, embedder)
	if err != nil {
		return domain.ScanProviderResult{}, err
	}

	records := make([]domain.VectorRecord, 0, len(chunks))
	chunkIDs := make([]string, 0, len(chunks))
	if len(chunks) > 0 {
		inputs := make([]domain.EmbedInput, 0, len(chunks))
		for _, chunk := range chunks {
			chunk.Metadata.DocType = FileType
			chunk.Document.DocType = FileType
			inputs = append(inputs, domain.EmbedInput{ChunkID: chunk.ID, Text: chunk.Content, Metadata: chunk.Metadata})
		}
		embeddings, err := embedder.Embed(ctx, domain.EmbedRequest{Recipe: request.Recipe, Inputs: inputs, Purpose: domain.EmbedPurposeDocument})
		if err != nil {
			return domain.ScanProviderResult{}, err
		}
		vectors := make(map[string][]float32, len(embeddings))
		for _, embedding := range embeddings {
			vectors[embedding.ChunkID] = append([]float32(nil), embedding.Vector...)
		}
		for _, chunk := range chunks {
			chunk.Metadata.DocType = FileType
			chunk.Document.DocType = FileType
			vector, ok := vectors[chunk.ID]
			if !ok {
				return domain.ScanProviderResult{}, fmt.Errorf("scan: missing vector for chunk %s", chunk.ID)
			}
			records = append(records, domain.VectorRecord{
				Chunk:        chunk,
				Vector:       vector,
				TagVector:    append([]float32(nil), tagVector...),
				SemanticTags: append([]string(nil), semanticTags...),
			})
			chunkIDs = append(chunkIDs, chunk.ID)
		}
	}

	now := request.Now()
	return domain.ScanProviderResult{
		State: domain.FileState{
			Source:        document.Source,
			FilePath:      document.FilePath,
			RelPath:       document.RelPath,
			FileName:      document.FileName,
			DocType:       FileType,
			FileHash:      document.FileHash,
			ModTime:       request.File.ModTime,
			RecipeHash:    request.Recipe.Hash(),
			ChunkIDs:      chunkIDs,
			ChunkCount:    len(chunkIDs),
			LastIndexedAt: now,
		},
		Records: records,
	}, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func buildSemanticTagData(ctx context.Context, request domain.ScanProviderRequest, document domain.Document, embedder domain.Embedder) ([]string, []float32, error) {
	semanticTags, err := extractSemanticTags(ctx, request, document)
	if err != nil {
		log.Printf("markdown: tag extraction failed for %s: %v", document.RelPath, err)
		return nil, nil, nil
	}
	if len(semanticTags) == 0 {
		return nil, nil, nil
	}

	inputs := make([]domain.EmbedInput, 0, len(semanticTags))
	for index, tag := range semanticTags {
		inputs = append(inputs, domain.EmbedInput{
			ChunkID:  fmt.Sprintf("tag-%d", index),
			Text:     tag,
			Metadata: domain.ChunkMetadata{RelPath: document.RelPath, DocType: FileType},
		})
	}
	embeddings, err := embedder.Embed(ctx, domain.EmbedRequest{Recipe: request.Recipe, Inputs: inputs, Purpose: domain.EmbedPurposeDocument})
	if err != nil {
		return nil, nil, err
	}
	vectors := make(map[string][]float32, len(embeddings))
	for _, embedding := range embeddings {
		vectors[embedding.ChunkID] = append([]float32(nil), embedding.Vector...)
	}
	ordered := make([][]float32, 0, len(inputs))
	for _, input := range inputs {
		vector, ok := vectors[input.ChunkID]
		if !ok {
			return nil, nil, fmt.Errorf("scan: missing tag vector for %s", input.ChunkID)
		}
		ordered = append(ordered, vector)
	}
	pooled, err := meanPoolNormalized(ordered)
	if err != nil {
		return nil, nil, err
	}
	return append([]string(nil), semanticTags...), pooled, nil
}

func extractSemanticTags(ctx context.Context, request domain.ScanProviderRequest, document domain.Document) ([]string, error) {
	spec := request.Recipe.TagExtractor
	if spec == nil {
		return nil, nil
	}
	if spec.SkipIfHasYAML && len(document.Tags) > 0 {
		return append([]string(nil), document.Tags...), nil
	}
	if request.GetTagExtractor == nil {
		return nil, fmt.Errorf("scan: tag extractor getter is required")
	}
	config := domain.TagExtractorConfig{
		Provider:      firstNonEmpty(request.Config.TagExtractor.Provider, spec.Provider),
		Model:         firstNonEmpty(request.Config.TagExtractor.Model, spec.Model),
		Options:       cloneStringMap(request.Config.TagExtractor.Options),
		Timeout:       request.Config.TagExtractor.Timeout,
		MaxTags:       firstPositive(request.Config.TagExtractor.MaxTags, spec.MaxTags),
		MinTagLen:     firstPositive(request.Config.TagExtractor.MinTagLen, spec.MinTagLen),
		MaxTagLen:     firstPositive(request.Config.TagExtractor.MaxTagLen, spec.MaxTagLen),
		SkipIfHasYAML: spec.SkipIfHasYAML || request.Config.TagExtractor.SkipIfHasYAML,
	}
	extractor, err := request.GetTagExtractor(config)
	if err != nil {
		return nil, err
	}
	result, err := extractor.Extract(ctx, domain.TagExtractRequest{Document: document, Config: config})
	if err != nil {
		return nil, err
	}
	if len(result.Tags) == 0 {
		return nil, nil
	}
	return append([]string(nil), result.Tags...), nil
}

func recipeTagExtractorSpec(config domain.TagExtractorConfig) *domain.TagExtractorRecipeSpec {
	if config.Provider == "" || config.Model == "" {
		return nil
	}
	return &domain.TagExtractorRecipeSpec{
		Provider:      config.Provider,
		Model:         config.Model,
		MaxTags:       config.MaxTags,
		MinTagLen:     config.MinTagLen,
		MaxTagLen:     config.MaxTagLen,
		SkipIfHasYAML: config.SkipIfHasYAML,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

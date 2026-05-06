package markdown

import (
	"context"
	"fmt"

	"github.com/bspiritxp/jcemb/internal/domain"
	internalfs "github.com/bspiritxp/jcemb/internal/fs"
	"github.com/bspiritxp/jcemb/internal/metadata"
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
	return domain.EmbedRecipe{
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
		},
		Flags: map[string]bool{
			"recursive": config.Recursive,
			"force":     config.Force,
		},
	}
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
	splitter, err := splitterFactory(domain.SplitterSpec{Name: Name, Version: Version})
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
			records = append(records, domain.VectorRecord{Chunk: chunk, Vector: vector})
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

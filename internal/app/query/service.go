package query

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	_ "github.com/bspiritxp/jcemb/internal/provider/ollama"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
	_ "github.com/bspiritxp/jcemb/internal/storage/lancedb"
)

const (
	defaultLimit                  = 10
	defaultSearchWindowMultiplier = 5
	defaultSearchWindowFloor      = 20
	defaultThresholdAlpha         = 0.85
	defaultThresholdDelta         = 0.10
	defaultRecipeType             = "query"
	defaultRecipeVersion          = "v1"
	defaultVectorStoreName        = lancedb.Name
	queryChunkID                  = "query"
)

type Request struct {
	Text           string
	Tags           []string
	Limit          int
	Path           string
	Unique         bool
	Full           bool
	ThresholdAlpha float64
	ThresholdDelta float64
	MMRLambda      float64
	SearchWindow   int
}

type Result struct {
	Query    string
	Tags     []string
	Limit    int
	PathRoot string
	RootDir  string
	Manifest domain.StoreConfig
	Results  []domain.SearchResult
	Full     bool
}

type queryScope struct {
	RootDir    string
	PathPrefix string
}

type Dependencies struct {
	LoadIndex      func(rootDir string) (index.Snapshot, error)
	GetProvider    func(name string) (registry.ProviderFactory, error)
	GetVectorStore func(name string) (registry.VectorStoreFactory, error)
	Stat           func(name string) (os.FileInfo, error)
	VectorStore    string
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	if deps.LoadIndex == nil {
		deps.LoadIndex = index.Load
	}
	if deps.GetProvider == nil {
		deps.GetProvider = registry.GetProvider
	}
	if deps.GetVectorStore == nil {
		deps.GetVectorStore = registry.GetVectorStore
	}
	if deps.Stat == nil {
		deps.Stat = os.Stat
	}
	if strings.TrimSpace(deps.VectorStore) == "" {
		deps.VectorStore = defaultVectorStoreName
	}
	return &Service{deps: deps}
}

func (s *Service) Run(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	normalized, err := normalizeRequest(request)
	if err != nil {
		return Result{}, err
	}

	scope, err := resolveQueryScope(normalized.Path)
	if err != nil {
		return Result{}, err
	}
	if err := s.ensureVectorDB(scope.RootDir); err != nil {
		return Result{}, err
	}

	snapshot, err := s.deps.LoadIndex(scope.RootDir)
	if err != nil {
		switch {
		case errors.Is(err, index.ErrStateNotFound):
			return Result{}, fmt.Errorf("query: .vectordb manifests are missing under %s", filepath.Join(scope.RootDir, index.DirectoryName))
		case errors.Is(err, index.ErrRebuildRequired):
			return Result{}, fmt.Errorf("query: .vectordb manifests are invalid: %w", err)
		default:
			return Result{}, err
		}
	}
	if err := validateManifest(snapshot.Config); err != nil {
		return Result{}, err
	}

	queryVector, err := s.embedQuery(ctx, snapshot.Config, normalized.Text)
	if err != nil {
		return Result{}, err
	}

	factory, err := s.deps.GetVectorStore(s.deps.VectorStore)
	if err != nil {
		return Result{}, err
	}

	storeConfig := snapshot.Config
	storeConfig.Namespace = s.deps.VectorStore
	store, err := factory(storeConfig)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()

	results, err := store.Search(ctx, domain.SearchQuery{
		Vector:     queryVector,
		Limit:      effectiveSearchWindow(normalized.Limit, normalized.SearchWindow),
		Tags:       normalized.Tags,
		PathPrefix: scope.PathPrefix,
	})
	if err != nil {
		if errors.Is(err, lancedb.ErrVectorDBNotFound) {
			return Result{}, fmt.Errorf("query: .vectordb storage is not initialized under %s", filepath.Join(scope.RootDir, index.DirectoryName))
		}
		return Result{}, err
	}

	sorted := domain.SortSearchResults(results)
	sorted = applyDynamicThreshold(sorted, normalized.ThresholdAlpha, normalized.ThresholdDelta)
	if normalized.Unique {
		sorted = dedupByRelPath(sorted)
	}
	sorted = mmrSelect(queryVector, sorted, normalized.Limit, normalized.MMRLambda)

	return Result{
		Query:    normalized.Text,
		Tags:     append([]string(nil), normalized.Tags...),
		Limit:    normalized.Limit,
		PathRoot: normalized.Path,
		RootDir:  scope.RootDir,
		Manifest: snapshot.Config,
		Results:  sorted,
		Full:     normalized.Full,
	}, nil
}

func normalizeRequest(request Request) (Request, error) {
	normalized := request
	normalized.Text = strings.TrimSpace(normalized.Text)
	if normalized.Text == "" {
		return Request{}, fmt.Errorf("query: text is required")
	}
	if strings.TrimSpace(normalized.Path) == "" {
		normalized.Path = "."
	}
	normalized.Tags = domain.NormalizeTags(normalized.Tags)
	if normalized.Limit <= 0 {
		normalized.Limit = defaultLimit
	}
	if normalized.ThresholdAlpha == 0 {
		normalized.ThresholdAlpha = defaultThresholdAlpha
	}
	if normalized.ThresholdAlpha < 0 {
		normalized.ThresholdAlpha = 0
	}
	if normalized.ThresholdDelta == 0 {
		normalized.ThresholdDelta = defaultThresholdDelta
	}
	if normalized.ThresholdDelta < 0 {
		normalized.ThresholdDelta = 0
	}
	if normalized.MMRLambda == 0 {
		normalized.MMRLambda = defaultMMRLambda
	}
	if normalized.MMRLambda < 0 {
		normalized.MMRLambda = 1.0
	}
	if normalized.MMRLambda > 1 {
		normalized.MMRLambda = 1.0
	}
	if normalized.SearchWindow < 0 {
		normalized.SearchWindow = 0
	}
	return normalized, nil
}

func effectiveSearchWindow(limit int, searchWindow int) int {
	if limit <= 0 {
		limit = defaultLimit
	}
	if searchWindow > 0 {
		if searchWindow < limit {
			return limit
		}
		return searchWindow
	}
	window := defaultSearchWindowMultiplier * limit
	if window < defaultSearchWindowFloor {
		return defaultSearchWindowFloor
	}
	return window
}

func applyDynamicThreshold(results []domain.SearchResult, alpha, delta float64) []domain.SearchResult {
	if len(results) == 0 {
		return results
	}

	top1 := results[0].Score
	filtered := make([]domain.SearchResult, 0, len(results))
	for index, result := range results {
		if index == 0 {
			filtered = append(filtered, result)
			continue
		}

		alphaPass := alpha <= 0 || result.Score >= alpha*top1
		deltaPass := delta <= 0 || top1-result.Score <= delta
		if alphaPass && deltaPass {
			filtered = append(filtered, result)
		}
	}

	for index := range filtered {
		filtered[index].Rank = index + 1
	}

	return filtered
}

func (s *Service) ensureVectorDB(rootDir string) error {
	vectorDBPath := filepath.Join(rootDir, index.DirectoryName)
	info, err := s.deps.Stat(vectorDBPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("query: .vectordb not found under %s", rootDir)
		}
		return fmt.Errorf("query: stat .vectordb: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("query: .vectordb is not a directory under %s", rootDir)
	}
	return nil
}

func validateManifest(config domain.StoreConfig) error {
	if strings.TrimSpace(config.Provider) == "" {
		return fmt.Errorf("query: manifest provider is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return fmt.Errorf("query: manifest model is required")
	}
	if config.VectorDim <= 0 {
		return fmt.Errorf("query: manifest vector_dim must be > 0")
	}
	if strings.TrimSpace(config.DBVersion) == "" {
		return fmt.Errorf("query: manifest db_version is required")
	}
	return nil
}

func (s *Service) embedQuery(ctx context.Context, config domain.StoreConfig, text string) ([]float32, error) {
	providerFactory, err := s.deps.GetProvider(config.Provider)
	if err != nil {
		return nil, fmt.Errorf("query: provider %q is not available: %w", config.Provider, err)
	}

	provider, err := providerFactory(domain.ProviderConfig{Name: config.Provider})
	if err != nil {
		return nil, fmt.Errorf("query: initialize provider %q: %w", config.Provider, err)
	}

	embedder, err := provider.NewEmbedder(domain.ModelSpec{Provider: config.Provider, Name: config.Model})
	if err != nil {
		return nil, fmt.Errorf("query: initialize model %q for provider %q: %w", config.Model, config.Provider, err)
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
		Inputs: []domain.EmbedInput{{ChunkID: queryChunkID, Text: text}},
	})
	if err != nil {
		return nil, err
	}
	if len(embeddings) != 1 {
		return nil, fmt.Errorf("query: provider returned %d embeddings for one query", len(embeddings))
	}

	vector := append([]float32(nil), embeddings[0].Vector...)
	if len(vector) != config.VectorDim {
		return nil, fmt.Errorf("query: provider vector dimension mismatch: expected=%d actual=%d", config.VectorDim, len(vector))
	}
	return vector, nil
}

func resolveQueryScope(inputPath string) (queryScope, error) {
	absPath, err := filepath.Abs(strings.TrimSpace(inputPath))
	if err != nil {
		return queryScope{}, fmt.Errorf("query: resolve path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return queryScope{}, fmt.Errorf("query: stat path: %w", err)
	}

	targetRoot := absPath
	if !info.IsDir() {
		targetRoot = filepath.Dir(absPath)
	}

	current := targetRoot
	for {
		vectorDBPath := filepath.Join(current, index.DirectoryName)
		vectorDBInfo, statErr := os.Stat(vectorDBPath)
		switch {
		case statErr == nil && vectorDBInfo.IsDir():
			prefix, prefixErr := filepath.Rel(current, absPath)
			if prefixErr != nil {
				return queryScope{}, fmt.Errorf("query: compute path prefix: %w", prefixErr)
			}
			prefix = filepath.ToSlash(prefix)
			if info.IsDir() && prefix == "." {
				prefix = ""
			}
			return queryScope{RootDir: current, PathPrefix: prefix}, nil
		case statErr == nil:
			return queryScope{}, fmt.Errorf("query: .vectordb is not a directory under %s", current)
		case !errors.Is(statErr, os.ErrNotExist):
			return queryScope{}, fmt.Errorf("query: stat .vectordb: %w", statErr)
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return queryScope{RootDir: targetRoot}, nil
}

func dedupByRelPath(results []domain.SearchResult) []domain.SearchResult {
	seen := make(map[string]struct{}, len(results))
	deduped := make([]domain.SearchResult, 0, len(results))
	for _, result := range results {
		key := result.Chunk.Metadata.RelPath
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, result)
	}
	for index := range deduped {
		deduped[index].Rank = index + 1
	}
	return deduped
}

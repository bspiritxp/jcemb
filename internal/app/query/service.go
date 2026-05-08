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
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	_ "github.com/bspiritxp/jcemb/internal/provider/ollama"
	_ "github.com/bspiritxp/jcemb/internal/provider/openai"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/bspiritxp/jcemb/internal/scanprovider/image"
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
	defaultFileType               = "markdown"
	queryChunkID                  = "query"
)

type Request struct {
	Text            string
	Tags            []string
	Limit           int
	Path            string
	DataDir         string
	Provider        string
	ProviderOptions map[string]string
	FileType        string
	Unique          bool
	Full            bool
	ThresholdAlpha  float64
	ThresholdDelta  float64
	MMRLambda       float64
	SearchWindow    int
	Rerank          string
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
	RootDir      string
	RootIdentity string
	CollectionID string
	DataDir      string
	StorageRoot  string
	PathPrefix   string
}

type Dependencies struct {
	LoadIndex         func(rootDir string) (index.Snapshot, error)
	LoadCollections   func(dataRoot string) (index.CollectionRegistry, error)
	ResolveAppPaths   func() (jcpaths.AppPaths, error)
	ResolveCollection func(dataRoot string, inputPath string, fileType string) (index.CollectionMatch, error)
	GetProvider       func(name string) (registry.ProviderFactory, error)
	GetVectorStore    func(name string) (registry.VectorStoreFactory, error)
	Stat              func(name string) (os.FileInfo, error)
	VectorStore       string
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	if deps.LoadIndex == nil {
		deps.LoadIndex = index.Load
	}
	if deps.LoadCollections == nil {
		deps.LoadCollections = index.LoadCollectionRegistry
	}
	if deps.ResolveAppPaths == nil {
		deps.ResolveAppPaths = jcpaths.ResolveAppPaths
	}
	if deps.ResolveCollection == nil {
		deps.ResolveCollection = index.ResolveCollectionForFileType
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

	scopes, err := s.resolveQueryScopes(normalized.Path, normalized.DataDir, normalized.FileType)
	if err != nil {
		return Result{}, err
	}
	results := make([]domain.SearchResult, 0)
	manifests := make([]domain.StoreConfig, 0, len(scopes))
	queryVectors := make([][]float32, 0, len(scopes))
	for _, scope := range scopes {
		snapshot, queryVector, collectionResults, err := s.searchScope(ctx, scope, normalized)
		if err != nil {
			return Result{}, err
		}
		manifests = append(manifests, snapshot.Config)
		queryVectors = append(queryVectors, queryVector)
		results = append(results, collectionResults...)
	}

	sorted := domain.SortSearchResults(results)
	sorted = applyDynamicThreshold(sorted, normalized.ThresholdAlpha, normalized.ThresholdDelta)
	if normalized.Unique {
		sorted = dedupByRelPath(sorted)
	}
	if normalized.Rerank == "bm25" {
		sorted = applyBM25Rerank(normalized.Text, sorted)
		sorted = truncateAndRerank(sorted, normalized.Limit)
	} else {
		sorted = selectFinalResults(queryVectors, manifests, sorted, normalized.Limit, normalized.MMRLambda)
	}

	return Result{
		Query:    normalized.Text,
		Tags:     append([]string(nil), normalized.Tags...),
		Limit:    normalized.Limit,
		PathRoot: normalized.Path,
		RootDir:  resultRootDir(scopes),
		Manifest: combinedManifest(manifests),
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
	normalized.Path = strings.TrimSpace(normalized.Path)
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
	normalized.Rerank = strings.TrimSpace(strings.ToLower(normalized.Rerank))
	if normalized.Rerank == "" {
		normalized.Rerank = "off"
	}
	if normalized.Rerank != "off" && normalized.Rerank != "bm25" {
		return Request{}, fmt.Errorf("query: rerank must be off or bm25")
	}
	normalized.Provider = strings.TrimSpace(normalized.Provider)
	normalized.ProviderOptions = cloneStringMap(normalized.ProviderOptions)
	normalized.FileType = normalizeFileType(normalized.FileType)
	if normalized.FileType == "" {
		normalized.FileType = defaultFileType
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

func validateManifest(config domain.StoreConfig) error {
	if normalizeFileType(config.FileType) == "" {
		config.FileType = defaultFileType
	}
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

func (s *Service) embedQuery(ctx context.Context, config domain.StoreConfig, request Request) ([]float32, error) {
	if normalizeFileType(config.FileType) == "image" || normalizeFileType(request.FileType) == "image" {
		return s.embedImageQuery(ctx, config, request)
	}
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
		Inputs:  []domain.EmbedInput{{ChunkID: queryChunkID, Text: request.Text}},
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

func (s *Service) embedImageQuery(ctx context.Context, config domain.StoreConfig, request Request) ([]float32, error) {
	imagePath := false
	if info, err := s.deps.Stat(request.Text); err == nil && !info.IsDir() && image.SupportedExtension(filepath.Ext(request.Text)) {
		imagePath = true
	}
	vector, err := image.EmbedQuery(ctx, config, request.ProviderOptions, request.Text, imagePath)
	if err != nil {
		return nil, err
	}
	if len(vector) != config.VectorDim {
		return nil, fmt.Errorf("query: provider vector dimension mismatch: expected=%d actual=%d", config.VectorDim, len(vector))
	}
	return vector, nil
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

func normalizeFileType(fileType string) string {
	trimmed := strings.TrimSpace(fileType)
	if trimmed == "" {
		return ""
	}
	if trimmed == "md" {
		return "markdown"
	}
	return trimmed
}

func (s *Service) searchScope(ctx context.Context, scope queryScope, request Request) (index.Snapshot, []float32, []domain.SearchResult, error) {
	snapshot, err := s.deps.LoadIndex(scope.StorageRoot)
	if err != nil {
		switch {
		case errors.Is(err, index.ErrStateNotFound):
			return index.Snapshot{}, nil, nil, fmt.Errorf("query: .vectordb manifests are missing under %s", filepath.Join(scope.StorageRoot, index.DirectoryName))
		case errors.Is(err, index.ErrRebuildRequired):
			return index.Snapshot{}, nil, nil, fmt.Errorf("query: .vectordb manifests are invalid: %w", err)
		default:
			return index.Snapshot{}, nil, nil, err
		}
	}
	snapshot.Config.RootDir = scope.RootDir
	snapshot.Config.RootIdentity = scope.RootIdentity
	snapshot.Config.CollectionID = scope.CollectionID
	snapshot.Config.DataDir = scope.DataDir
	snapshot.Config.FileType = normalizeFileType(snapshot.Config.FileType)
	if snapshot.Config.FileType == "" {
		snapshot.Config.FileType = defaultFileType
	}
	if err := validateManifest(snapshot.Config); err != nil {
		return index.Snapshot{}, nil, nil, err
	}

	queryVector, err := s.embedQuery(ctx, snapshot.Config, request)
	if err != nil {
		return index.Snapshot{}, nil, nil, err
	}

	factory, err := s.deps.GetVectorStore(s.deps.VectorStore)
	if err != nil {
		return index.Snapshot{}, nil, nil, err
	}

	storeConfig := snapshot.Config
	storeConfig.Namespace = s.deps.VectorStore
	store, err := factory(storeConfig)
	if err != nil {
		return index.Snapshot{}, nil, nil, err
	}
	defer func() { _ = store.Close() }()

	pathPrefix := scope.PathPrefix
	if pathPrefix != "" {
		pathPrefix = filepath.Join(scope.RootDir, pathPrefix)
	}

	results, err := store.Search(ctx, domain.SearchQuery{
		Vector:     queryVector,
		Limit:      effectiveSearchWindow(request.Limit, request.SearchWindow),
		Tags:       request.Tags,
		PathPrefix: pathPrefix,
	})
	if err != nil {
		if errors.Is(err, lancedb.ErrVectorDBNotFound) {
			return index.Snapshot{}, nil, nil, fmt.Errorf("query: .vectordb storage is not initialized under %s", filepath.Join(scope.StorageRoot, index.DirectoryName))
		}
		return index.Snapshot{}, nil, nil, err
	}
	return snapshot, queryVector, results, nil
}

func (s *Service) resolveQueryScopes(inputPath string, dataDir string, fileType string) ([]queryScope, error) {
	if strings.TrimSpace(dataDir) == "" {
		paths, err := s.deps.ResolveAppPaths()
		if err != nil {
			return nil, err
		}
		dataDir = paths.DataRoot
	}
	expandedDataDir, err := jcpaths.ExpandUserHome(strings.TrimSpace(dataDir))
	if err != nil {
		return nil, fmt.Errorf("query: resolve data dir: %w", err)
	}
	dataDir = filepath.Clean(expandedDataDir)
	if strings.TrimSpace(inputPath) == "" {
		return s.resolveGlobalQueryScopes(dataDir, fileType)
	}
	return s.resolvePathQueryScopes(inputPath, dataDir, fileType)
}

func (s *Service) resolvePathQueryScopes(inputPath string, dataDir string, fileType string) ([]queryScope, error) {
	match, err := s.deps.ResolveCollection(dataDir, inputPath, fileType)
	if err != nil {
		if errors.Is(err, index.ErrCollectionNotFound) {
			legacyDBPath, legacyFound, legacyErr := s.findLegacyLocalIndex(inputPath)
			if legacyErr != nil {
				return nil, fmt.Errorf("query: inspect legacy local index: %w", legacyErr)
			}
			if legacyFound {
				return nil, fmt.Errorf("query: legacy local index unsupported at %s; run jcemb scan %s to rebuild into unified storage", legacyDBPath, strings.TrimSpace(inputPath))
			}
			return nil, fmt.Errorf("query: path is not indexed: %s", strings.TrimSpace(inputPath))
		}
		return nil, fmt.Errorf("query: resolve path: %w", err)
	}

	return []queryScope{{
		RootDir:      match.RootDir,
		RootIdentity: match.RootIdentity,
		CollectionID: match.CollectionID,
		DataDir:      dataDir,
		StorageRoot:  jcpaths.CollectionStorageDir(dataDir, match.CollectionID),
		PathPrefix:   match.PathPrefix,
	}}, nil
}

func (s *Service) resolveGlobalQueryScopes(dataDir string, fileType string) ([]queryScope, error) {
	registry, err := s.deps.LoadCollections(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, noIndexedCollectionsError(dataDir)
		}
		return nil, fmt.Errorf("query: load collection registry: %w", err)
	}
	if len(registry.Collections) == 0 {
		return nil, noIndexedCollectionsError(dataDir)
	}

	scopes := make([]queryScope, 0, len(registry.Collections))
	for _, entry := range registry.Collections {
		entryFileType := normalizeFileType(entry.FileType)
		if entryFileType == "" {
			entryFileType = defaultFileType
		}
		if entryFileType != normalizeFileType(fileType) {
			continue
		}
		info, statErr := s.deps.Stat(entry.RootDir)
		if statErr != nil || !info.IsDir() {
			continue
		}
		scopes = append(scopes, queryScope{
			RootDir:      entry.RootDir,
			RootIdentity: entry.RootIdentity,
			CollectionID: entry.CollectionID,
			DataDir:      dataDir,
			StorageRoot:  jcpaths.CollectionStorageDir(dataDir, entry.CollectionID),
		})
	}
	if len(scopes) == 0 {
		return nil, noIndexedCollectionsError(dataDir)
	}
	return scopes, nil
}

func noIndexedCollectionsError(dataDir string) error {
	return fmt.Errorf("query: no usable indexed collections in %s; run jcemb scan <path> -r first", dataDir)
}

func resultRootDir(scopes []queryScope) string {
	if len(scopes) == 1 {
		return scopes[0].RootDir
	}
	return ""
}

func combinedManifest(manifests []domain.StoreConfig) domain.StoreConfig {
	if len(manifests) == 0 {
		return domain.StoreConfig{}
	}
	combined := manifests[0]
	for _, manifest := range manifests[1:] {
		if combined.Provider != manifest.Provider {
			combined.Provider = "multiple"
		}
		if combined.Model != manifest.Model {
			combined.Model = "multiple"
		}
		if combined.VectorDim != manifest.VectorDim {
			combined.VectorDim = 0
		}
		if combined.DBVersion != manifest.DBVersion {
			combined.DBVersion = "multiple"
		}
		if combined.FileType != manifest.FileType {
			combined.FileType = "multiple"
		}
	}
	if len(manifests) > 1 {
		combined.RootDir = ""
		combined.RootIdentity = ""
		combined.CollectionID = ""
	}
	return combined
}

func selectFinalResults(queryVectors [][]float32, manifests []domain.StoreConfig, results []domain.SearchResult, limit int, lambda float64) []domain.SearchResult {
	if len(queryVectors) == 0 || !compatibleManifestsForMMR(manifests) {
		return truncateAndRerank(results, limit)
	}
	return mmrSelect(queryVectors[0], results, limit, lambda)
}

func compatibleManifestsForMMR(manifests []domain.StoreConfig) bool {
	if len(manifests) <= 1 {
		return true
	}
	first := manifests[0]
	for _, manifest := range manifests[1:] {
		if first.Provider != manifest.Provider || first.Model != manifest.Model || first.VectorDim != manifest.VectorDim {
			return false
		}
	}
	return true
}

func (s *Service) findLegacyLocalIndex(inputPath string) (string, bool, error) {
	resolved, err := jcpaths.ResolveCollectionRoot(inputPath)
	if err != nil {
		return "", false, err
	}

	for current := resolved.RootDir; ; current = filepath.Dir(current) {
		candidate := filepath.Join(current, index.DirectoryName)
		info, statErr := s.deps.Stat(candidate)
		switch {
		case statErr == nil && info.IsDir():
			return candidate, true, nil
		case statErr != nil && !os.IsNotExist(statErr):
			return "", false, statErr
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
	}
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

package lancedb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/domain"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"github.com/bspiritxp/jcemb/internal/registry"
)

const (
	Name              = "lancedb"
	DBVersion         = "lancedb-v1"
	storageFormatV1   = "v1"
	storageFormatV2   = "v2"
	storageDirName    = ".vectordb"
	storageFileName   = "lancedb.records.json"
	missingVectorDB   = "lancedb: .vectordb not initialized"
	dimensionMismatch = "lancedb: vector dimension mismatch"
)

var (
	ErrVectorDBNotFound  = errors.New(missingVectorDB)
	ErrVectorDimMismatch = errors.New(dimensionMismatch)
)

type Store struct {
	mu          sync.RWMutex
	config      domain.StoreConfig
	storagePath string
	initialized bool
	records     map[string]domain.VectorRecord
	fileStates  map[string]domain.FileState
}

type persistedStore struct {
	Version    string                  `json:"version"`
	Collection persistedCollection     `json:"collection,omitempty"`
	Files      []persistedFileState    `json:"files,omitempty"`
	Records    []persistedVectorRecord `json:"records"`
}

type persistedCollection struct {
	CollectionID    string            `json:"collection_id,omitempty"`
	RootIdentity    string            `json:"root_identity,omitempty"`
	FileType        string            `json:"file_type,omitempty"`
	Provider        string            `json:"provider"`
	ProviderOptions map[string]string `json:"provider_options,omitempty"`
	Model           string            `json:"model"`
	Splitter        string            `json:"splitter"`
	VectorDim       int               `json:"vector_dim"`
	DBVersion       string            `json:"db_version"`
	CreatedAt       time.Time         `json:"created_at"`
	Flags           map[string]bool   `json:"flags,omitempty"`
}

type persistedFileState struct {
	Source        string     `json:"source,omitempty"`
	FilePath      string     `json:"file_path,omitempty"`
	RelPath       string     `json:"rel_path"`
	FileName      string     `json:"file_name,omitempty"`
	DocType       string     `json:"doc_type,omitempty"`
	FileHash      string     `json:"file_hash"`
	ModTime       *time.Time `json:"mod_time,omitempty"`
	RecipeHash    string     `json:"recipe_hash"`
	ChunkIDs      []string   `json:"chunk_ids"`
	ChunkCount    int        `json:"chunk_count"`
	LastIndexedAt time.Time  `json:"last_indexed_at"`
}

type persistedVectorRecord struct {
	Chunk        domain.Chunk `json:"chunk"`
	Vector       []float32    `json:"vector"`
	TagVector    []float32    `json:"tag_vector,omitempty"`
	SemanticTags []string     `json:"semantic_tags,omitempty"`
}

func init() {
	registry.MustRegisterVectorStore(Name, New)
}

func New(config domain.StoreConfig) (domain.VectorStore, error) {
	trimmedRoot := strings.TrimSpace(config.RootDir)
	if trimmedRoot == "" {
		return nil, fmt.Errorf("lancedb: root dir is required")
	}
	if config.VectorDim <= 0 {
		return nil, fmt.Errorf("lancedb: vector_dim must be > 0")
	}

	store := &Store{
		config:      cloneConfig(config),
		storagePath: filepath.Join(resolveStorageRoot(config), storageDirName, storageFileName),
		records:     make(map[string]domain.VectorRecord),
		fileStates:  make(map[string]domain.FileState),
	}

	if err := store.load(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}

	return store, nil
}

func (s *Store) Config() domain.StoreConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneConfig(s.config)
}

func (s *Store) Upsert(ctx context.Context, chunks []domain.VectorRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureLoaded(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	for _, record := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.validateRecord(record); err != nil {
			return err
		}
		s.records[record.Chunk.ID] = cloneVectorRecord(record)
	}

	if err := s.persistLocked(); err != nil {
		return err
	}

	s.initialized = true
	return nil
}

func (s *Store) DeleteBySource(ctx context.Context, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	trimmedSource := strings.TrimSpace(source)
	if trimmedSource == "" {
		return fmt.Errorf("lancedb: source is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureLoaded(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for id, record := range s.records {
		if record.Chunk.Metadata.Source == trimmedSource {
			delete(s.records, id)
		}
	}

	if err := s.persistLocked(); err != nil {
		return err
	}

	s.initialized = true
	return nil
}

func (s *Store) PutFileState(ctx context.Context, state domain.FileState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(state.RelPath) == "" {
		return fmt.Errorf("lancedb: file state rel_path is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureLoaded(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	s.fileStates[state.RelPath] = cloneFileState(state)
	if err := s.persistLocked(); err != nil {
		return err
	}

	s.initialized = true
	return nil
}

func (s *Store) DeleteFileState(ctx context.Context, relPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	trimmedRelPath := strings.TrimSpace(relPath)
	if trimmedRelPath == "" {
		return fmt.Errorf("lancedb: file state rel_path is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureLoaded(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	delete(s.fileStates, trimmedRelPath)
	if err := s.persistLocked(); err != nil {
		return err
	}

	s.initialized = true
	return nil
}

func (s *Store) Snapshot(ctx context.Context) (domain.StoreConfig, []domain.FileState, error) {
	if err := ctx.Err(); err != nil {
		return domain.StoreConfig{}, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureLoaded(); err != nil {
		return domain.StoreConfig{}, nil, err
	}

	return cloneConfig(s.config), cloneFileStatesMap(s.fileStates), nil
}

func (s *Store) Search(ctx context.Context, query domain.SearchQuery) ([]domain.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := s.validateQuery(query); err != nil {
		return nil, err
	}

	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return nil, ErrVectorDBNotFound
	}

	records := make([]domain.VectorRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, cloneVectorRecord(record))
	}
	s.mu.RUnlock()

	results := make([]domain.SearchResult, 0, len(records))
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !matchesTags(record.Chunk.Metadata.Tags, query.Tags) {
			continue
		}
		if !matchesPathPrefix(record.Chunk.Metadata.RelPath, query.PathPrefix) {
			continue
		}

		contentScore := cosineSimilarity(query.Vector, record.Vector)
		tagScore, finalScore := fusedSearchScores(query, record, contentScore)

		results = append(results, domain.SearchResult{
			Chunk:    cloneChunk(record.Chunk),
			Score:    finalScore,
			TagScore: tagScore,
			Vector:   append([]float32(nil), record.Vector...),
		})
	}

	if query.MinScore > 0 {
		filtered := results[:0]
		for _, result := range results {
			if result.Score < query.MinScore {
				continue
			}
			filtered = append(filtered, result)
		}
		results = filtered
	}

	sorted := domain.SortSearchResults(results)
	if query.Limit > 0 && len(sorted) > query.Limit {
		sorted = sorted[:query.Limit]
	}

	return sorted, nil
}

func (s *Store) FindBySource(ctx context.Context, source string) ([]domain.VectorRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	trimmedSource := strings.TrimSpace(source)
	if trimmedSource == "" {
		return nil, fmt.Errorf("lancedb: source is required")
	}

	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return nil, ErrVectorDBNotFound
	}

	results := make([]domain.VectorRecord, 0)
	for _, record := range s.records {
		if record.Chunk.Metadata.Source == trimmedSource {
			results = append(results, cloneVectorRecord(record))
		}
	}
	s.mu.RUnlock()

	return results, nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) validateRecord(record domain.VectorRecord) error {
	if strings.TrimSpace(record.Chunk.ID) == "" {
		return fmt.Errorf("lancedb: chunk id is required")
	}
	if err := record.Chunk.Metadata.Validate(); err != nil {
		return err
	}
	if len(record.Vector) != s.config.VectorDim {
		return fmt.Errorf("%w: expected=%d actual=%d", ErrVectorDimMismatch, s.config.VectorDim, len(record.Vector))
	}
	return nil
}

func (s *Store) validateQuery(query domain.SearchQuery) error {
	if len(query.Vector) != s.config.VectorDim {
		return fmt.Errorf("%w: expected=%d actual=%d", ErrVectorDimMismatch, s.config.VectorDim, len(query.Vector))
	}
	return nil
}

func (s *Store) ensureLoaded() error {
	if s.initialized {
		return nil
	}
	return s.load()
}

func (s *Store) load() error {
	persisted, err := readPersistedStore(s.storagePath)
	if err != nil {
		return err
	}

	config := cloneConfig(s.config)
	if persisted.Version == storageFormatV2 {
		config = persisted.Collection.toDomain(config)
	}

	loaded := make(map[string]domain.VectorRecord, len(persisted.Records))
	for index, entry := range persisted.Records {
		record := domain.VectorRecord{
			Chunk:        cloneChunk(entry.Chunk),
			Vector:       append([]float32(nil), entry.Vector...),
			TagVector:    append([]float32(nil), entry.TagVector...),
			SemanticTags: append([]string(nil), entry.SemanticTags...),
		}
		if err := s.validateRecord(record); err != nil {
			return fmt.Errorf("lancedb: records[%d]: %w", index, err)
		}
		loaded[record.Chunk.ID] = record
	}

	states := make(map[string]domain.FileState, len(persisted.Files))
	for index, entry := range persisted.Files {
		state, err := entry.toDomain()
		if err != nil {
			return fmt.Errorf("lancedb: files[%d]: %w", index, err)
		}
		states[state.RelPath] = state
	}

	s.config = config
	s.records = loaded
	s.fileStates = states
	s.initialized = true
	return nil
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.storagePath), 0o755); err != nil {
		return fmt.Errorf("lancedb: create storage directory: %w", err)
	}

	records := make([]persistedVectorRecord, 0, len(s.records))
	ids := make([]string, 0, len(s.records))
	for id := range s.records {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		record := s.records[id]
		records = append(records, persistedVectorRecord{
			Chunk:        cloneChunk(record.Chunk),
			Vector:       append([]float32(nil), record.Vector...),
			TagVector:    append([]float32(nil), record.TagVector...),
			SemanticTags: append([]string(nil), record.SemanticTags...),
		})
	}

	files := make([]persistedFileState, 0, len(s.fileStates))
	paths := make([]string, 0, len(s.fileStates))
	for relPath := range s.fileStates {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	for _, relPath := range paths {
		files = append(files, persistedFileStateFromDomain(s.fileStates[relPath]))
	}

	file, err := os.CreateTemp(filepath.Dir(s.storagePath), storageFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("lancedb: create temp store file: %w", err)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(persistedStore{
		Version:    storageFormatV2,
		Collection: persistedCollectionFromDomain(s.config),
		Files:      files,
		Records:    records,
	}); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return fmt.Errorf("lancedb: encode store: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return fmt.Errorf("lancedb: close temp store file: %w", err)
	}
	if err := os.Rename(file.Name(), s.storagePath); err != nil {
		_ = os.Remove(file.Name())
		return fmt.Errorf("lancedb: replace store file: %w", err)
	}

	return nil
}

func LoadSnapshot(rootDir string) (domain.StoreConfig, []domain.FileState, error) {
	persisted, storagePath, err := loadPersistedSnapshot(rootDir)
	if err != nil {
		return domain.StoreConfig{}, nil, err
	}
	if persisted.Version != storageFormatV2 {
		return domain.StoreConfig{}, nil, fmt.Errorf("lancedb: storage metadata unavailable for store version %q", persisted.Version)
	}

	config := persisted.Collection.toDomain(domain.StoreConfig{RootDir: strings.TrimSpace(rootDir), Namespace: Name})
	if dataDir, ok := resolveDataDirFromSnapshotPath(storagePath); ok {
		config.DataDir = dataDir
	}
	states := make([]domain.FileState, 0, len(persisted.Files))
	for index, entry := range persisted.Files {
		state, err := entry.toDomain()
		if err != nil {
			return domain.StoreConfig{}, nil, fmt.Errorf("lancedb: files[%d]: %w", index, err)
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i int, j int) bool {
		return strings.Compare(states[i].RelPath, states[j].RelPath) < 0
	})
	return config, states, nil
}

func loadPersistedSnapshot(rootDir string) (persistedStore, string, error) {
	globalPath, err := resolveSnapshotPath(rootDir)
	if err != nil {
		return persistedStore{}, "", err
	}
	persisted, readErr := readPersistedStore(globalPath)
	switch {
	case readErr == nil:
		return persisted, globalPath, nil
	case !errors.Is(readErr, os.ErrNotExist):
		return persistedStore{}, "", readErr
	}
	return persistedStore{}, "", readErr
}

func resolveSnapshotPath(rootDir string) (string, error) {
	cleanRoot := filepath.Clean(strings.TrimSpace(rootDir))
	if isUnifiedStorageRoot(cleanRoot) {
		return filepath.Join(cleanRoot, storageDirName, storageFileName), nil
	}

	resolved, err := jcpaths.ResolveCollectionRoot(rootDir)
	if err != nil {
		return "", nil
	}
	dataRoot, err := resolveDataRoot("")
	if err != nil {
		return "", err
	}
	collectionID := jcpaths.CollectionIDForRoot(resolved.Identity)
	if collectionID == "" {
		return "", nil
	}
	return filepath.Join(jcpaths.CollectionStorageDir(dataRoot, collectionID), storageDirName, storageFileName), nil
}

func resolveDataDirFromSnapshotPath(storagePath string) (string, bool) {
	cleanPath := filepath.Clean(strings.TrimSpace(storagePath))
	if cleanPath == "" || cleanPath == "." {
		return "", false
	}

	storageDir := filepath.Dir(cleanPath)
	if filepath.Base(storageDir) != storageDirName {
		return "", false
	}

	collectionDir := filepath.Dir(storageDir)
	if filepath.Base(filepath.Dir(collectionDir)) != "collections" {
		return "", false
	}

	dataRoot := filepath.Dir(filepath.Dir(collectionDir))
	if strings.TrimSpace(dataRoot) == "" || dataRoot == "." {
		return "", false
	}

	return dataRoot, true
}

func resolveStorageRoot(config domain.StoreConfig) string {
	dataRoot, err := resolveDataRoot(config.DataDir)
	if err == nil && strings.TrimSpace(config.CollectionID) != "" {
		return jcpaths.CollectionStorageDir(dataRoot, config.CollectionID)
	}
	return strings.TrimSpace(config.RootDir)
}

func resolveDataRoot(explicit string) (string, error) {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		expanded, err := jcpaths.ExpandUserHome(trimmed)
		if err != nil {
			return "", err
		}
		return filepath.Clean(expanded), nil
	}
	runtime, err := config.Load()
	if err != nil {
		return "", err
	}
	return filepath.Clean(runtime.Settings.DataDir), nil
}

func isUnifiedStorageRoot(rootDir string) bool {
	parent := filepath.Dir(rootDir)
	return filepath.Base(parent) == "collections" && filepath.Base(rootDir) != "." && filepath.Base(rootDir) != string(filepath.Separator)
}

func readPersistedStore(path string) (persistedStore, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return persistedStore{}, err
	}

	var persisted persistedStore
	if err := json.Unmarshal(payload, &persisted); err != nil {
		return persistedStore{}, fmt.Errorf("lancedb: decode store: %w", err)
	}
	version := strings.TrimSpace(persisted.Version)
	if version != storageFormatV1 && version != storageFormatV2 {
		return persistedStore{}, fmt.Errorf("lancedb: unsupported store version %q", persisted.Version)
	}
	return persisted, nil
}

func persistedCollectionFromDomain(config domain.StoreConfig) persistedCollection {
	return persistedCollection{
		CollectionID:    strings.TrimSpace(config.CollectionID),
		RootIdentity:    strings.TrimSpace(config.RootIdentity),
		FileType:        strings.TrimSpace(config.FileType),
		Provider:        strings.TrimSpace(config.Provider),
		ProviderOptions: safeProviderOptions(config.ProviderOptions),
		Model:           strings.TrimSpace(config.Model),
		Splitter:        strings.TrimSpace(config.Splitter),
		VectorDim:       config.VectorDim,
		DBVersion:       strings.TrimSpace(config.DBVersion),
		CreatedAt:       config.CreatedAt.UTC(),
		Flags:           cloneBoolMap(config.Flags),
	}
}

func (p persistedCollection) toDomain(base domain.StoreConfig) domain.StoreConfig {
	config := cloneConfig(base)
	config.CollectionID = strings.TrimSpace(p.CollectionID)
	config.RootIdentity = strings.TrimSpace(p.RootIdentity)
	config.FileType = strings.TrimSpace(p.FileType)
	config.Provider = strings.TrimSpace(p.Provider)
	config.ProviderOptions = cloneStringMap(p.ProviderOptions)
	config.Model = strings.TrimSpace(p.Model)
	config.Splitter = strings.TrimSpace(p.Splitter)
	config.VectorDim = p.VectorDim
	config.DBVersion = strings.TrimSpace(p.DBVersion)
	config.CreatedAt = p.CreatedAt
	config.Flags = cloneBoolMap(p.Flags)
	return config
}

func persistedFileStateFromDomain(state domain.FileState) persistedFileState {
	entry := persistedFileState{
		Source:        state.Source,
		FilePath:      state.FilePath,
		RelPath:       state.RelPath,
		FileName:      state.FileName,
		DocType:       state.DocType,
		FileHash:      state.FileHash,
		RecipeHash:    state.RecipeHash,
		ChunkIDs:      append([]string(nil), state.ChunkIDs...),
		ChunkCount:    state.ChunkCount,
		LastIndexedAt: state.LastIndexedAt.UTC(),
	}
	if !state.ModTime.IsZero() {
		modTime := state.ModTime.UTC()
		entry.ModTime = &modTime
	}
	return entry
}

func (p persistedFileState) toDomain() (domain.FileState, error) {
	state := domain.FileState{
		Source:        p.Source,
		FilePath:      p.FilePath,
		RelPath:       strings.TrimSpace(p.RelPath),
		FileName:      p.FileName,
		DocType:       p.DocType,
		FileHash:      strings.TrimSpace(p.FileHash),
		RecipeHash:    strings.TrimSpace(p.RecipeHash),
		ChunkIDs:      append([]string(nil), p.ChunkIDs...),
		ChunkCount:    p.ChunkCount,
		LastIndexedAt: p.LastIndexedAt,
	}
	if p.ModTime != nil {
		state.ModTime = p.ModTime.UTC()
	}
	if state.RelPath == "" {
		return domain.FileState{}, fmt.Errorf("rel_path is required")
	}
	if state.FileHash == "" {
		return domain.FileState{}, fmt.Errorf("file_hash is required")
	}
	if state.RecipeHash == "" {
		return domain.FileState{}, fmt.Errorf("recipe_hash is required")
	}
	if state.ChunkCount < 0 {
		return domain.FileState{}, fmt.Errorf("chunk_count must be >= 0")
	}
	if len(state.ChunkIDs) != state.ChunkCount {
		return domain.FileState{}, fmt.Errorf("chunk_count must match chunk_ids length")
	}
	if state.LastIndexedAt.IsZero() {
		return domain.FileState{}, fmt.Errorf("last_indexed_at is required")
	}
	for index, chunkID := range state.ChunkIDs {
		if strings.TrimSpace(chunkID) == "" {
			return domain.FileState{}, fmt.Errorf("chunk_ids[%d] is required", index)
		}
	}
	return state, nil
}

func matchesTags(recordTags []string, queryTags []string) bool {
	if len(queryTags) == 0 {
		return true
	}

	available := make(map[string]struct{}, len(recordTags))
	for _, tag := range recordTags {
		available[strings.TrimSpace(strings.ToLower(tag))] = struct{}{}
	}

	for _, tag := range domain.NormalizeTags(queryTags) {
		if _, ok := available[tag]; !ok {
			return false
		}
	}

	return true
}

func matchesPathPrefix(relPath string, prefix string) bool {
	normalizedPrefix := normalizePathPrefix(prefix)
	if normalizedPrefix == "" {
		return true
	}

	normalizedPath := normalizePathPrefix(relPath)
	if normalizedPath == normalizedPrefix {
		return true
	}

	return strings.HasPrefix(normalizedPath, normalizedPrefix+"/")
}

func normalizePathPrefix(value string) string {
	return jcpaths.NormalizeStoredPath(value)
}

func cosineSimilarity(left []float32, right []float32) float64 {
	var dot float64
	var leftNorm float64
	var rightNorm float64

	for index := range left {
		leftValue := float64(left[index])
		rightValue := float64(right[index])
		dot += leftValue * rightValue
		leftNorm += leftValue * leftValue
		rightNorm += rightValue * rightValue
	}

	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}

	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func fusedSearchScores(query domain.SearchQuery, record domain.VectorRecord, contentScore float64) (tagScore float64, finalScore float64) {
	if !canFuseTagScore(query, record) {
		return 0, contentScore
	}

	tagScore = cosineSimilarity(query.TagVector, record.TagVector)
	finalScore = ((1 - query.TagWeight) * contentScore) + (query.TagWeight * tagScore)
	return tagScore, finalScore
}

func canFuseTagScore(query domain.SearchQuery, record domain.VectorRecord) bool {
	if !query.UseTagFusion {
		return false
	}
	if len(query.TagVector) == 0 || len(record.TagVector) == 0 {
		return false
	}
	return len(query.TagVector) == len(record.TagVector)
}

func cloneConfig(config domain.StoreConfig) domain.StoreConfig {
	cloned := config
	cloned.Flags = cloneBoolMap(config.Flags)
	cloned.ProviderOptions = cloneStringMap(config.ProviderOptions)
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func safeProviderOptions(values map[string]string) map[string]string {
	cloned := cloneStringMap(values)
	delete(cloned, "openai_api_key")
	return cloned
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	if values == nil {
		return nil
	}
	cloned := make(map[string]bool, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneVectorRecord(record domain.VectorRecord) domain.VectorRecord {
	return domain.VectorRecord{
		Chunk:        cloneChunk(record.Chunk),
		Vector:       append([]float32(nil), record.Vector...),
		TagVector:    append([]float32(nil), record.TagVector...),
		SemanticTags: append([]string(nil), record.SemanticTags...),
	}
}

func cloneChunk(chunk domain.Chunk) domain.Chunk {
	cloned := chunk
	cloned.Document = domain.Document{
		Source:    chunk.Document.Source,
		FilePath:  chunk.Document.FilePath,
		RelPath:   chunk.Document.RelPath,
		FileName:  chunk.Document.FileName,
		DocType:   chunk.Document.DocType,
		FileHash:  chunk.Document.FileHash,
		Title:     chunk.Document.Title,
		Content:   chunk.Document.Content,
		TitlePath: append([]string(nil), chunk.Document.TitlePath...),
		Tags:      append([]string(nil), chunk.Document.Tags...),
		YAML:      cloneMap(chunk.Document.YAML),
	}
	cloned.Metadata = domain.ChunkMetadata{
		Source:     chunk.Metadata.Source,
		FilePath:   chunk.Metadata.FilePath,
		RelPath:    chunk.Metadata.RelPath,
		FileName:   chunk.Metadata.FileName,
		DocType:    chunk.Metadata.DocType,
		FileHash:   chunk.Metadata.FileHash,
		Title:      chunk.Metadata.Title,
		ChunkIndex: chunk.Metadata.ChunkIndex,
		TitlePath:  append([]string(nil), chunk.Metadata.TitlePath...),
		Tags:       append([]string(nil), chunk.Metadata.Tags...),
		YAML:       cloneMap(chunk.Metadata.YAML),
	}
	return cloned
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneFileState(state domain.FileState) domain.FileState {
	cloned := state
	cloned.ChunkIDs = append([]string(nil), state.ChunkIDs...)
	return cloned
}

func cloneFileStatesMap(states map[string]domain.FileState) []domain.FileState {
	cloned := make([]domain.FileState, 0, len(states))
	for _, state := range states {
		cloned = append(cloned, cloneFileState(state))
	}
	sort.Slice(cloned, func(i int, j int) bool {
		return strings.Compare(cloned[i].RelPath, cloned[j].RelPath) < 0
	})
	return cloned
}

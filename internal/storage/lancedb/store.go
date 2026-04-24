package lancedb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/bspiritxp/jcemb/internal/index"
	"github.com/bspiritxp/jcemb/internal/registry"
)

const (
	Name              = "lancedb"
	DBVersion         = "lancedb-v1"
	storageFormatV1   = "v1"
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
}

type persistedStore struct {
	Version string                  `json:"version"`
	Records []persistedVectorRecord `json:"records"`
}

type persistedVectorRecord struct {
	Chunk  domain.Chunk `json:"chunk"`
	Vector []float32    `json:"vector"`
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
		storagePath: filepath.Join(trimmedRoot, index.DirectoryName, storageFileName),
		records:     make(map[string]domain.VectorRecord),
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

		results = append(results, domain.SearchResult{
			Chunk:  cloneChunk(record.Chunk),
			Score:  cosineSimilarity(query.Vector, record.Vector),
			Vector: append([]float32(nil), record.Vector...),
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
	payload, err := os.ReadFile(s.storagePath)
	if err != nil {
		return err
	}

	var persisted persistedStore
	if err := json.Unmarshal(payload, &persisted); err != nil {
		return fmt.Errorf("lancedb: decode store: %w", err)
	}
	if strings.TrimSpace(persisted.Version) != storageFormatV1 {
		return fmt.Errorf("lancedb: unsupported store version %q", persisted.Version)
	}

	loaded := make(map[string]domain.VectorRecord, len(persisted.Records))
	for index, entry := range persisted.Records {
		record := domain.VectorRecord{
			Chunk:  cloneChunk(entry.Chunk),
			Vector: append([]float32(nil), entry.Vector...),
		}
		if err := s.validateRecord(record); err != nil {
			return fmt.Errorf("lancedb: records[%d]: %w", index, err)
		}
		loaded[record.Chunk.ID] = record
	}

	s.records = loaded
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
			Chunk:  cloneChunk(record.Chunk),
			Vector: append([]float32(nil), record.Vector...),
		})
	}

	file, err := os.CreateTemp(filepath.Dir(s.storagePath), storageFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("lancedb: create temp store file: %w", err)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(persistedStore{Version: storageFormatV1, Records: records}); err != nil {
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
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" || trimmed == "." {
		return ""
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." {
		return ""
	}
	return strings.TrimPrefix(cleaned, "./")
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

func cloneConfig(config domain.StoreConfig) domain.StoreConfig {
	cloned := config
	if config.Flags != nil {
		cloned.Flags = make(map[string]bool, len(config.Flags))
		for key, value := range config.Flags {
			cloned.Flags[key] = value
		}
	}
	return cloned
}

func cloneVectorRecord(record domain.VectorRecord) domain.VectorRecord {
	return domain.VectorRecord{
		Chunk:  cloneChunk(record.Chunk),
		Vector: append([]float32(nil), record.Vector...),
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

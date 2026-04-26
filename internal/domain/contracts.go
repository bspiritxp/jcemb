package domain

import (
	"context"
	"sort"
	"strings"
	"time"
)

type Document struct {
	Source    string
	FilePath  string
	RelPath   string
	FileName  string
	DocType   string
	FileHash  string
	Title     string
	Content   string
	TitlePath []string
	Tags      []string
	YAML      map[string]any
}

type Chunk struct {
	ID                 string
	Document           Document
	Content            string
	Metadata           ChunkMetadata
	RecipeHash         string
	SectionFingerprint string
	CreatedAt          time.Time
}

type EmbedInput struct {
	ChunkID  string
	Text     string
	Metadata ChunkMetadata
}

type EmbedRequest struct {
	Recipe EmbedRecipe
	Inputs []EmbedInput
}

type Embedding struct {
	ChunkID string
	Vector  []float32
}

type VectorRecord struct {
	Chunk  Chunk
	Vector []float32
}

type EmbeddedChunk = VectorRecord

type SearchQuery struct {
	Vector     []float32
	Limit      int
	Tags       []string
	PathPrefix string
	MinScore   float64
}

type SearchResult struct {
	Chunk  Chunk
	Score  float64
	Rank   int
	Vector []float32
}

type SearchResults []SearchResult

func (r SearchResults) Len() int {
	return len(r)
}

func (r SearchResults) Less(i, j int) bool {
	if r[i].Score != r[j].Score {
		return r[i].Score > r[j].Score
	}
	if r[i].Chunk.Metadata.RelPath != r[j].Chunk.Metadata.RelPath {
		return strings.Compare(r[i].Chunk.Metadata.RelPath, r[j].Chunk.Metadata.RelPath) < 0
	}
	if r[i].Chunk.Metadata.ChunkIndex != r[j].Chunk.Metadata.ChunkIndex {
		return r[i].Chunk.Metadata.ChunkIndex < r[j].Chunk.Metadata.ChunkIndex
	}
	return strings.Compare(r[i].Chunk.ID, r[j].Chunk.ID) < 0
}

func (r SearchResults) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}

func SortSearchResults(results []SearchResult) []SearchResult {
	sorted := append([]SearchResult(nil), results...)
	sort.Sort(SearchResults(sorted))
	for index := range sorted {
		sorted[index].Rank = index + 1
	}
	return sorted
}

type StoreConfig struct {
	CollectionID    string
	RootIdentity    string
	RootDir         string
	DataDir         string
	Namespace       string
	Provider        string
	Model           string
	Splitter        string
	VectorDim       int
	ManifestVersion string
	DBVersion       string
	CreatedAt       time.Time
	Flags           map[string]bool
}

type IndexRecord struct {
	ChunkID            string
	Source             string
	RelPath            string
	FileHash           string
	RecipeHash         string
	ChunkIndex         int
	SectionFingerprint string
}

type FileState struct {
	Source        string
	FilePath      string
	RelPath       string
	FileName      string
	DocType       string
	FileHash      string
	ModTime       time.Time
	RecipeHash    string
	ChunkIDs      []string
	ChunkCount    int
	LastIndexedAt time.Time
}

type Splitter interface {
	Split(ctx context.Context, document Document) ([]Chunk, error)
}

type EmbedderProvider interface {
	Name() string
	NewEmbedder(model ModelSpec) (Embedder, error)
}

type Embedder interface {
	Provider() string
	Model() ModelSpec
	Embed(ctx context.Context, request EmbedRequest) ([]Embedding, error)
}

type VectorStore interface {
	Config() StoreConfig
	Upsert(ctx context.Context, chunks []VectorRecord) error
	DeleteBySource(ctx context.Context, source string) error
	PutFileState(ctx context.Context, state FileState) error
	DeleteFileState(ctx context.Context, relPath string) error
	Snapshot(ctx context.Context) (StoreConfig, []FileState, error)
	Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
	Close() error
}

type Indexer interface {
	Get(ctx context.Context, relPath string) (FileState, bool, error)
	Put(ctx context.Context, state FileState) error
	Delete(ctx context.Context, relPath string) error
	List(ctx context.Context) ([]FileState, error)
}

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

type SourceFile struct {
	RootDir  string
	FilePath string
	RelPath  string
	FileName string
	DocType  string
	ModTime  time.Time
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

// EmbedPurpose 描述本次 embedding 调用的语义意图，部分 provider 会根据它
// 调整请求参数（例如 Voyage / Jina 的 input_type）。零值表示未指定，
// provider 应该回退到自己的默认行为。
type EmbedPurpose string

const (
	// EmbedPurposeUnspecified 表示调用方未给出语义信息。
	EmbedPurposeUnspecified EmbedPurpose = ""
	// EmbedPurposeDocument 表示输入是被索引的文档片段（写侧）。
	EmbedPurposeDocument EmbedPurpose = "document"
	// EmbedPurposeQuery 表示输入是用户的检索查询（读侧）。
	EmbedPurposeQuery EmbedPurpose = "query"
)

type EmbedRequest struct {
	Recipe  EmbedRecipe
	Inputs  []EmbedInput
	Purpose EmbedPurpose
}

type Embedding struct {
	ChunkID string
	Vector  []float32
}

type VectorRecord struct {
	Chunk        Chunk
	Vector       []float32
	TagVector    []float32
	SemanticTags []string
}

type EmbeddedChunk = VectorRecord

type SearchQuery struct {
	Vector       []float32
	TagVector    []float32
	Limit        int
	Tags         []string
	PathPrefix   string
	MinScore     float64
	TagWeight    float64
	UseTagFusion bool
}

type SearchResult struct {
	Chunk             Chunk
	Score             float64
	ContentScore      float64
	HasContentScore   bool
	TagScore          float64
	HasTagScore       bool
	PreRerankScore    float64
	HasPreRerankScore bool
	BM25Score         float64
	BM25Norm          float64
	SemanticNorm      float64
	MMRRelevance      float64
	MMRDiversity      float64
	MMRScore          float64
	Rank              int
	Vector            []float32
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
	FileType        string
	Namespace       string
	Provider        string
	ProviderOptions map[string]string
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

type ScanProviderConfig struct {
	FileType        string
	DataDir         string
	Provider        string
	ProviderOptions map[string]string
	Model           string
	TagExtractor    TagExtractorConfig
	Recursive       bool
	Force           bool
}

type ScanProviderRequest struct {
	File            SourceFile
	Config          ScanProviderConfig
	Recipe          EmbedRecipe
	Now             func() time.Time
	GetProvider     func(name string) (func(ProviderConfig) (EmbedderProvider, error), error)
	GetSplitter     func(name string) (func(SplitterSpec) (Splitter, error), error)
	GetTagExtractor func(config TagExtractorConfig) (TagExtractor, error)
}

type ScanProviderResult struct {
	State   FileState
	Records []VectorRecord
}

type ScanProvider interface {
	FileType() string
	Extensions() []string
	Recipe(ScanProviderConfig) EmbedRecipe
	BuildRecords(context.Context, ScanProviderRequest) (ScanProviderResult, error)
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
	FindBySource(ctx context.Context, source string) ([]VectorRecord, error)
	Close() error
}

type Indexer interface {
	Get(ctx context.Context, relPath string) (FileState, bool, error)
	Put(ctx context.Context, state FileState) error
	Delete(ctx context.Context, relPath string) error
	List(ctx context.Context) ([]FileState, error)
}

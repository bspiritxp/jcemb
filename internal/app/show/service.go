package show

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/index"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"github.com/bspiritxp/jcemb/internal/registry"
)

type Request struct {
	FilePath string
	DataDir  string
}

type ChunkInfo struct {
	ChunkID   string
	Tags      []string
	VectorLen int
	Title     string
	Content   string
}

type FileInfo struct {
	FilePath   string
	RelPath    string
	FileName   string
	DocType    string
	FileHash   string
	ChunkCount int
}

type CollectionInfo struct {
	CollectionID string
	RootDir      string
	Provider     string
	Model        string
	VectorDim    int
	FileType     string
}

type Result struct {
	Found      bool
	File       FileInfo
	Collection CollectionInfo
	Chunks     []ChunkInfo
}

type Dependencies struct {
	LoadCollections func(dataRoot string) (index.CollectionRegistry, error)
	LoadIndex       func(rootDir string) (index.Snapshot, error)
	ResolveAppPaths func() (jcpaths.AppPaths, error)
	GetVectorStore  func(name string) (registry.VectorStoreFactory, error)
	Stat            func(name string) (os.FileInfo, error)
	VectorStore     string
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	if deps.LoadCollections == nil {
		deps.LoadCollections = index.LoadCollectionRegistry
	}
	if deps.LoadIndex == nil {
		deps.LoadIndex = index.Load
	}
	if deps.ResolveAppPaths == nil {
		deps.ResolveAppPaths = jcpaths.ResolveAppPaths
	}
	if deps.GetVectorStore == nil {
		deps.GetVectorStore = registry.GetVectorStore
	}
	if deps.Stat == nil {
		deps.Stat = os.Stat
	}
	if strings.TrimSpace(deps.VectorStore) == "" {
		deps.VectorStore = "lancedb"
	}
	return &Service{deps: deps}
}

func (s *Service) Run(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	filePath, err := resolveFilePath(request.FilePath)
	if err != nil {
		return Result{}, err
	}

	dataDir, err := resolveDataDir(request.DataDir)
	if err != nil {
		return Result{}, err
	}

	registry, err := s.deps.LoadCollections(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Found: false}, nil
		}
		return Result{}, fmt.Errorf("show: load collections: %w", err)
	}

	for _, entry := range registry.Collections {
		result, found, err := s.checkCollection(ctx, dataDir, entry, filePath)
		if err != nil {
			return Result{}, err
		}
		if found {
			return result, nil
		}
	}

	return Result{Found: false}, nil
}

func (s *Service) checkCollection(ctx context.Context, dataDir string, entry index.CollectionEntry, filePath string) (Result, bool, error) {
	relPath, belongs := computeRelPath(entry.RootDir, filePath)
	if !belongs {
		return Result{}, false, nil
	}

	storageRoot := jcpaths.CollectionStorageDir(dataDir, entry.CollectionID)
	snapshot, err := s.deps.LoadIndex(storageRoot)
	if err != nil {
		return Result{}, false, nil
	}

	factory, err := s.deps.GetVectorStore(s.deps.VectorStore)
	if err != nil {
		return Result{}, false, fmt.Errorf("show: vector store %q not available: %w", s.deps.VectorStore, err)
	}

	storeConfig := snapshot.Config
	storeConfig.CollectionID = entry.CollectionID
	storeConfig.RootDir = entry.RootDir
	storeConfig.DataDir = dataDir
	storeConfig.Namespace = s.deps.VectorStore
	store, err := factory(storeConfig)
	if err != nil {
		return Result{}, false, fmt.Errorf("show: open store for %s: %w", entry.CollectionID, err)
	}
	defer func() { _ = store.Close() }()

	records, err := store.FindBySource(ctx, relPath)
	if err != nil {
		return Result{}, false, fmt.Errorf("show: find by source: %w", err)
	}

	if len(records) == 0 {
		records, err = store.FindBySource(ctx, filePath)
		if err != nil {
			return Result{}, false, fmt.Errorf("show: find by source: %w", err)
		}
	}

	if len(records) == 0 {
		return Result{}, false, nil
	}

	var fileInfo FileInfo
	for _, state := range snapshot.Files {
		if state.RelPath == relPath {
			fileInfo = FileInfo{
				FilePath:   state.FilePath,
				RelPath:    state.RelPath,
				FileName:   state.FileName,
				DocType:    state.DocType,
				FileHash:   state.FileHash,
				ChunkCount: state.ChunkCount,
			}
			break
		}
	}

	if fileInfo.RelPath == "" {
		fileInfo = FileInfo{
			FilePath: filePath,
			RelPath:  relPath,
			FileName: filepath.Base(filePath),
			DocType:  records[0].Chunk.Metadata.DocType,
		}
	}

	chunks := make([]ChunkInfo, 0, len(records))
	for _, record := range records {
		chunks = append(chunks, ChunkInfo{
			ChunkID:   record.Chunk.ID,
			Tags:      append([]string(nil), record.Chunk.Metadata.Tags...),
			VectorLen: len(record.Vector),
			Title:     record.Chunk.Metadata.Title,
			Content:   record.Chunk.Content,
		})
	}

	return Result{
		Found: true,
		File:  fileInfo,
		Collection: CollectionInfo{
			CollectionID: entry.CollectionID,
			RootDir:      entry.RootDir,
			Provider:     snapshot.Config.Provider,
			Model:        snapshot.Config.Model,
			VectorDim:    snapshot.Config.VectorDim,
			FileType:     normalizeFileType(entry.FileType),
		},
		Chunks: chunks,
	}, true, nil
}

func resolveFilePath(input string) (string, error) {
	absPath, err := jcpaths.ResolveAbsolutePath(input)
	if err != nil {
		return "", fmt.Errorf("show: resolve file path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return absPath, nil
		}
		return "", fmt.Errorf("show: stat file: %w", err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("show: %s is a directory, expected a file", absPath)
	}

	return absPath, nil
}

func normalizeFileType(fileType string) string {
	trimmed := strings.TrimSpace(fileType)
	if trimmed == "" || trimmed == "md" {
		return "markdown"
	}
	return trimmed
}

func resolveDataDir(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = config.DefaultSettings().DataDir
	}
	expanded, err := jcpaths.ExpandUserHome(trimmed)
	if err != nil {
		return "", fmt.Errorf("show: data dir: %w", err)
	}
	return filepath.Clean(expanded), nil
}

func computeRelPath(rootDir, filePath string) (string, bool) {
	cleanRoot := filepath.Clean(rootDir)
	cleanFile := filepath.Clean(filePath)

	if cleanRoot == cleanFile {
		return filepath.Base(cleanFile), true
	}

	sep := string(filepath.Separator)
	if !strings.HasPrefix(cleanFile, cleanRoot+sep) {
		return "", false
	}

	rel, err := filepath.Rel(cleanRoot, cleanFile)
	if err != nil {
		return "", false
	}

	return jcpaths.NormalizeStoredPath(rel), true
}

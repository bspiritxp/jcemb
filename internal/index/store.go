package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bspiritxp/jcemb/internal/domain"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
	"github.com/bspiritxp/jcemb/internal/storage/lancedb"
)

const (
	DirectoryName   = ".vectordb"
	IndexFileName   = "index.json"
	ConfigFileName  = "config.json"
	SchemaVersionV1 = "v1"
)

var (
	ErrStateNotFound   = errors.New("index: state not found")
	ErrRebuildRequired = errors.New("index: rebuild required")
)

type Snapshot struct {
	Config domain.StoreConfig
	Files  []domain.FileState
}

type fileManifest struct {
	Version    string              `json:"version"`
	Generation string              `json:"generation"`
	Files      []fileManifestEntry `json:"files"`
}

type fileManifestEntry struct {
	Path       string     `json:"path"`
	FileHash   string     `json:"file_hash"`
	ModTime    *time.Time `json:"mod_time,omitempty"`
	RecipeHash string     `json:"recipe_hash"`
	ChunkIDs   []string   `json:"chunk_ids"`
	ChunkCount int        `json:"chunk_count"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type ConfigManifest struct {
	CollectionID    string            `json:"collection_id,omitempty"`
	RootIdentity    string            `json:"root_identity,omitempty"`
	FileType        string            `json:"file_type,omitempty"`
	Version         string            `json:"version"`
	Generation      string            `json:"generation"`
	Provider        string            `json:"provider"`
	ProviderOptions map[string]string `json:"provider_options,omitempty"`
	Model           string            `json:"model"`
	Splitter        string            `json:"splitter"`
	VectorDim       int               `json:"vector_dim"`
	DBVersion       string            `json:"db_version"`
	CreatedAt       time.Time         `json:"created_at"`
}

func Load(rootDir string) (Snapshot, error) {
	storageConfig, storageFiles, storageErr := lancedb.LoadSnapshot(rootDir)
	switch {
	case storageErr == nil:
		return Snapshot{Config: storageConfig, Files: storageFiles}, nil
	case !errors.Is(storageErr, os.ErrNotExist):
		return Snapshot{}, fmt.Errorf("%w: storage metadata unreadable: %v", ErrRebuildRequired, storageErr)
	default:
		return Snapshot{}, ErrStateNotFound
	}
}

func Save(rootDir string, config domain.StoreConfig, files []domain.FileState) error {
	manifestDir := dbPath(rootDir)
	if candidate, err := collectionDBPath(config); err != nil {
		return err
	} else if candidate != "" {
		manifestDir = candidate
	}
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return fmt.Errorf("index: create manifest directory: %w", err)
	}

	createdAt := config.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	generation := strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	configManifest := configManifestFromDomain(config, generation, createdAt)
	if err := configManifest.validate(); err != nil {
		return err
	}

	fileManifest := fileManifestFromDomain(files, generation)
	if err := fileManifest.validate(); err != nil {
		return err
	}

	configTemp, err := writeAtomicJSON(manifestDir, ConfigFileName, configManifest)
	if err != nil {
		return err
	}
	indexTemp, err := writeAtomicJSON(manifestDir, IndexFileName, fileManifest)
	if err != nil {
		_ = os.Remove(configTemp)
		return err
	}

	if err := os.Rename(configTemp, filepath.Join(manifestDir, ConfigFileName)); err != nil {
		_ = os.Remove(configTemp)
		_ = os.Remove(indexTemp)
		return fmt.Errorf("index: replace config manifest: %w", err)
	}
	if err := os.Rename(indexTemp, filepath.Join(manifestDir, IndexFileName)); err != nil {
		_ = os.Remove(indexTemp)
		return fmt.Errorf("index: replace index manifest: %w", err)
	}

	return nil
}

func dbPath(rootDir string) string {
	return filepath.Join(rootDir, DirectoryName)
}

func fileManifestFromDomain(files []domain.FileState, generation string) fileManifest {
	states := append([]domain.FileState(nil), files...)
	sortFileStates(states)

	entries := make([]fileManifestEntry, 0, len(states))
	for _, state := range states {
		var modTime *time.Time
		if !state.ModTime.IsZero() {
			value := state.ModTime.UTC()
			modTime = &value
		}
		entries = append(entries, fileManifestEntry{
			Path:       strings.TrimSpace(state.RelPath),
			FileHash:   strings.TrimSpace(state.FileHash),
			ModTime:    modTime,
			RecipeHash: strings.TrimSpace(state.RecipeHash),
			ChunkIDs:   append([]string(nil), state.ChunkIDs...),
			ChunkCount: state.ChunkCount,
			UpdatedAt:  state.LastIndexedAt.UTC(),
		})
	}

	return fileManifest{
		Version:    SchemaVersionV1,
		Generation: generation,
		Files:      entries,
	}
}

func configManifestFromDomain(config domain.StoreConfig, generation string, createdAt time.Time) ConfigManifest {
	rootIdentity := strings.TrimSpace(config.RootIdentity)
	if rootIdentity == "" {
		rootIdentity = jcpaths.NormalizeStoredPath(config.RootDir)
	}

	collectionID := strings.TrimSpace(config.CollectionID)
	if collectionID == "" {
		collectionID = CollectionIDForRoot(rootIdentity)
	}

	return ConfigManifest{
		CollectionID:    collectionID,
		RootIdentity:    rootIdentity,
		FileType:        normalizeFileType(config.FileType),
		Version:         SchemaVersionV1,
		Generation:      generation,
		Provider:        strings.TrimSpace(config.Provider),
		ProviderOptions: safeProviderOptions(config.ProviderOptions),
		Model:           strings.TrimSpace(config.Model),
		Splitter:        strings.TrimSpace(config.Splitter),
		VectorDim:       config.VectorDim,
		DBVersion:       strings.TrimSpace(config.DBVersion),
		CreatedAt:       createdAt,
	}
}

func writeAtomicJSON(dir string, name string, value any) (string, error) {
	file, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("index: create temp file for %s: %w", name, err)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("index: encode %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("index: close temp file for %s: %w", name, err)
	}

	return file.Name(), nil
}

func readJSON(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, target); err != nil {
		return err
	}
	return nil
}

func sortFileStates(states []domain.FileState) {
	sort.Slice(states, func(i, j int) bool {
		return strings.Compare(states[i].RelPath, states[j].RelPath) < 0
	})
}

func (m fileManifest) validate() error {
	if strings.TrimSpace(m.Version) != SchemaVersionV1 {
		return fmt.Errorf("index: unsupported index manifest version %q", m.Version)
	}
	if strings.TrimSpace(m.Generation) == "" {
		return fmt.Errorf("index: index manifest generation is required")
	}

	seen := make(map[string]struct{}, len(m.Files))
	for index, entry := range m.Files {
		if err := entry.validate(); err != nil {
			return fmt.Errorf("index: files[%d]: %w", index, err)
		}
		if _, exists := seen[entry.Path]; exists {
			return fmt.Errorf("index: duplicate path %q", entry.Path)
		}
		seen[entry.Path] = struct{}{}
	}

	return nil
}

func (e fileManifestEntry) validate() error {
	if strings.TrimSpace(e.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if strings.TrimSpace(e.FileHash) == "" {
		return fmt.Errorf("file_hash is required")
	}
	if strings.TrimSpace(e.RecipeHash) == "" {
		return fmt.Errorf("recipe_hash is required")
	}
	if e.ChunkCount < 0 {
		return fmt.Errorf("chunk_count must be >= 0")
	}
	if len(e.ChunkIDs) != e.ChunkCount {
		return fmt.Errorf("chunk_count must match chunk_ids length")
	}
	if e.UpdatedAt.IsZero() {
		return fmt.Errorf("updated_at is required")
	}
	for index, chunkID := range e.ChunkIDs {
		if strings.TrimSpace(chunkID) == "" {
			return fmt.Errorf("chunk_ids[%d] is required", index)
		}
	}
	return nil
}

func (m ConfigManifest) validate() error {
	if strings.TrimSpace(m.Version) != SchemaVersionV1 {
		return fmt.Errorf("index: unsupported config manifest version %q", m.Version)
	}
	if strings.TrimSpace(m.Generation) == "" {
		return fmt.Errorf("index: config manifest generation is required")
	}
	if rootIdentity := strings.TrimSpace(m.RootIdentity); rootIdentity != "" {
		if strings.TrimSpace(m.CollectionID) == "" {
			return fmt.Errorf("index: config collection_id is required when root_identity is present")
		}
		if rootIdentity != jcpaths.NormalizeStoredPath(rootIdentity) {
			return fmt.Errorf("index: config root_identity must be normalized")
		}
	}
	if strings.TrimSpace(m.Provider) == "" {
		return fmt.Errorf("index: config provider is required")
	}
	if strings.TrimSpace(m.Model) == "" {
		return fmt.Errorf("index: config model is required")
	}
	if strings.TrimSpace(m.Splitter) == "" {
		return fmt.Errorf("index: config splitter is required")
	}
	if m.VectorDim < 0 {
		return fmt.Errorf("index: config vector_dim must be >= 0")
	}
	if strings.TrimSpace(m.DBVersion) == "" {
		return fmt.Errorf("index: config db_version is required")
	}
	if m.CreatedAt.IsZero() {
		return fmt.Errorf("index: config created_at is required")
	}
	return nil
}

func CollectionIDForRoot(rootIdentity string) string {
	return CollectionIDForRootAndFileType(rootIdentity, "markdown")
}

func CollectionIDForRootAndFileType(rootIdentity string, fileType string) string {
	normalizedRoot := jcpaths.NormalizeStoredPath(rootIdentity)
	if normalizedRoot == "" {
		return ""
	}
	normalizedFileType := normalizeFileType(fileType)
	if normalizedFileType == "" {
		normalizedFileType = "markdown"
	}
	if normalizedFileType == "markdown" {
		return jcpaths.CollectionIDForRoot(normalizedRoot)
	}
	return jcpaths.CollectionIDForRoot(normalizedRoot + "|file_type=" + normalizedFileType)
}

func collectionDBPath(config domain.StoreConfig) (string, error) {
	if strings.TrimSpace(config.CollectionID) == "" || strings.TrimSpace(config.DataDir) == "" {
		return "", nil
	}
	return filepath.Join(jcpaths.CollectionStorageDir(config.DataDir, config.CollectionID), DirectoryName), nil
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

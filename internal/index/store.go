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
	Path       string    `json:"path"`
	FileHash   string    `json:"file_hash"`
	RecipeHash string    `json:"recipe_hash"`
	ChunkIDs   []string  `json:"chunk_ids"`
	ChunkCount int       `json:"chunk_count"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ConfigManifest struct {
	Version    string    `json:"version"`
	Generation string    `json:"generation"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Splitter   string    `json:"splitter"`
	VectorDim  int       `json:"vector_dim"`
	DBVersion  string    `json:"db_version"`
	CreatedAt  time.Time `json:"created_at"`
}

func Load(rootDir string) (Snapshot, error) {
	configPath := configPath(rootDir)
	indexPath := indexPath(rootDir)

	configExists, err := fileExists(configPath)
	if err != nil {
		return Snapshot{}, err
	}
	indexExists, err := fileExists(indexPath)
	if err != nil {
		return Snapshot{}, err
	}

	switch {
	case !configExists && !indexExists:
		return Snapshot{}, ErrStateNotFound
	case !configExists || !indexExists:
		return Snapshot{}, fmt.Errorf("%w: config/index pair incomplete", ErrRebuildRequired)
	}

	config, err := readConfigManifest(configPath)
	if err != nil {
		return Snapshot{}, err
	}
	manifest, err := readFileManifest(indexPath)
	if err != nil {
		return Snapshot{}, err
	}

	if config.Generation != manifest.Generation {
		return Snapshot{}, fmt.Errorf("%w: config/index generation mismatch", ErrRebuildRequired)
	}

	states := make([]domain.FileState, 0, len(manifest.Files))
	for _, entry := range manifest.Files {
		states = append(states, entry.toDomain())
	}
	sortFileStates(states)

	return Snapshot{
		Config: config.toDomain(rootDir),
		Files:  states,
	}, nil
}

func Save(rootDir string, config domain.StoreConfig, files []domain.FileState) error {
	manifestDir := dbPath(rootDir)
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

func configPath(rootDir string) string {
	return filepath.Join(dbPath(rootDir), ConfigFileName)
}

func indexPath(rootDir string) string {
	return filepath.Join(dbPath(rootDir), IndexFileName)
}

func dbPath(rootDir string) string {
	return filepath.Join(rootDir, DirectoryName)
}

func fileManifestFromDomain(files []domain.FileState, generation string) fileManifest {
	states := append([]domain.FileState(nil), files...)
	sortFileStates(states)

	entries := make([]fileManifestEntry, 0, len(states))
	for _, state := range states {
		entries = append(entries, fileManifestEntry{
			Path:       strings.TrimSpace(state.RelPath),
			FileHash:   strings.TrimSpace(state.FileHash),
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
	return ConfigManifest{
		Version:    SchemaVersionV1,
		Generation: generation,
		Provider:   strings.TrimSpace(config.Provider),
		Model:      strings.TrimSpace(config.Model),
		Splitter:   strings.TrimSpace(config.Splitter),
		VectorDim:  config.VectorDim,
		DBVersion:  strings.TrimSpace(config.DBVersion),
		CreatedAt:  createdAt,
	}
}

func (m ConfigManifest) toDomain(rootDir string) domain.StoreConfig {
	return domain.StoreConfig{
		RootDir:         rootDir,
		Provider:        m.Provider,
		Model:           m.Model,
		Splitter:        m.Splitter,
		VectorDim:       m.VectorDim,
		ManifestVersion: m.Version,
		DBVersion:       m.DBVersion,
		CreatedAt:       m.CreatedAt,
	}
}

func (e fileManifestEntry) toDomain() domain.FileState {
	return domain.FileState{
		RelPath:       e.Path,
		FileHash:      e.FileHash,
		RecipeHash:    e.RecipeHash,
		ChunkIDs:      append([]string(nil), e.ChunkIDs...),
		ChunkCount:    e.ChunkCount,
		LastIndexedAt: e.UpdatedAt,
	}
}

func readConfigManifest(path string) (ConfigManifest, error) {
	var manifest ConfigManifest
	if err := readJSON(path, &manifest); err != nil {
		return ConfigManifest{}, fmt.Errorf("%w: config manifest unreadable: %v", ErrRebuildRequired, err)
	}
	if err := manifest.validate(); err != nil {
		return ConfigManifest{}, fmt.Errorf("%w: %v", ErrRebuildRequired, err)
	}
	return manifest, nil
}

func readFileManifest(path string) (fileManifest, error) {
	var manifest fileManifest
	if err := readJSON(path, &manifest); err != nil {
		return fileManifest{}, fmt.Errorf("%w: index manifest unreadable: %v", ErrRebuildRequired, err)
	}
	if err := manifest.validate(); err != nil {
		return fileManifest{}, fmt.Errorf("%w: %v", ErrRebuildRequired, err)
	}
	return manifest, nil
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

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
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

package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
)

const collectionRegistryFileName = "collections.json"

var ErrCollectionNotFound = errors.New("index: collection not found")

type CollectionEntry struct {
	CollectionID string    `json:"collection_id"`
	RootIdentity string    `json:"root_identity"`
	RootDir      string    `json:"root_dir"`
	FileType     string    `json:"file_type,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CollectionRegistry struct {
	Version     string            `json:"version"`
	Collections []CollectionEntry `json:"collections"`
}

type CollectionMatch struct {
	CollectionEntry
	InputPath   string
	PathPrefix  string
	PathIsDir   bool
	InputRoot   string
	InputTarget string
}

func SaveCollection(dataRoot string, entry CollectionEntry) error {
	registry, err := LoadCollectionRegistry(dataRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		registry = CollectionRegistry{Version: SchemaVersionV1}
	}

	normalized, err := normalizeCollectionEntry(entry)
	if err != nil {
		return err
	}

	replaced := false
	for index := range registry.Collections {
		if registry.Collections[index].RootIdentity == normalized.RootIdentity && normalizeFileType(registry.Collections[index].FileType) == normalizeFileType(normalized.FileType) {
			registry.Collections[index] = normalized
			replaced = true
			break
		}
	}
	if !replaced {
		registry.Collections = append(registry.Collections, normalized)
	}
	sortCollectionEntries(registry.Collections)

	return saveCollectionRegistry(dataRoot, registry)
}

// DeleteCollection removes the entry with collectionID from the global
// collections registry. The collection's on-disk storage directory is NOT
// touched here — the caller is responsible for removing
// `<dataRoot>/collections/<collectionID>` separately. Returns
// ErrCollectionNotFound if no matching entry exists.
func DeleteCollection(dataRoot string, collectionID string) (CollectionEntry, error) {
	collectionID = strings.TrimSpace(collectionID)
	if collectionID == "" {
		return CollectionEntry{}, fmt.Errorf("index: collection_id is required")
	}

	registry, err := LoadCollectionRegistry(dataRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CollectionEntry{}, fmt.Errorf("%w: %s", ErrCollectionNotFound, collectionID)
		}
		return CollectionEntry{}, err
	}

	for i, entry := range registry.Collections {
		if entry.CollectionID != collectionID {
			continue
		}
		registry.Collections = append(registry.Collections[:i], registry.Collections[i+1:]...)
		if err := saveCollectionRegistry(dataRoot, registry); err != nil {
			return CollectionEntry{}, err
		}
		return entry, nil
	}

	return CollectionEntry{}, fmt.Errorf("%w: %s", ErrCollectionNotFound, collectionID)
}

func LoadCollectionRegistry(dataRoot string) (CollectionRegistry, error) {
	path := collectionRegistryPath(dataRoot)
	var registry CollectionRegistry
	if err := readJSON(path, &registry); err != nil {
		return CollectionRegistry{}, err
	}
	if err := registry.validate(); err != nil {
		return CollectionRegistry{}, err
	}
	sortCollectionEntries(registry.Collections)
	return registry, nil
}

func ResolveCollection(dataRoot string, inputPath string) (CollectionMatch, error) {
	return ResolveCollectionForFileType(dataRoot, inputPath, "")
}

func ResolveCollectionForFileType(dataRoot string, inputPath string, fileType string) (CollectionMatch, error) {
	resolved, err := jcpaths.ResolveCollectionRoot(inputPath)
	if err != nil {
		return CollectionMatch{}, err
	}

	registry, err := LoadCollectionRegistry(dataRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CollectionMatch{}, fmt.Errorf("%w: %s", ErrCollectionNotFound, resolved.InputPath)
		}
		return CollectionMatch{}, err
	}

	searchIdentity := resolved.Identity
	if !resolved.IsDir {
		searchIdentity = jcpaths.NormalizeStoredPath(resolved.InputPath)
	}

	var best *CollectionEntry
	for index := range registry.Collections {
		candidate := &registry.Collections[index]
		if normalizedFileType := normalizeFileType(fileType); normalizedFileType != "" && normalizeFileType(candidate.FileType) != normalizedFileType {
			continue
		}
		if !hasRootPrefix(searchIdentity, candidate.RootIdentity) {
			continue
		}
		if best == nil || len(candidate.RootIdentity) > len(best.RootIdentity) {
			best = candidate
		}
	}
	if best == nil {
		return CollectionMatch{}, fmt.Errorf("%w: %s", ErrCollectionNotFound, resolved.InputPath)
	}

	prefix, err := filepath.Rel(best.RootDir, resolved.InputPath)
	if err != nil {
		return CollectionMatch{}, fmt.Errorf("index: compute query path prefix: %w", err)
	}

	return CollectionMatch{
		CollectionEntry: *best,
		InputPath:       resolved.InputPath,
		PathPrefix:      jcpaths.NormalizeStoredPath(prefix),
		PathIsDir:       resolved.IsDir,
		InputRoot:       resolved.RootDir,
		InputTarget:     searchIdentity,
	}, nil
}

func collectionRegistryPath(dataRoot string) string {
	return filepath.Join(dataRoot, collectionRegistryFileName)
}

func saveCollectionRegistry(dataRoot string, registry CollectionRegistry) error {
	if strings.TrimSpace(dataRoot) == "" {
		return fmt.Errorf("index: data root is required")
	}
	registry.Version = SchemaVersionV1
	if err := registry.validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return fmt.Errorf("index: create data root: %w", err)
	}

	tempPath, err := writeAtomicJSON(dataRoot, collectionRegistryFileName, registry)
	if err != nil {
		return err
	}
	finalPath := collectionRegistryPath(dataRoot)
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("index: replace collection registry: %w", err)
	}
	return nil
}

func normalizeCollectionEntry(entry CollectionEntry) (CollectionEntry, error) {
	rootDirInput := strings.TrimSpace(entry.RootDir)
	if rootDirInput == "" {
		return CollectionEntry{}, fmt.Errorf("index: collection root_dir is required")
	}

	resolved, err := jcpaths.ResolveCollectionRoot(rootDirInput)
	if err != nil {
		return CollectionEntry{}, fmt.Errorf("index: resolve collection root: %w", err)
	}
	if !resolved.IsDir {
		return CollectionEntry{}, fmt.Errorf("index: collection root_dir must be a directory")
	}

	rootDir := resolved.RootDir
	rootIdentity := jcpaths.NormalizeStoredPath(entry.RootIdentity)
	if rootIdentity == "" {
		rootIdentity = resolved.Identity
	}
	if rootIdentity == "" {
		return CollectionEntry{}, fmt.Errorf("index: collection root_identity is required")
	}

	collectionID := strings.TrimSpace(entry.CollectionID)
	if collectionID == "" {
		collectionID = CollectionIDForRootAndFileType(rootIdentity, entry.FileType)
	}
	if collectionID == "" {
		return CollectionEntry{}, fmt.Errorf("index: collection_id is required")
	}

	updatedAt := entry.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	return CollectionEntry{
		CollectionID: collectionID,
		RootIdentity: rootIdentity,
		RootDir:      rootDir,
		FileType:     normalizeFileType(entry.FileType),
		UpdatedAt:    updatedAt,
	}, nil
}

func (r CollectionRegistry) validate() error {
	if strings.TrimSpace(r.Version) != SchemaVersionV1 {
		return fmt.Errorf("index: unsupported collection registry version %q", r.Version)
	}

	seen := make(map[string]struct{}, len(r.Collections))
	for index, entry := range r.Collections {
		if strings.TrimSpace(entry.CollectionID) == "" {
			return fmt.Errorf("index: collections[%d]: collection_id is required", index)
		}
		rootIdentity := jcpaths.NormalizeStoredPath(entry.RootIdentity)
		if rootIdentity == "" {
			return fmt.Errorf("index: collections[%d]: root_identity is required", index)
		}
		if rootIdentity != entry.RootIdentity {
			return fmt.Errorf("index: collections[%d] root_identity must be normalized", index)
		}
		if entry.CollectionID != CollectionIDForRootAndFileType(rootIdentity, entry.FileType) {
			return fmt.Errorf("index: collections[%d]: collection_id does not match root_identity", index)
		}
		if strings.TrimSpace(entry.RootDir) == "" {
			return fmt.Errorf("index: collections[%d]: root_dir is required", index)
		}
		seenKey := entry.RootIdentity + "|" + normalizeFileType(entry.FileType)
		if _, exists := seen[seenKey]; exists {
			return fmt.Errorf("index: duplicate collection root_identity/file_type %q/%q", entry.RootIdentity, entry.FileType)
		}
		seen[seenKey] = struct{}{}
	}

	return nil
}

func normalizeFileType(fileType string) string {
	trimmed := strings.TrimSpace(fileType)
	if trimmed == "" {
		return "markdown"
	}
	if trimmed == "md" {
		return "markdown"
	}
	return trimmed
}

func sortCollectionEntries(entries []CollectionEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if len(entries[i].RootIdentity) != len(entries[j].RootIdentity) {
			return len(entries[i].RootIdentity) > len(entries[j].RootIdentity)
		}
		return strings.Compare(entries[i].RootIdentity, entries[j].RootIdentity) < 0
	})
}

func hasRootPrefix(target string, root string) bool {
	target = jcpaths.NormalizeStoredPath(target)
	root = jcpaths.NormalizeStoredPath(root)
	if target == "" || root == "" {
		return false
	}
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+"/")
}

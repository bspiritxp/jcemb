package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bspiritxp/jcemb/internal/config"
	"github.com/bspiritxp/jcemb/internal/index"
	jcpaths "github.com/bspiritxp/jcemb/internal/paths"
)

var (
	ErrCollectionAmbiguousID = errors.New("collection: ambiguous id prefix")
	ErrCollectionDeleteAborted = errors.New("collection: deletion aborted by user")
)

type CollectionInfo struct {
	CollectionID string
	RootDir      string
	RootIdentity string
	FileType     string
	Provider     string
	Model        string
	VectorDim    int
	FileCount    int
	UpdatedAt    time.Time
	CreatedAt    time.Time
	LoadError    error
}

type CollectionListRequest struct {
	DataDir         string
	LoadRegistry    func(dataRoot string) (index.CollectionRegistry, error)
	LoadSnapshot    func(storageRoot string) (index.Snapshot, error)
}

type CollectionListResult struct {
	DataDir     string
	Collections []CollectionInfo
}

type CollectionDeleteRequest struct {
	DataDir       string
	IDOrPrefix    string
	AssumeYes     bool
	In            io.Reader
	Out           io.Writer
	LoadRegistry  func(dataRoot string) (index.CollectionRegistry, error)
	DeleteEntry   func(dataRoot string, collectionID string) (index.CollectionEntry, error)
	RemoveAll     func(path string) error
	Confirm       func(reader *bufio.Reader, writer io.Writer, label string) (bool, error)
}

type CollectionDeleteResult struct {
	Deleted     index.CollectionEntry
	StorageRoot string
}

func RunCollectionList(request CollectionListRequest) (CollectionListResult, error) {
	if request.LoadRegistry == nil {
		request.LoadRegistry = index.LoadCollectionRegistry
	}
	if request.LoadSnapshot == nil {
		request.LoadSnapshot = index.Load
	}
	dataDir, err := resolveDataDir(request.DataDir)
	if err != nil {
		return CollectionListResult{}, err
	}

	registry, err := request.LoadRegistry(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CollectionListResult{DataDir: dataDir}, nil
		}
		return CollectionListResult{}, err
	}

	infos := make([]CollectionInfo, 0, len(registry.Collections))
	for _, entry := range registry.Collections {
		info := CollectionInfo{
			CollectionID: entry.CollectionID,
			RootDir:      entry.RootDir,
			RootIdentity: entry.RootIdentity,
			FileType:     entry.FileType,
			UpdatedAt:    entry.UpdatedAt,
		}
		storageRoot := jcpaths.CollectionStorageDir(dataDir, entry.CollectionID)
		snapshot, snapErr := request.LoadSnapshot(storageRoot)
		if snapErr != nil {
			info.LoadError = snapErr
		} else {
			info.Provider = snapshot.Config.Provider
			info.Model = snapshot.Config.Model
			info.VectorDim = snapshot.Config.VectorDim
			info.CreatedAt = snapshot.Config.CreatedAt
			info.FileCount = len(snapshot.Files)
		}
		infos = append(infos, info)
	}

	return CollectionListResult{DataDir: dataDir, Collections: infos}, nil
}

func RunCollectionDelete(request CollectionDeleteRequest) (CollectionDeleteResult, error) {
	if request.LoadRegistry == nil {
		request.LoadRegistry = index.LoadCollectionRegistry
	}
	if request.DeleteEntry == nil {
		request.DeleteEntry = index.DeleteCollection
	}
	if request.RemoveAll == nil {
		request.RemoveAll = os.RemoveAll
	}
	if request.Confirm == nil {
		request.Confirm = promptConfirm
	}
	if request.Out == nil {
		request.Out = io.Discard
	}
	if request.In == nil {
		request.In = os.Stdin
	}

	dataDir, err := resolveDataDir(request.DataDir)
	if err != nil {
		return CollectionDeleteResult{}, err
	}

	prefix := strings.TrimSpace(request.IDOrPrefix)
	if prefix == "" {
		return CollectionDeleteResult{}, fmt.Errorf("collection: id or prefix is required")
	}

	registry, err := request.LoadRegistry(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CollectionDeleteResult{}, fmt.Errorf("%w: %s", index.ErrCollectionNotFound, prefix)
		}
		return CollectionDeleteResult{}, err
	}

	matched, err := matchCollectionByPrefix(registry.Collections, prefix)
	if err != nil {
		return CollectionDeleteResult{}, err
	}

	storageRoot := jcpaths.CollectionStorageDir(dataDir, matched.CollectionID)

	if !request.AssumeYes {
		_, _ = fmt.Fprintf(request.Out, "About to delete collection:\n")
		_, _ = fmt.Fprintf(request.Out, "  id        : %s\n", matched.CollectionID)
		_, _ = fmt.Fprintf(request.Out, "  root_dir  : %s\n", matched.RootDir)
		_, _ = fmt.Fprintf(request.Out, "  file_type : %s\n", matched.FileType)
		_, _ = fmt.Fprintf(request.Out, "  storage   : %s\n", storageRoot)

		reader := bufio.NewReader(request.In)
		ok, confirmErr := request.Confirm(reader, request.Out, "Proceed with deletion?")
		if confirmErr != nil {
			return CollectionDeleteResult{}, confirmErr
		}
		if !ok {
			return CollectionDeleteResult{}, ErrCollectionDeleteAborted
		}
	}

	deleted, err := request.DeleteEntry(dataDir, matched.CollectionID)
	if err != nil {
		return CollectionDeleteResult{}, err
	}

	if err := request.RemoveAll(storageRoot); err != nil {
		return CollectionDeleteResult{Deleted: deleted, StorageRoot: storageRoot}, fmt.Errorf("collection: remove storage %s: %w", storageRoot, err)
	}

	return CollectionDeleteResult{Deleted: deleted, StorageRoot: storageRoot}, nil
}

func matchCollectionByPrefix(entries []index.CollectionEntry, prefix string) (index.CollectionEntry, error) {
	prefix = strings.TrimSpace(prefix)
	var matches []index.CollectionEntry
	for _, entry := range entries {
		if entry.CollectionID == prefix {
			return entry, nil
		}
		if strings.HasPrefix(entry.CollectionID, prefix) {
			matches = append(matches, entry)
		}
	}

	switch len(matches) {
	case 0:
		return index.CollectionEntry{}, fmt.Errorf("%w: %s", index.ErrCollectionNotFound, prefix)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.CollectionID)
		}
		return index.CollectionEntry{}, fmt.Errorf("%w: %q matches %d collections: %s", ErrCollectionAmbiguousID, prefix, len(matches), strings.Join(ids, ", "))
	}
}

func resolveDataDir(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = config.DefaultSettings().DataDir
	}
	expanded, err := jcpaths.ExpandUserHome(trimmed)
	if err != nil {
		return "", fmt.Errorf("collection: data dir: %w", err)
	}
	return filepath.Clean(expanded), nil
}

func promptConfirm(reader *bufio.Reader, writer io.Writer, label string) (bool, error) {
	for {
		_, _ = fmt.Fprintf(writer, "%s [y/N]: ", label)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "":
			return false, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		if err == io.EOF {
			return false, nil
		}
		_, _ = fmt.Fprintln(writer, "  Please answer 'y' or 'n'.")
	}
}

package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	internalfs "github.com/bspiritxp/jcemb/internal/fs"
	"github.com/stretchr/testify/require"
)

func TestLoadFileParsesFrontMatterAndBuildsDomainReadyMetadata(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "docs", "guide.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))

	raw := []byte("---\ntitle: Getting Started\ntags: [ Go , vector, go ]\nowner:\n  team: docs\n---\n\n# Hello\n")
	require.NoError(t, os.WriteFile(filePath, raw, 0o644))

	document, err := LoadFile(internalfs.File{
		RootDir:  filepath.ToSlash(filepath.Clean(root)),
		FilePath: filepath.ToSlash(filepath.Clean(filePath)),
		RelPath:  "docs/guide.md",
		FileName: "guide.md",
		DocType:  "md",
	})
	require.NoError(t, err)

	sum := sha256.Sum256(raw)
	require.Equal(t, "# Hello\n", document.Content)
	require.Equal(t, DocumentMetadata{
		Source:   "docs/guide.md",
		FilePath: filepath.ToSlash(filepath.Clean(filePath)),
		RelPath:  "docs/guide.md",
		FileName: "guide.md",
		DocType:  "md",
		FileHash: hex.EncodeToString(sum[:]),
		Title:    "Getting Started",
		Tags:     []string{"go", "vector"},
		YAML: map[string]any{
			"title": "Getting Started",
			"tags":  []any{"Go", "vector", "go"},
			"owner": map[string]any{"team": "docs"},
		},
	}, document.Metadata)

	domainDocument := document.Metadata.DomainDocument(document.Content)
	require.Equal(t, domain.Document{
		Source:   "docs/guide.md",
		FilePath: filepath.ToSlash(filepath.Clean(filePath)),
		RelPath:  "docs/guide.md",
		FileName: "guide.md",
		DocType:  "md",
		FileHash: hex.EncodeToString(sum[:]),
		Title:    "Getting Started",
		Content:  "# Hello\n",
		Tags:     []string{"go", "vector"},
		YAML: map[string]any{
			"title": "Getting Started",
			"tags":  []any{"Go", "vector", "go"},
			"owner": map[string]any{"team": "docs"},
		},
	}, domainDocument)

	chunkMetadata, err := document.Metadata.ChunkMetadata(0, []string{"Getting Started"})
	require.NoError(t, err)
		require.Equal(t, domain.ChunkMetadata{
			Source:     "docs/guide.md",
			FilePath:   filepath.ToSlash(filepath.Clean(filePath)),
			RelPath:    "docs/guide.md",
			FileName:   "guide.md",
			DocType:    "md",
			FileHash:   hex.EncodeToString(sum[:]),
			Title:      "Getting Started",
			ChunkIndex: 0,
			TitlePath:  []string{"Getting Started"},
			Tags:       []string{"go", "vector"},
		YAML: map[string]any{
			"title": "Getting Started",
			"tags":  []any{"Go", "vector", "go"},
			"owner": map[string]any{"team": "docs"},
		},
	}, chunkMetadata)

	chunkMap := chunkMetadata.AsMap()
	require.Equal(t, "docs/guide.md", chunkMap[domain.MetadataSourcePathKey])
	require.Equal(t, filepath.ToSlash(filepath.Clean(filePath)), chunkMap[domain.MetadataSourceFileKey])
	require.Equal(t, hex.EncodeToString(sum[:]), chunkMap[domain.MetadataDocHashKey])
	require.Equal(t, "Getting Started", chunkMap[domain.MetadataTitleKey])
	require.Equal(t, map[string]any{
		"title": "Getting Started",
		"tags":  []any{"Go", "vector", "go"},
		"owner": map[string]any{"team": "docs"},
	}, chunkMap[domain.MetadataYAMLKey])
}

func TestLoadFileRejectsInvalidYAMLFrontMatter(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "broken.md")
	require.NoError(t, os.WriteFile(filePath, []byte("---\ntags: [broken\n---\nbody\n"), 0o644))

	_, err := LoadFile(internalfs.File{
		RootDir:  filepath.ToSlash(filepath.Clean(root)),
		FilePath: filepath.ToSlash(filepath.Clean(filePath)),
		RelPath:  "broken.md",
		FileName: "broken.md",
		DocType:  "md",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid yaml front matter")
}

func TestLoadFileHandlesEmptyFileAndFrontMatterOnlyDocuments(t *testing.T) {
	root := t.TempDir()

	emptyPath := filepath.Join(root, "empty.md")
	require.NoError(t, os.WriteFile(emptyPath, nil, 0o644))
	emptyDocument, err := LoadFile(internalfs.File{
		RootDir:  filepath.ToSlash(filepath.Clean(root)),
		FilePath: filepath.ToSlash(filepath.Clean(emptyPath)),
		RelPath:  "empty.md",
		FileName: "empty.md",
		DocType:  "md",
	})
	require.NoError(t, err)
	require.Empty(t, emptyDocument.Content)
	require.Empty(t, emptyDocument.Metadata.Title)
	require.Empty(t, emptyDocument.Metadata.Tags)
	require.Equal(t, map[string]any{}, emptyDocument.Metadata.YAML)

	frontMatterOnlyPath := filepath.Join(root, "frontmatter-only.md")
	require.NoError(t, os.WriteFile(frontMatterOnlyPath, []byte("---\ntitle: Only Metadata\ntags:\n  - alpha\n---\n"), 0o644))
	frontMatterOnlyDocument, err := LoadFile(internalfs.File{
		RootDir:  filepath.ToSlash(filepath.Clean(root)),
		FilePath: filepath.ToSlash(filepath.Clean(frontMatterOnlyPath)),
		RelPath:  "frontmatter-only.md",
		FileName: "frontmatter-only.md",
		DocType:  "md",
	})
	require.NoError(t, err)
	require.Empty(t, frontMatterOnlyDocument.Content)
	require.Equal(t, "Only Metadata", frontMatterOnlyDocument.Metadata.Title)
	require.Equal(t, []string{"alpha"}, frontMatterOnlyDocument.Metadata.Tags)
	require.Equal(t, map[string]any{
		"title": "Only Metadata",
		"tags":  []any{"alpha"},
	}, frontMatterOnlyDocument.Metadata.YAML)
}

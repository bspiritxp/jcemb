package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChunkMetadataNormalizesTagsAndPreservesContract(t *testing.T) {
	metadata, err := NewChunkMetadata(map[string]any{
		MetadataSourceKey:     "docs/guide.md",
		MetadataFilePathKey:   "/repo/docs/guide.md",
		MetadataRelPathKey:    "docs/guide.md",
		MetadataFileNameKey:   "guide.md",
		MetadataDocTypeKey:    "md",
		MetadataFileHashKey:   "abc123",
		MetadataTitleKey:      "Guide",
		MetadataChunkIndexKey: 2,
		MetadataTitlePathKey:  []any{" Intro ", "Usage", ""},
		MetadataTagsKey:       []any{" Go ", "vector", "go", ""},
		MetadataYAMLKey: map[string]any{
			"title": "Guide",
			"tags":  []string{"Go", "vector"},
		},
	})

	require.NoError(t, err)
	require.Equal(t, "Guide", metadata.Title)
	require.Equal(t, []string{"Intro", "Usage"}, metadata.TitlePath)
	require.Equal(t, []string{"go", "vector"}, metadata.Tags)
	require.Equal(t, map[string]any{
		MetadataSourceKey:     "docs/guide.md",
		MetadataSourcePathKey: "docs/guide.md",
		MetadataSourceFileKey: "/repo/docs/guide.md",
		MetadataFilePathKey:   "/repo/docs/guide.md",
		MetadataRelPathKey:    "docs/guide.md",
		MetadataFileNameKey:   "guide.md",
		MetadataDocTypeKey:    "md",
		MetadataFileHashKey:   "abc123",
		MetadataDocHashKey:    "abc123",
		MetadataTitleKey:      "Guide",
		MetadataChunkIndexKey: 2,
		MetadataTitlePathKey:  []string{"Intro", "Usage"},
		MetadataTagsKey:       []string{"go", "vector"},
		MetadataYAMLKey: map[string]any{
			"title": "Guide",
			"tags":  []string{"Go", "vector"},
		},
	}, metadata.AsMap())
}

func TestChunkMetadataAcceptsRequestedAliasKeys(t *testing.T) {
	metadata, err := NewChunkMetadata(map[string]any{
		MetadataSourcePathKey: "docs/guide.md",
		MetadataSourceFileKey: "/repo/docs/guide.md",
		MetadataRelPathKey:    "docs/guide.md",
		MetadataFileNameKey:   "guide.md",
		MetadataDocTypeKey:    "md",
		MetadataDocHashKey:    "abc123",
		MetadataTitleKey:      "Guide",
		MetadataChunkIndexKey: 0,
		MetadataYAMLKey:       map[string]any{},
	})

	require.NoError(t, err)
	require.Equal(t, "docs/guide.md", metadata.Source)
	require.Equal(t, "/repo/docs/guide.md", metadata.FilePath)
	require.Equal(t, "abc123", metadata.FileHash)
	require.Equal(t, "Guide", metadata.Title)
}

func TestChunkMetadataRejectsInvalidMetadata(t *testing.T) {
	tests := map[string]map[string]any{
		"missing source": {
			MetadataFilePathKey:   "/repo/docs/guide.md",
			MetadataRelPathKey:    "docs/guide.md",
			MetadataFileNameKey:   "guide.md",
			MetadataDocTypeKey:    "md",
			MetadataFileHashKey:   "abc123",
			MetadataChunkIndexKey: 0,
		},
		"invalid tags type": {
			MetadataSourceKey:     "docs/guide.md",
			MetadataFilePathKey:   "/repo/docs/guide.md",
			MetadataRelPathKey:    "docs/guide.md",
			MetadataFileNameKey:   "guide.md",
			MetadataDocTypeKey:    "md",
			MetadataFileHashKey:   "abc123",
			MetadataChunkIndexKey: 0,
			MetadataTagsKey:       "go",
		},
		"negative chunk index": {
			MetadataSourceKey:     "docs/guide.md",
			MetadataFilePathKey:   "/repo/docs/guide.md",
			MetadataRelPathKey:    "docs/guide.md",
			MetadataFileNameKey:   "guide.md",
			MetadataDocTypeKey:    "md",
			MetadataFileHashKey:   "abc123",
			MetadataChunkIndexKey: -1,
		},
		"invalid yaml type": {
			MetadataSourceKey:     "docs/guide.md",
			MetadataFilePathKey:   "/repo/docs/guide.md",
			MetadataRelPathKey:    "docs/guide.md",
			MetadataFileNameKey:   "guide.md",
			MetadataDocTypeKey:    "md",
			MetadataFileHashKey:   "abc123",
			MetadataChunkIndexKey: 0,
			MetadataYAMLKey:       []string{"bad"},
		},
	}

	for name, values := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewChunkMetadata(values)
			require.Error(t, err)
		})
	}
}

func TestChunkMetadataValidateRejectsNonNormalizedTags(t *testing.T) {
	metadata := ChunkMetadata{
		Source:     "docs/guide.md",
		FilePath:   "/repo/docs/guide.md",
		RelPath:    "docs/guide.md",
		FileName:   "guide.md",
		DocType:    "md",
		FileHash:   "abc123",
		ChunkIndex: 0,
		Tags:       []string{"Go", "go"},
		YAML:       map[string]any{},
	}

	err := metadata.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), MetadataTagsKey)
}

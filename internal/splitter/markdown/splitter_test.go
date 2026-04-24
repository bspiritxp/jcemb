package markdown

import (
	"context"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSplitStructuredMarkdownByHeadingHierarchy(t *testing.T) {
	t.Parallel()

	splitter := newTestSplitter(t, map[string]string{"max_chunk_chars": "200"})
	document := testDocument(`# Intro

Intro paragraph.

## Install

- Step one
- Step two

> Important note

` + "```bash\n" + `go test ./...
` + "```" + `

## Usage

Run the command.
`)

	chunks, err := splitter.Split(context.Background(), document)
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	require.Equal(t, 0, chunks[0].Metadata.ChunkIndex)
	require.Equal(t, []string{"Intro"}, chunks[0].Metadata.TitlePath)
	require.Contains(t, chunks[0].Content, "Intro")
	require.Contains(t, chunks[0].Content, "Intro paragraph.")

	require.Equal(t, 1, chunks[1].Metadata.ChunkIndex)
	require.Equal(t, []string{"Intro", "Install"}, chunks[1].Metadata.TitlePath)
	require.Contains(t, chunks[1].Content, "- Step one")
	require.Contains(t, chunks[1].Content, "> Important note")
	require.Contains(t, chunks[1].Content, "```bash")
	require.Contains(t, chunks[1].Content, "go test ./...")

	require.Equal(t, 2, chunks[2].Metadata.ChunkIndex)
	require.Equal(t, []string{"Intro", "Usage"}, chunks[2].Metadata.TitlePath)
	require.Contains(t, chunks[2].Content, "Run the command.")

	for _, chunk := range chunks {
		require.Equal(t, document.RelPath, chunk.Metadata.RelPath)
		require.Equal(t, document.Source, chunk.Metadata.Source)
		require.Equal(t, []string{"docs", "vector"}, chunk.Metadata.Tags)
		require.NotEmpty(t, chunk.ID)
		require.NotEmpty(t, chunk.SectionFingerprint)
	}
}

func TestSplitHandlesDocumentsWithoutHeadings(t *testing.T) {
	t.Parallel()

	splitter := newTestSplitter(t, map[string]string{"max_chunk_chars": "200"})
	document := testDocument("First paragraph.\n\nSecond paragraph with more detail.")
	document.YAML["title"] = "Front Matter Title"

	chunks, err := splitter.Split(context.Background(), document)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, []string{"Front Matter Title"}, chunks[0].Metadata.TitlePath)
	require.Contains(t, chunks[0].Content, "Front Matter Title")
	require.Contains(t, chunks[0].Content, "First paragraph.")
	require.Equal(t, 0, chunks[0].Metadata.ChunkIndex)
}

func TestSplitOversizedSectionPreservesTitlePathAndStableIDs(t *testing.T) {
	t.Parallel()

	splitter := newTestSplitter(t, map[string]string{
		"max_chunk_chars":     "90",
		"chunk_overlap_chars": "10",
	})
	longBody := "This section contains a deliberately long paragraph that should be broken into multiple deterministic chunks while keeping the same title context for every emitted chunk."
	document := testDocument("# Guide\n\n## Deep Dive\n\n" + longBody)

	first, err := splitter.Split(context.Background(), document)
	require.NoError(t, err)
	require.Greater(t, len(first), 1)

	second, err := splitter.Split(context.Background(), document)
	require.NoError(t, err)
	require.Len(t, second, len(first))

	for index := range first {
		require.Equal(t, index, first[index].Metadata.ChunkIndex)
		require.Equal(t, []string{"Guide", "Deep Dive"}, first[index].Metadata.TitlePath)
		require.Equal(t, first[index].ID, second[index].ID)
		require.Equal(t, first[index].Content, second[index].Content)
		require.Equal(t, first[index].SectionFingerprint, second[index].SectionFingerprint)
		require.LessOrEqual(t, len([]rune(first[index].Content)), 90)
	}
}

func TestSplitUsesStableOrderForPreambleAndSubsections(t *testing.T) {
	t.Parallel()

	splitter := newTestSplitter(t, nil)
	document := testDocument("Preamble text.\n\n# First\n\nAlpha\n\n## Second\n\nBeta")

	chunks, err := splitter.Split(context.Background(), document)
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	require.Empty(t, chunks[0].Metadata.TitlePath)
	require.Equal(t, []string{"First"}, chunks[1].Metadata.TitlePath)
	require.Equal(t, []string{"First", "Second"}, chunks[2].Metadata.TitlePath)
	require.Equal(t, []int{0, 1, 2}, []int{chunks[0].Metadata.ChunkIndex, chunks[1].Metadata.ChunkIndex, chunks[2].Metadata.ChunkIndex})
}

func newTestSplitter(t *testing.T, options map[string]string) *Splitter {
	t.Helper()

	splitter, err := New(domain.SplitterSpec{
		Name:    Name,
		Version: DefaultVersion,
		Options: options,
	})
	require.NoError(t, err)
	return splitter
}

func testDocument(content string) domain.Document {
	return domain.Document{
		Source:   "docs/guide.md",
		FilePath: "/tmp/docs/guide.md",
		RelPath:  "docs/guide.md",
		FileName: "guide.md",
		DocType:  "md",
		FileHash: "abc123",
		Content:  content,
		Tags:     []string{"docs", "vector"},
		YAML:     map[string]any{},
	}
}

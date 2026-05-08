package output

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	queryapp "github.com/bspiritxp/jcemb/internal/app/query"
	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func stripANSI(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(s, "")
}

func TestRenderQueryTextIncludesStableFields(t *testing.T) {
	t.Parallel()

	result := sampleQueryResult()
	var buffer bytes.Buffer

	require.NoError(t, RenderQueryText(&buffer, result))
	output := stripANSI(buffer.String())
	require.Contains(t, output, "0.990")
	require.Contains(t, output, "docs/guide.md")
	require.Contains(t, output, "Guide / Intro")
	require.Contains(t, output, "image, search")
	require.Contains(t, output, "Hello world with extra whitespace.")
	require.Contains(t, output, "Tags (AND): go, vector")
}

func TestRenderQueryJSONUsesVersionedSchemaV1(t *testing.T) {
	t.Parallel()

	result := sampleQueryResult()
	var buffer bytes.Buffer

	require.NoError(t, RenderQueryJSON(&buffer, result))

	var payload QueryJSONEnvelope
	require.NoError(t, json.Unmarshal(buffer.Bytes(), &payload))
	require.Equal(t, QuerySchemaVersionV1, payload.Version)
	require.Equal(t, "hello vector", payload.Query)
	require.Equal(t, "/tmp/project", payload.RootPath)
	require.Equal(t, "ollama", payload.Provider)
	require.Equal(t, "bge-m3", payload.Model)
	require.Equal(t, 1024, payload.VectorDim)
	require.Equal(t, []string{"go", "vector"}, payload.Tags)
	require.Len(t, payload.Results, 1)
	require.Equal(t, 1, payload.Results[0].Rank)
	require.Equal(t, 0.99, payload.Results[0].Score)
	require.Equal(t, "docs/guide.md", payload.Results[0].RelPath)
	require.Equal(t, []string{"Guide", "Intro"}, payload.Results[0].TitlePath)
	require.Equal(t, []string{"image", "search"}, payload.Results[0].Tags)
	require.Equal(t, "chunk-1", payload.Results[0].ChunkID)
	require.Equal(t, "Hello world with extra whitespace.", payload.Results[0].Preview)
}

func TestRenderQueryTSVUsesStableFields(t *testing.T) {
	t.Parallel()

	result := sampleQueryResult()
	var buffer bytes.Buffer

	require.NoError(t, RenderQueryTSV(&buffer, result))
	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	require.Equal(t, "rank\tscore\trel_path\ttitle_path\ttags\tchunk_id\tpreview", lines[0])
	require.Equal(t, "1\t0.990000\tdocs/guide.md\tGuide / Intro\timage,search\tchunk-1\tHello world with extra whitespace.", lines[1])
}

func TestRenderQueryTableUsesBorderedHumanReadableRows(t *testing.T) {
	t.Parallel()

	result := sampleQueryResult()
	result.Results[0].Chunk.ID = "1234567890abcdef"
	result.Results[0].Chunk.Metadata.RelPath = "/very/long/path/to/docs/guide.md"
	result.Results[0].Chunk.Content = "Hello\n\n world   with   extra whitespace and enough extra text to make the preview table cell truncate."
	var buffer bytes.Buffer

	require.NoError(t, RenderQueryTable(&buffer, result))
	output := buffer.String()
	require.Contains(t, output, "┌")
	require.Contains(t, output, "├")
	require.Contains(t, output, "└")
	require.Contains(t, output, "│ Rank │ Score │ Path")
	require.NotContains(t, output, "1234567890")
	require.Contains(t, output, "Hello world with extra")
	require.Contains(t, output, "whitespace and enough")
	require.NotContains(t, output, "\n\n world")

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		require.LessOrEqual(t, visibleWidth(line), 96, line)
	}
}

func TestRenderQueryTSVZUsesNULRecords(t *testing.T) {
	t.Parallel()

	result := sampleQueryResult()
	var buffer bytes.Buffer

	require.NoError(t, RenderQueryTSVZ(&buffer, result))
	require.Equal(t, "1\t0.990000\tdocs/guide.md\tGuide / Intro\timage,search\tchunk-1\tHello world with extra whitespace.\x00", buffer.String())
}

func sampleQueryResult() queryapp.Result {
	return queryapp.Result{
		Query:   "hello vector",
		Tags:    []string{"go", "vector"},
		Limit:   10,
		RootDir: "/tmp/project",
		Manifest: domain.StoreConfig{
			Provider:  "ollama",
			Model:     "bge-m3",
			VectorDim: 1024,
		},
		Results: []domain.SearchResult{{
			Rank:  1,
			Score: 0.99,
			Chunk: domain.Chunk{
				ID:      "chunk-1",
				Content: "Hello\n\n world   with   extra whitespace.",
				Metadata: domain.ChunkMetadata{
					RelPath:   "docs/guide.md",
					TitlePath: []string{"Guide", "Intro"},
					Tags:      []string{"image", "search"},
				},
			},
		}},
	}
}

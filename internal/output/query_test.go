package output

import (
	"bytes"
	"encoding/json"
	"regexp"
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

package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	openaiprovider "github.com/bspiritxp/jcemb/internal/provider/openai"
	"github.com/bspiritxp/jcemb/internal/registry"
	"github.com/stretchr/testify/require"
)

func TestExtractSuccessAndRegistryRegistration(t *testing.T) {
	t.Parallel()

	longContent := strings.Repeat("x", 4100) + "TAIL"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "gpt-test", payload["model"])

		format := payload["text"].(map[string]any)["format"].(map[string]any)
		require.Equal(t, "json_schema", format["type"])
		require.Equal(t, "tags", format["name"])
		require.Equal(t, true, format["strict"])
		require.NotContains(t, format, "json_schema", "schema fields must be flat under text.format, not nested in json_schema")
		schema := format["schema"].(map[string]any)
		require.Equal(t, false, schema["additionalProperties"])

		input := payload["input"].([]any)
		content := input[0].(map[string]any)["content"].([]any)
		prompt := content[0].(map[string]any)["text"].(string)
		require.Contains(t, prompt, `Document title: "Test Doc"`)
		require.NotContains(t, prompt, "TAIL")

		_, _ = w.Write([]byte(`{"output_text":"{\"tags\":[\"Go\",\" cli \",\"go\",\"123\",\"https://example.com\"]}"}`))
	}))
	defer server.Close()

	factory, err := registry.GetTagExtractor(Name)
	require.NoError(t, err)

	extractor, err := factory(domain.TagExtractorConfig{
		Model: "gpt-test",
		Options: map[string]string{
			openaiprovider.OptionBaseURL: server.URL + "/v1/",
			openaiprovider.OptionAPIKey:  "test-key",
		},
		MaxTags:   8,
		MinTagLen: 2,
		MaxTagLen: 32,
	})
	require.NoError(t, err)

	result, err := extractor.Extract(context.Background(), domain.TagExtractRequest{
		Document: domain.Document{Title: "Test Doc", Content: longContent},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"go", "cli"}, result.Tags)
}

func TestExtractMissingAPIKeyReturnsClearError(t *testing.T) {
	t.Parallel()

	extractor, err := New(domain.TagExtractorConfig{})
	require.NoError(t, err)

	_, err = extractor.Extract(context.Background(), domain.TagExtractRequest{Document: domain.Document{Title: "Doc"}})
	require.EqualError(t, err, "tagextractor/openai: api key is required; set "+openaiprovider.OptionAPIKey)
}

func TestExtractPermissiveLocalParseWhenOutputJSONHasExtraFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":[{"content":[{"type":"output_text","text":"{\"tags\":[\"A\",\"a\"],\"extra\":1}"}]}]}`))
	}))
	defer server.Close()

	extractor, err := New(domain.TagExtractorConfig{
		Options: map[string]string{
			openaiprovider.OptionBaseURL: server.URL + "/v1",
			openaiprovider.OptionAPIKey:  "test-key",
		},
		MaxTags:   8,
		MinTagLen: 1,
		MaxTagLen: 32,
	})
	require.NoError(t, err)

	result, err := extractor.Extract(context.Background(), domain.TagExtractRequest{
		Document: domain.Document{Title: "Doc", Content: "hello"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, result.Tags)
}

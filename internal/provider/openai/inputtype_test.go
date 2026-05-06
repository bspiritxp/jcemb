package openai

import (
	"testing"

	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestResolveInputType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		model    string
		purpose  domain.EmbedPurpose
		override string
		want     string
	}{
		{name: "voyage_document_auto", model: "voyage-4", purpose: domain.EmbedPurposeDocument, want: InputTypeDocument},
		{name: "voyage_query_auto", model: "voyage-4", purpose: domain.EmbedPurposeQuery, want: InputTypeQuery},
		{name: "voyage_namespaced_prefix", model: "voyage/voyage-4", purpose: domain.EmbedPurposeQuery, want: InputTypeQuery},
		{name: "voyage_unspecified_purpose_skips_field", model: "voyage-4", purpose: domain.EmbedPurposeUnspecified, want: ""},
		{name: "openai_native_model_skips_field", model: "text-embedding-3-small", purpose: domain.EmbedPurposeDocument, want: ""},
		{name: "jina_embeddings_query_auto", model: "jina-embeddings-v3", purpose: domain.EmbedPurposeQuery, want: "retrieval.query"},
		{name: "jina_clip_document_auto", model: "jina-clip-v2", purpose: domain.EmbedPurposeDocument, want: "retrieval.passage"},
		{name: "override_off_disables_even_for_voyage", model: "voyage-4", purpose: domain.EmbedPurposeQuery, override: "off", want: ""},
		{name: "override_off_case_insensitive", model: "voyage-4", purpose: domain.EmbedPurposeQuery, override: "OFF", want: ""},
		{name: "override_auto_falls_back_to_preset", model: "voyage-4", purpose: domain.EmbedPurposeDocument, override: "auto", want: InputTypeDocument},
		{name: "override_explicit_value_passes_through", model: "voyage-4", purpose: domain.EmbedPurposeQuery, override: "classification", want: "classification"},
		{name: "override_explicit_value_for_unknown_model", model: "text-embedding-3-small", purpose: domain.EmbedPurposeDocument, override: "search_document", want: "search_document"},
		{name: "empty_model_returns_empty", model: "", purpose: domain.EmbedPurposeDocument, want: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveInputType(tc.model, tc.purpose, tc.override)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSupportsInputType(t *testing.T) {
	t.Parallel()

	require.True(t, SupportsInputType("voyage-4"))
	require.True(t, SupportsInputType("voyage/voyage-3-large"))
	require.True(t, SupportsInputType("jina-embeddings-v3"))
	require.True(t, SupportsInputType("jina-clip-v2"))
	require.False(t, SupportsInputType("text-embedding-3-small"))
	require.False(t, SupportsInputType(""))
}

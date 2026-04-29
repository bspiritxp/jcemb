package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderCollectionListJSONEmittsEmptyArrayForEmpty(t *testing.T) {
	buf := &bytes.Buffer{}
	require.NoError(t, RenderCollectionListJSON(buf, "/tmp/data", nil))

	var envelope CollectionListJSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))
	require.Equal(t, CollectionListSchemaVersionV1, envelope.Version)
	require.Equal(t, "/tmp/data", envelope.DataDir)
	require.NotNil(t, envelope.Collections)
	require.Len(t, envelope.Collections, 0)
}

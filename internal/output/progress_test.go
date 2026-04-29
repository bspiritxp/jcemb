package output

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bspiritxp/jcemb/internal/app/embed"
	"github.com/bspiritxp/jcemb/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestEmbedProgressUpdateFitsTerminalWidth(t *testing.T) {
	buf := &bytes.Buffer{}
	progress := NewEmbedProgressBar(buf)
	progress.terminalWidth = func(io.Writer) int {
		return 72
	}

	progress.Update(embed.ProgressUpdate{
		Total:     32,
		Completed: 16,
		Current:   "/Users/me/jc-dev-docs/运维/飞书 CLI 总结/非常长的中文路径/notes.md",
		Status:    "completed",
	})

	line := strings.TrimPrefix(stripANSIEscapes(buf.String()), "\r")
	require.LessOrEqual(t, displayWidth(line), 72)
	require.Contains(t, line, "...")
	require.True(t, utf8.ValidString(line))
}

func TestTruncatePathUsesDisplayWidth(t *testing.T) {
	truncated := truncatePath("/tmp/运维/飞书 CLI 总结.md", 16)

	require.True(t, utf8.ValidString(truncated))
	require.LessOrEqual(t, displayWidth(truncated), 16)
	require.Equal(t, "... CLI 总结.md", truncated)
}

func TestEmbedProgressFinishIncludesCollectionMetadata(t *testing.T) {
	buf := &bytes.Buffer{}
	progress := NewEmbedProgressBar(buf)

	progress.Finish(embed.Result{
		Summary: embed.Summary{
			Processed: 3,
			Updated:   2,
			Skipped:   1,
		},
		CollectionCount: 1,
		Store: domain.StoreConfig{
			CollectionID: "abc1234567890def",
			Model:        "fixture-model",
			VectorDim:    1024,
		},
	})

	output := stripANSIEscapes(buf.String())
	require.Contains(t, output, "Scan complete!")
	require.Contains(t, output, "Collection: abc1234567890def")
	require.Contains(t, output, "Model:      fixture-model")
	require.Contains(t, output, "Vector dim: 1024")
	require.Contains(t, output, "Processed:  3")
	require.Contains(t, output, "Updated:    2")
	require.Contains(t, output, "Skipped:    1")
}

func TestEmbedProgressFinishOmitsCollectionMetadataForMultipleCollections(t *testing.T) {
	buf := &bytes.Buffer{}
	progress := NewEmbedProgressBar(buf)

	progress.Finish(embed.Result{
		Summary: embed.Summary{
			Processed: 5,
			Updated:   4,
			Skipped:   1,
		},
		CollectionCount: 2,
		Store: domain.StoreConfig{
			CollectionID: "abc1234567890def",
			Model:        "fixture-model",
			VectorDim:    1024,
		},
	})

	output := stripANSIEscapes(buf.String())
	require.Contains(t, output, "Scan complete!")
	require.NotContains(t, output, "Collection:")
	require.NotContains(t, output, "Model:")
	require.NotContains(t, output, "Vector dim:")
	require.Contains(t, output, "Processed:  5")
}

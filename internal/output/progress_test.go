package output

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bspiritxp/jcemb/internal/app/embed"
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

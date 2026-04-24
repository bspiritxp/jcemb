package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbedReturnsFailingFileDetailsInErrorText(t *testing.T) {
	rootDir := t.TempDir()
	badPath := filepath.Join(rootDir, "docs", "bad.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(badPath), 0o755))
	require.NoError(t, os.WriteFile(badPath, []byte("---\ntags: [broken\n---\nbody\n"), 0o644))

	err := Embed(EmbedRequest{
		Path:        rootDir,
		Type:        "md",
		Concurrency: 1,
		Provider:    "ollama",
		Model:       "bge-m3",
		Recursive:   true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "completed with 1 file error(s)")
	require.Contains(t, err.Error(), "docs/bad.md")
	require.Contains(t, err.Error(), "invalid yaml front matter")
}

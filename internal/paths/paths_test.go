package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveCollectionRoot(t *testing.T) {
	rootDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "docs", "nested"), 0o755))
	filePath := filepath.Join(rootDir, "docs", "nested", "guide.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# Guide\n"), 0o644))

	absRoot := filepath.Clean(rootDir)
	fileRoot := filepath.Join(absRoot, "docs", "nested")

	t.Run("relative and absolute directory inputs share the same identity", func(t *testing.T) {
		t.Chdir(rootDir)

		relative, err := ResolveCollectionRoot(".")
		require.NoError(t, err)

		absolute, err := ResolveCollectionRoot(rootDir)
		require.NoError(t, err)

		require.True(t, relative.IsDir)
		require.True(t, absolute.IsDir)
		require.Equal(t, absRoot, relative.RootDir)
		require.Equal(t, absRoot, absolute.RootDir)
		require.Equal(t, relative.Identity, absolute.Identity)
	})

	t.Run("file input resolves to parent dir identity", func(t *testing.T) {
		resolved, err := ResolveCollectionRoot(filePath)
		require.NoError(t, err)

		require.False(t, resolved.IsDir)
		require.Equal(t, filepath.Clean(filePath), resolved.InputPath)
		require.Equal(t, fileRoot, resolved.RootDir)
		require.Equal(t, NormalizeStoredPath(fileRoot), resolved.Identity)
	})

	t.Run("tilde inputs expand before canonicalization", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		require.NoError(t, err)

		tildeRoot := filepath.Join(homeDir, "jcemb-path-helper-test")
		require.NoError(t, os.MkdirAll(tildeRoot, 0o755))
		t.Cleanup(func() {
			_ = os.RemoveAll(tildeRoot)
		})

		resolved, err := ResolveCollectionRoot(filepath.Join("~", "jcemb-path-helper-test"))
		require.NoError(t, err)
		require.Equal(t, filepath.Clean(tildeRoot), resolved.RootDir)
	})
}

func TestNormalizeStoredPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty trims to blank", input: "   ", want: ""},
		{name: "relative segments cleaned", input: " ./docs/../docs/nested/guide.md ", want: "docs/nested/guide.md"},
		{name: "backslashes normalize to slashes", input: `docs\nested\guide.md`, want: "docs/nested/guide.md"},
		{name: "windows-style relative paths are case folded", input: `DOCS\NESTED\Guide.md`, want: "docs/nested/guide.md"},
		{name: "windows drive letters are case folded", input: `C:\Users\Alice\Docs\Guide.md`, want: "c:/users/alice/docs/guide.md"},
		{name: "windows slash paths are case folded", input: `D:/Work/Project/Notes`, want: "d:/work/project/notes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NormalizeStoredPath(tt.input))
		})
	}
}

func TestResolveAppPaths(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveAppPaths()
	require.NoError(t, err)

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	require.Equal(t, filepath.Join(homeDir, ".config", "jcemb", "jcemb.json"), resolved.ConfigFile)
	require.Equal(t, filepath.Join(homeDir, ".local", "share", "jcemb"), resolved.DataRoot)
}

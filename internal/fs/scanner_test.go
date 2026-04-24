package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanMarkdownRecursiveRespectsIgnoredDirectories(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "root.md"), []byte("# root"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "nested", "guide.md"), []byte("# guide"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "nested", "notes.txt"), []byte("ignore"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "ignored.md"), []byte("# ignored"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".vectordb"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".vectordb", "ignored.md"), []byte("# ignored"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "pkg", "ignored.md"), []byte("# ignored"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: filepath.Join(root, "."), Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 2)

	require.Equal(t, []File{
		{
			RootDir:  filepath.ToSlash(filepath.Clean(root)),
			FilePath: filepath.ToSlash(filepath.Join(root, "docs", "nested", "guide.md")),
			RelPath:  "docs/nested/guide.md",
			FileName: "guide.md",
			DocType:  "md",
		},
		{
			RootDir:  filepath.ToSlash(filepath.Clean(root)),
			FilePath: filepath.ToSlash(filepath.Join(root, "root.md")),
			RelPath:  "root.md",
			FileName: "root.md",
			DocType:  "md",
		},
	}, files)
}

func TestScanMarkdownNonRecursiveOnlyReadsTopLevelMarkdown(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "root.md"), []byte("# root"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# guide"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: false})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "root.md", files[0].RelPath)
	require.Equal(t, "root.md", files[0].FileName)
	require.Equal(t, "md", files[0].DocType)
}

func TestScanMarkdownSupportsSingleFileRoots(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "guide.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# guide"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: filePath, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Clean(root)), files[0].RootDir)
	require.Equal(t, "guide.md", files[0].RelPath)
	require.Equal(t, filepath.ToSlash(filepath.Clean(filePath)), files[0].FilePath)
}

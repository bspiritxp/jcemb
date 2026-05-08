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
	require.NoError(t, os.WriteFile(filepath.Join(root, ".vectordb", "legacy.md"), []byte("# legacy"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "pkg", "ignored.md"), []byte("# ignored"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: filepath.Join(root, "."), Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 3)

	require.Equal(t, []File{
		{
			RootDir:  filepath.ToSlash(filepath.Clean(root)),
			FilePath: filepath.ToSlash(filepath.Join(root, ".vectordb", "legacy.md")),
			RelPath:  filepath.ToSlash(filepath.Join(root, ".vectordb", "legacy.md")),
			FileName: "legacy.md",
			DocType:  "md",
		},
		{
			RootDir:  filepath.ToSlash(filepath.Clean(root)),
			FilePath: filepath.ToSlash(filepath.Join(root, "docs", "nested", "guide.md")),
			RelPath:  filepath.ToSlash(filepath.Join(root, "docs", "nested", "guide.md")),
			FileName: "guide.md",
			DocType:  "md",
		},
		{
			RootDir:  filepath.ToSlash(filepath.Clean(root)),
			FilePath: filepath.ToSlash(filepath.Join(root, "root.md")),
			RelPath:  filepath.ToSlash(filepath.Join(root, "root.md")),
			FileName: "root.md",
			DocType:  "md",
		},
	}, []File{
		{
			RootDir:  files[0].RootDir,
			FilePath: files[0].FilePath,
			RelPath:  files[0].RelPath,
			FileName: files[0].FileName,
			DocType:  files[0].DocType,
		},
		{
			RootDir:  files[1].RootDir,
			FilePath: files[1].FilePath,
			RelPath:  files[1].RelPath,
			FileName: files[1].FileName,
			DocType:  files[1].DocType,
		},
		{
			RootDir:  files[2].RootDir,
			FilePath: files[2].FilePath,
			RelPath:  files[2].RelPath,
			FileName: files[2].FileName,
			DocType:  files[2].DocType,
		},
	})
	require.False(t, files[0].ModTime.IsZero())
	require.False(t, files[1].ModTime.IsZero())
	require.False(t, files[2].ModTime.IsZero())
}

func TestScanFilesUsesRegisteredExtensions(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("# A"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.png"), []byte("png"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "c.txt"), []byte("txt"), 0o644))

	files, err := ScanFiles(ScanOptions{
		RootPath:  root,
		Recursive: true,
		Extensions: map[string]string{
			".md":  "markdown",
			".png": "image",
		},
	})

	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "markdown", files[0].DocType)
	require.Equal(t, "image", files[1].DocType)
}

func TestScanFilesFiltersExtensionsAndExcludePatterns(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "keep.md"), []byte("# keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "drop.md"), []byte("# drop"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "image.png"), []byte("png"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "notes.txt"), []byte("txt"), 0o644))

	files, err := ScanFiles(ScanOptions{
		RootPath:        root,
		Recursive:       true,
		ExcludePatterns: []string{"docs/drop.md"},
		Extensions: map[string]string{
			".md":  "markdown",
			".png": "image",
		},
	})
	require.NoError(t, err)

	relPaths := make([]string, 0, len(files))
	for _, file := range files {
		relPaths = append(relPaths, file.RelPath)
	}
	require.ElementsMatch(t, []string{
		filepath.ToSlash(filepath.Join(root, "docs", "image.png")),
		filepath.ToSlash(filepath.Join(root, "docs", "keep.md")),
	}, relPaths)
}

func TestScanMarkdownNonRecursiveOnlyReadsTopLevelMarkdown(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "root.md"), []byte("# root"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# guide"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: false})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "root.md")), files[0].RelPath)
	require.Equal(t, "root.md", files[0].FileName)
	require.Equal(t, "md", files[0].DocType)
	require.False(t, files[0].ModTime.IsZero())
}

func TestScanMarkdownSupportsSingleFileRoots(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "guide.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# guide"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: filePath, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Clean(root)), files[0].RootDir)
	require.Equal(t, filepath.ToSlash(filepath.Clean(filePath)), files[0].RelPath)
	require.Equal(t, filepath.ToSlash(filepath.Clean(filePath)), files[0].FilePath)
	require.False(t, files[0].ModTime.IsZero())
}

func TestScanMarkdownGitIgnoreFiltersFilesAndDirectories(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.md"), []byte("# keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "draft.md"), []byte("# draft"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "build"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "build", "out.md"), []byte("# out"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# guide"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "pkg", "kept.md"), []byte("# kept"), 0o644))

	gitignore := "draft.md\nbuild/\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: true})
	require.NoError(t, err)

	relPaths := make([]string, 0, len(files))
	for _, f := range files {
		relPaths = append(relPaths, f.RelPath)
	}

	require.ElementsMatch(t, []string{
		filepath.ToSlash(filepath.Join(root, "docs", "guide.md")),
		filepath.ToSlash(filepath.Join(root, "keep.md")),
	}, relPaths)
}

func TestScanMarkdownGitIgnoreSupportsNegation(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "drop.md"), []byte("# drop"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "keep.md"), []byte("# keep"), 0o644))

	gitignore := "docs/*.md\n!docs/keep.md\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "docs", "keep.md")), files[0].RelPath)
}

func TestScanMarkdownGitIgnoreDoesNotSpecialCaseLegacyVectorDB(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "root.md"), []byte("# root"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".vectordb"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".vectordb", "leak.md"), []byte("# leak"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("# empty\n"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, ".vectordb", "leak.md")), files[0].RelPath)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "root.md")), files[1].RelPath)
}

func TestScanMarkdownGitIgnoreDoesNotApplyToSingleFileRoot(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "draft.md")
	require.NoError(t, os.WriteFile(filePath, []byte("# draft"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("draft.md\n"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: filePath, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filePath), files[0].RelPath)
}

func TestScanMarkdownWithoutGitIgnoreUsesDefaultIgnoreList(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "root.md"), []byte("# root"), 0o644))
	for _, dir := range []string{
		".claude",
		".codex",
		".git",
		".idea",
		".obsidian",
		".vscode",
		filepath.Join("node_modules", "pkg"),
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, "ignored.md"), []byte("# ignored"), 0o644))
	}

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "root.md")), files[0].RelPath)
}

func TestScanMarkdownIgnoresRecycleBinAndSystemDirectories(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "root.md"), []byte("# root"), 0o644))
	for _, dir := range []string{
		"$RECYCLE.BIN",
		"RECYCLER",
		"System Volume Information",
		".Trashes",
		".Trash",
		".Trash-1000",
		".Trash-501",
		".fseventsd",
		".Spotlight-V100",
		".DocumentRevisions-V100",
		".TemporaryItems",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, "leak.md"), []byte("# leak"), 0o644))
	}

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "root.md")), files[0].RelPath)
}

func TestScanMarkdownIgnoredDirectoryNamesAreCaseInsensitive(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "root.md"), []byte("# root"), 0o644))
	for _, dir := range []string{
		"$Recycle.Bin",
		".trashes",
		"system volume INFORMATION",
		".TRASH-1000",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, "leak.md"), []byte("# leak"), 0o644))
	}

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "root.md")), files[0].RelPath)
}

func TestScanMarkdownDefaultIgnoreListAlsoAppliesAlongsideGitIgnore(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.md"), []byte("# keep"), 0o644))
	for _, dir := range []string{".git", "node_modules", "$RECYCLE.BIN", ".Trash-1000"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, "leak.md"), []byte("# leak"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("# user gitignore present\n"), 0o644))

	files, err := ScanMarkdown(ScanOptions{RootPath: root, Recursive: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "keep.md")), files[0].RelPath)
}

func TestScanMarkdownExcludePatternsApplyOnTopOfGitIgnore(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.md"), []byte("# keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "draft.md"), []byte("# draft"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "drop.md"), []byte("# drop"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "keep.md"), []byte("# keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("draft.md\n"), 0o644))

	files, err := ScanMarkdown(ScanOptions{
		RootPath:        root,
		Recursive:       true,
		ExcludePatterns: []string{"docs/drop.md"},
	})
	require.NoError(t, err)

	relPaths := make([]string, 0, len(files))
	for _, file := range files {
		relPaths = append(relPaths, file.RelPath)
	}
	require.ElementsMatch(t, []string{
		filepath.ToSlash(filepath.Join(root, "docs", "keep.md")),
		filepath.ToSlash(filepath.Join(root, "keep.md")),
	}, relPaths)
}

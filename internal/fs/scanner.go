package fs

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const markdownDocType = "md"

var ignoredDirectories = map[string]struct{}{
	".git":         {},
	".vectordb":    {},
	"node_modules": {},
}

type ScanOptions struct {
	RootPath  string
	Recursive bool
}

type File struct {
	RootDir  string
	FilePath string
	RelPath  string
	FileName string
	DocType  string
	ModTime  time.Time
}

func ScanMarkdown(options ScanOptions) ([]File, error) {
	if strings.TrimSpace(options.RootPath) == "" {
		return nil, fmt.Errorf("fs: root path is required")
	}

	rootPath, err := filepath.Abs(options.RootPath)
	if err != nil {
		return nil, fmt.Errorf("fs: resolve root path: %w", err)
	}

	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, fmt.Errorf("fs: stat root path: %w", err)
	}

	if info.IsDir() {
		return scanDirectory(rootPath, options.Recursive)
	}

	file, ok, err := newMarkdownFile(filepath.Dir(rootPath), rootPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []File{}, nil
	}

	return []File{file}, nil
}

func scanDirectory(rootPath string, recursive bool) ([]File, error) {
	files := make([]File, 0)

	err := filepath.WalkDir(rootPath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("fs: walk %s: %w", currentPath, walkErr)
		}

		if entry.IsDir() {
			if currentPath != rootPath {
				if shouldIgnoreDirectory(entry.Name()) {
					return filepath.SkipDir
				}

				if !recursive {
					return filepath.SkipDir
				}
			}
			return nil
		}

		file, ok, err := newMarkdownFile(rootPath, currentPath)
		if err != nil {
			return err
		}
		if ok {
			files = append(files, file)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

func newMarkdownFile(rootPath string, filePath string) (File, bool, error) {
	if strings.ToLower(filepath.Ext(filePath)) != ".md" {
		return File{}, false, nil
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return File{}, false, fmt.Errorf("fs: stat %s: %w", filePath, err)
	}

	normalizedRoot, err := normalizeAbsolutePath(rootPath)
	if err != nil {
		return File{}, false, err
	}

	normalizedFilePath, err := normalizeAbsolutePath(filePath)
	if err != nil {
		return File{}, false, err
	}

	relPath, err := filepath.Rel(rootPath, filePath)
	if err != nil {
		return File{}, false, fmt.Errorf("fs: derive relative path for %s: %w", filePath, err)
	}

	return File{
		RootDir:  normalizedRoot,
		FilePath: normalizedFilePath,
		RelPath:  normalizeRelativePath(relPath),
		FileName: path.Base(normalizedFilePath),
		DocType:  markdownDocType,
		ModTime:  info.ModTime().UTC(),
	}, true, nil
}

func normalizeAbsolutePath(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("fs: normalize absolute path %s: %w", value, err)
	}

	return filepath.ToSlash(filepath.Clean(abs)), nil
}

func normalizeRelativePath(value string) string {
	cleaned := path.Clean(filepath.ToSlash(value))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func shouldIgnoreDirectory(name string) bool {
	_, ignored := ignoredDirectories[name]
	return ignored
}

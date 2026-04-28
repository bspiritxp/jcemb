package fs

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	gitignore "github.com/denormal/go-gitignore"
)

const (
	markdownDocType   = "md"
	gitIgnoreFileName = ".gitignore"
)

var defaultIgnoredDirectories = map[string]struct{}{
	".claude":      {},
	".codex":       {},
	".git":         {},
	".idea":        {},
	".obsidian":    {},
	".vscode":      {},
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

	matcher, err := loadGitIgnoreMatcher(rootPath)
	if err != nil {
		return nil, err
	}

	err = filepath.WalkDir(rootPath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("fs: walk %s: %w", currentPath, walkErr)
		}

		if entry.IsDir() {
			if currentPath == rootPath {
				return nil
			}

			if shouldIgnoreDirectory(rootPath, currentPath, entry.Name(), matcher) {
				return filepath.SkipDir
			}

			if !recursive {
				return filepath.SkipDir
			}

			return nil
		}

		if shouldIgnoreFile(rootPath, currentPath, matcher) {
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

	return File{
		RootDir:  normalizedRoot,
		FilePath: normalizedFilePath,
		RelPath:  normalizedFilePath,
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

func loadGitIgnoreMatcher(rootPath string) (gitignore.GitIgnore, error) {
	candidate := filepath.Join(rootPath, gitIgnoreFileName)
	info, err := os.Stat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("fs: stat %s: %w", candidate, err)
	}
	if info.IsDir() {
		return nil, nil
	}

	matcher, err := gitignore.NewFromFile(candidate)
	if err != nil {
		return nil, fmt.Errorf("fs: parse %s: %w", candidate, err)
	}
	return matcher, nil
}

func shouldIgnoreDirectory(rootPath, currentPath, name string, matcher gitignore.GitIgnore) bool {
	if matcher == nil {
		_, ignored := defaultIgnoredDirectories[name]
		return ignored
	}

	rel, ok := relativeSlashPath(rootPath, currentPath)
	if !ok {
		return false
	}

	match := matcher.Relative(rel, true)
	return match != nil && match.Ignore()
}

func shouldIgnoreFile(rootPath, currentPath string, matcher gitignore.GitIgnore) bool {
	if matcher == nil {
		return false
	}

	rel, ok := relativeSlashPath(rootPath, currentPath)
	if !ok {
		return false
	}

	match := matcher.Relative(rel, false)
	return match != nil && match.Ignore()
}

func relativeSlashPath(rootPath, currentPath string) (string, bool) {
	rel, err := filepath.Rel(rootPath, currentPath)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return "", false
	}
	return rel, true
}

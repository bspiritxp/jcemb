package fs

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	gitignore "github.com/denormal/go-gitignore"
)

const (
	markdownDocType   = "md"
	gitIgnoreFileName = ".gitignore"
)

var defaultIgnoredDirectoryNames = []string{
	".claude",
	".codex",
	".git",
	".idea",
	".obsidian",
	".vscode",
	"node_modules",

	"$RECYCLE.BIN",
	"RECYCLER",
	"System Volume Information",

	".Trashes",
	".Trash",
	".fseventsd",
	".Spotlight-V100",
	".DocumentRevisions-V100",
	".TemporaryItems",
}

// defaultIgnoredDirectoryPrefixes 匹配那些带可变后缀的回收站/系统目录名
// （例如 Linux XDG 回收站会以 ".Trash-1000" 形式按 UID 命名）。
// 比较时一律小写化，所以这些常量本身用小写即可。
var defaultIgnoredDirectoryPrefixes = []string{
	".trash-",
}

var defaultIgnoredDirectorySet = buildIgnoredDirectorySet(defaultIgnoredDirectoryNames)

func buildIgnoredDirectorySet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = struct{}{}
	}
	return set
}

func isDefaultIgnoredDirectory(name string) bool {
	lowered := strings.ToLower(name)
	if _, ok := defaultIgnoredDirectorySet[lowered]; ok {
		return true
	}
	for _, prefix := range defaultIgnoredDirectoryPrefixes {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

type ScanOptions struct {
	RootPath        string
	Recursive       bool
	Extensions      map[string]string
	ExcludePatterns []string
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
	options.Extensions = map[string]string{".md": markdownDocType}
	return ScanFiles(options)
}

func ScanFiles(options ScanOptions) ([]File, error) {
	if strings.TrimSpace(options.RootPath) == "" {
		return nil, fmt.Errorf("fs: root path is required")
	}
	extensions := normalizeExtensions(options.Extensions)
	if len(extensions) == 0 {
		return nil, fmt.Errorf("fs: at least one extension is required")
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
		return scanDirectory(rootPath, options.Recursive, extensions, options.ExcludePatterns)
	}

	file, ok, err := newFile(filepath.Dir(rootPath), rootPath, extensions)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []File{}, nil
	}

	return []File{file}, nil
}

func scanDirectory(rootPath string, recursive bool, extensions map[string]string, excludePatterns []string) ([]File, error) {
	files := make([]File, 0)

	matcher, err := loadGitIgnoreMatcher(rootPath)
	if err != nil {
		return nil, err
	}
	excludeMatcher, err := buildExcludeMatcher(rootPath, excludePatterns)
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

			if shouldIgnoreDirectory(rootPath, currentPath, entry.Name(), matcher) || shouldIgnoreDirectory(rootPath, currentPath, entry.Name(), excludeMatcher) {
				return filepath.SkipDir
			}

			if !recursive {
				return filepath.SkipDir
			}

			return nil
		}

		if shouldIgnoreFile(rootPath, currentPath, matcher) || shouldIgnoreFile(rootPath, currentPath, excludeMatcher) {
			return nil
		}

		file, ok, err := newFile(rootPath, currentPath, extensions)
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

func buildExcludeMatcher(rootPath string, patterns []string) (gitignore.GitIgnore, error) {
	normalized := normalizeExcludePatterns(patterns)
	if len(normalized) == 0 {
		return nil, nil
	}
	return gitignore.New(strings.NewReader(strings.Join(normalized, "\n")), rootPath, nil), nil
}

func normalizeExcludePatterns(values []string) []string {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, piece := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' }) {
			token := strings.TrimSpace(piece)
			if token == "" || slices.Contains(out, token) {
				continue
			}
			if looksLikeExtensionPattern(token) {
				token = "*" + token
			}
			out = append(out, token)
		}
	}
	return out
}

func looksLikeExtensionPattern(value string) bool {
	if !strings.HasPrefix(value, ".") || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, `/\*?[]`)
}

func newFile(rootPath string, filePath string, extensions map[string]string) (File, bool, error) {
	docType, ok := extensions[strings.ToLower(filepath.Ext(filePath))]
	if !ok {
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
		DocType:  docType,
		ModTime:  info.ModTime().UTC(),
	}, true, nil
}

func normalizeExtensions(values map[string]string) map[string]string {
	normalized := make(map[string]string, len(values))
	for extension, docType := range values {
		trimmedExtension := strings.TrimSpace(strings.ToLower(extension))
		if trimmedExtension == "" {
			continue
		}
		if !strings.HasPrefix(trimmedExtension, ".") {
			trimmedExtension = "." + trimmedExtension
		}
		trimmedDocType := strings.TrimSpace(docType)
		if trimmedDocType == "" {
			continue
		}
		normalized[trimmedExtension] = trimmedDocType
	}
	return normalized
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
	if isDefaultIgnoredDirectory(name) {
		return true
	}
	if matcher == nil {
		return false
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

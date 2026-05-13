package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type AppPaths struct {
	ConfigFile string
	DataRoot   string
}

type CollectionRoot struct {
	InputPath string
	RootDir   string
	Identity  string
	IsDir     bool
}

func ResolveAppPaths() (AppPaths, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return AppPaths{}, err
	}

	homeDir = filepath.Clean(homeDir)
	return AppPaths{
		ConfigFile: filepath.Join(homeDir, ".config", "jcemb", "jcemb.json"),
		DataRoot:   filepath.Join(homeDir, ".local", "share", "jcemb"),
	}, nil
}

func ResolveAbsolutePath(input string) (string, error) {
	expanded, err := ExpandUserHome(strings.TrimSpace(input))
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absPath), nil
}

func ResolveCollectionRoot(inputPath string) (CollectionRoot, error) {
	absPath, err := ResolveAbsolutePath(inputPath)
	if err != nil {
		return CollectionRoot{}, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return CollectionRoot{}, err
	}

	rootDir := absPath
	if !info.IsDir() {
		rootDir = filepath.Dir(absPath)
	}

	return CollectionRoot{
		InputPath: absPath,
		RootDir:   rootDir,
		Identity:  NormalizeStoredPath(rootDir),
		IsDir:     info.IsDir(),
	}, nil
}

func NormalizeStoredPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "." {
		return ""
	}

	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	cleaned := path.Clean(normalized)
	if cleaned == "." {
		return ""
	}
	cleaned = strings.TrimPrefix(cleaned, "./")

	if isWindowsPath(cleaned) || strings.Contains(trimmed, "\\") {
		return strings.ToLower(cleaned)
	}

	return cleaned
}

func CollectionIDForRoot(rootIdentity string) string {
	normalized := NormalizeStoredPath(rootIdentity)
	if normalized == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:16])
}

func CollectionStorageDir(dataRoot string, collectionID string) string {
	return filepath.Join(filepath.Clean(strings.TrimSpace(dataRoot)), "collections", strings.TrimSpace(collectionID))
}

func ExpandUserHome(value string) (string, error) {
	return expandUserHome(strings.TrimSpace(value))
}

func expandUserHome(value string) (string, error) {
	if value == "" || value[0] != '~' {
		return value, nil
	}
	if len(value) > 1 && value[1] != '/' && value[1] != '\\' {
		return "", fmt.Errorf("unsupported home path %q", value)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if value == "~" {
		return homeDir, nil
	}

	remainder := strings.TrimLeft(value[1:], "/\\")
	return filepath.Join(homeDir, remainder), nil
}

func isWindowsPath(value string) bool {
	if strings.HasPrefix(value, "//") {
		return true
	}
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	first := value[0]
	return (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
}

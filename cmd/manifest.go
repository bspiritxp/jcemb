package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

var embeddedManifest []byte

type Manifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Version     string `json:"version"`
}

func SetManifest(content []byte) {
	embeddedManifest = append([]byte(nil), content...)
}

func LoadManifest() (Manifest, error) {
	content := embeddedManifest
	if len(content) == 0 {
		var err error
		content, err = readManifestFile()
		if err != nil {
			return Manifest{}, err
		}
	}
	return ParseManifest(content)
}

func readManifestFile() ([]byte, error) {
	candidates := []string{"manifest.json", "../manifest.json"}
	for _, candidate := range candidates {
		content, err := os.ReadFile(candidate)
		if err == nil {
			return content, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest: read %s: %w", candidate, err)
		}
	}
	return nil, fmt.Errorf("manifest: read manifest.json: not found")
}

func ParseManifest(content []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("manifest: parse: %w", err)
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Author = strings.TrimSpace(manifest.Author)
	manifest.Version = strings.TrimSpace(manifest.Version)
	if manifest.Name == "" {
		return Manifest{}, fmt.Errorf("manifest: name is required")
	}
	if manifest.Description == "" {
		return Manifest{}, fmt.Errorf("manifest: description is required")
	}
	if manifest.Author == "" {
		return Manifest{}, fmt.Errorf("manifest: author is required")
	}
	if manifest.Version == "" {
		return Manifest{}, fmt.Errorf("manifest: version is required")
	}
	return manifest, nil
}

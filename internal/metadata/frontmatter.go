package metadata

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var frontMatterPattern = regexp.MustCompile(`(?s)\A---[ \t]*\r?\n(.*?)\r?\n(?:---|\.\.\.)[ \t]*(?:\r?\n|$)`)

func ExtractFrontMatter(content string) (string, map[string]any, error) {
	matches := frontMatterPattern.FindStringSubmatchIndex(content)
	if matches == nil {
		return content, map[string]any{}, nil
	}

	frontMatter := content[matches[2]:matches[3]]
	body := content[matches[1]:]

	parsed, err := parseYAMLMap(frontMatter)
	if err != nil {
		return "", nil, err
	}

	return strings.TrimLeft(body, "\r\n"), parsed, nil
}

func parseYAMLMap(content string) (map[string]any, error) {
	if strings.TrimSpace(content) == "" {
		return map[string]any{}, nil
	}

	var decoded any
	if err := yaml.Unmarshal([]byte(content), &decoded); err != nil {
		return nil, fmt.Errorf("invalid yaml front matter: %w", err)
	}

	if decoded == nil {
		return map[string]any{}, nil
	}

	normalized, err := normalizeYAMLValue(decoded)
	if err != nil {
		return nil, err
	}

	values, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid yaml front matter: top-level value must be a mapping")
	}

	return values, nil
}

func normalizeYAMLValue(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized, err := normalizeYAMLValue(child)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			text, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("invalid yaml front matter: map key %v is not a string", key)
			}
			normalized, err := normalizeYAMLValue(child)
			if err != nil {
				return nil, err
			}
			result[text] = normalized
		}
		return result, nil
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeYAMLValue(item)
			if err != nil {
				return nil, err
			}
			items[index] = normalized
		}
		return items, nil
	default:
		return typed, nil
	}
}

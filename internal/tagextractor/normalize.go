package tagextractor

import (
	"strings"
	"unicode"

	"github.com/bspiritxp/jcemb/internal/domain"
)

func NormalizeSemanticTags(raw []string, cfg domain.TagExtractorConfig) []string {
	if len(raw) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(raw))
	normalized := make([]string, 0, len(raw))
	for _, tag := range raw {
		value := normalizeSemanticTag(tag)
		if value == "" {
			continue
		}
		if !semanticTagLengthWithinBounds(value, cfg) || semanticTagIsRejected(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
		if cfg.MaxTags > 0 && len(normalized) >= cfg.MaxTags {
			break
		}
	}

	return normalized
}

func normalizeSemanticTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

func semanticTagLengthWithinBounds(tag string, cfg domain.TagExtractorConfig) bool {
	length := len([]rune(tag))
	if cfg.MinTagLen > 0 && length < cfg.MinTagLen {
		return false
	}
	if cfg.MaxTagLen > 0 && length > cfg.MaxTagLen {
		return false
	}
	return true
}

func semanticTagIsRejected(tag string) bool {
	if containsControlRune(tag) || containsURLMarker(tag) {
		return true
	}
	return isPureNumber(tag) || isPurePunctuation(tag)
}

func containsControlRune(tag string) bool {
	for _, r := range tag {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func containsURLMarker(tag string) bool {
	return strings.Contains(tag, "://") || strings.Contains(tag, "http://") || strings.Contains(tag, "https://")
}

func isPureNumber(tag string) bool {
	if tag == "" {
		return false
	}
	for _, r := range tag {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isPurePunctuation(tag string) bool {
	if tag == "" {
		return false
	}
	for _, r := range tag {
		if !unicode.IsPunct(r) && !unicode.IsSymbol(r) {
			return false
		}
	}
	return true
}

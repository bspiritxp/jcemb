package openai

import (
	"strings"

	"github.com/bspiritxp/jcemb/internal/domain"
)

const (
	OptionInputType = "openai_input_type"

	InputTypeDocument = "document"
	InputTypeQuery    = "query"

	inputTypeOverrideDisabled = "off"
	inputTypeOverrideAuto     = "auto"
)

// SupportsInputType 表示模型在 ResolveInputType 中是否参与自动派发。
// 没列入的模型（例如 OpenAI 自家的 text-embedding-*）会自动跳过 input_type，
// 避免向不认识该字段的上游报错。
func SupportsInputType(modelName string) bool {
	return matchInputTypePreset(modelName) != nil
}

// ResolveInputType 根据 (模型名, EmbedPurpose, 用户显式覆盖) 决定最终
// 应该写入请求体的 input_type 字符串。返回空串表示请求里不要带 input_type。
//
// 优先级：
//  1. override == "off"        -> 强制不发送
//  2. override == "auto" / ""  -> 走预设表自动派发
//  3. override 为其它非空值     -> 原样使用（用户显式指定，例如 "classification"）
func ResolveInputType(modelName string, purpose domain.EmbedPurpose, override string) string {
	switch normalizeOverride(override) {
	case inputTypeOverrideDisabled:
		return ""
	case inputTypeOverrideAuto:
		return autoInputType(modelName, purpose)
	default:
		return strings.TrimSpace(override)
	}
}

func autoInputType(modelName string, purpose domain.EmbedPurpose) string {
	preset := matchInputTypePreset(modelName)
	if preset == nil {
		return ""
	}
	switch purpose {
	case domain.EmbedPurposeDocument:
		return preset.Document
	case domain.EmbedPurposeQuery:
		return preset.Query
	default:
		return ""
	}
}

func normalizeOverride(override string) string {
	trimmed := strings.ToLower(strings.TrimSpace(override))
	if trimmed == "" {
		return inputTypeOverrideAuto
	}
	return trimmed
}

type inputTypePreset struct {
	Document string
	Query    string
}

// inputTypePresets 列出已知支持 input_type 字段的模型族；匹配按"前缀包含"
// 进行，可覆盖代理把模型重命名成 "voyage/voyage-4" 这类带命名空间前缀的写法。
var inputTypePresets = []struct {
	match  string
	preset inputTypePreset
}{
	{match: "voyage", preset: inputTypePreset{Document: InputTypeDocument, Query: InputTypeQuery}},
	{match: "jina-embeddings", preset: inputTypePreset{Document: "retrieval.passage", Query: "retrieval.query"}},
	{match: "jina-clip", preset: inputTypePreset{Document: "retrieval.passage", Query: "retrieval.query"}},
}

func matchInputTypePreset(modelName string) *inputTypePreset {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return nil
	}
	for index := range inputTypePresets {
		entry := inputTypePresets[index]
		if strings.Contains(name, entry.match) {
			preset := entry.preset
			return &preset
		}
	}
	return nil
}

---
name: jcemb
description: |
  本地优先的 Go CLI 向量搜索工具，用于将 Markdown 文档和图片嵌入本地向量库并进行语义搜索。
  适用于个人知识库、项目文档、技术笔记等场景。支持 Ollama 和 OpenAI 作为 embedding provider。
  当用户需要：1) 对本地文档建立语义搜索索引；2) 通过自然语言查询文档内容；3) 查看已索引文件的详细信息；4) 管理索引集合时使用此工具。
author: bspiritxp
category: tool
version: 0.1.0
---

# jcemb 使用指南

## 工具概述

`jcemb` 是一个本地优先的命令行工具，用于将 Markdown 文档和图片向量化到本地向量库，并通过语义搜索进行检索。

- **本地优先**: 所有数据存储在本地（`~/.local/share/jcemb`），无需外部向量数据库服务
- **多文件类型**: 支持 Markdown (`.md`) 和图片 (`.png`, `.jpg`, `.jpeg`, `.webp` 等)
- **多 Provider**: 支持 Ollama（默认）和 OpenAI 作为 embedding provider
- **语义搜索**: 使用向量相似度进行语义查询，而不仅仅是关键词匹配

## 适用场景

- **个人知识库**: 对自己的笔记、文档建立可搜索的索引
- **项目文档**: 为项目文档集提供语义搜索能力
- **图片管理**: 通过自然语言描述搜索图片（如"红色自行车"）
- **代码文档**: 索引技术文档和 README，快速找到相关实现说明

## 核心命令

### 1. scan — 扫描并索引文件

将文件扫描到统一向量库中，建立语义搜索索引。

```bash
# 扫描 Markdown 文件（递归）
jcemb scan /path/to/docs -r

# 扫描图片目录
jcemb scan /path/to/images -r

# 强制重新扫描（忽略缓存）
jcemb scan /path/to/docs -r --force

# 使用 OpenAI provider
jcemb scan /path/to/docs -r -p openai -m text-embedding-3-small
```

**常用参数**:
- `-r, --recursive`: 递归扫描子目录
- `-p, --provider`: Embedding provider（默认：ollama）
- `-m, --model`: Embedding model（默认：bge-m3）
- `-c, --concurccy`: 并发 worker 数量（默认：2）
- `--force`: 即使文件未变化也强制重新索引

**注意事项**:
- 扫描会跳过 `.git`、`node_modules` 等目录
- 增量扫描：只处理修改过的文件（通过 hash 判断）
- 会自动清理已删除或重命名的文件

### 2. query — 语义搜索

使用自然语言查询已索引的内容。

```bash
# 查询 Markdown（默认）
jcemb query "how do I configure the gateway?" -l 10

# 查询图片（文本描述搜图）
jcemb query "red bicycle" --file-type image

# 图片搜图（以图搜图）
jcemb query ./query.png --file-type image

# JSON 输出
jcemb query "deployment checklist" --json

# 按标签过滤（AND 语义）
jcemb query "callback flow" --tags architecture,gateway

# 限制搜索范围
jcemb query "API docs" --path /path/to/docs
```

**常用参数**:
- `-l, --limit`: 最大返回结果数（默认：10）
- `-t, --file-type`: 文件类型（默认：markdown；图片搜索用 `image`）
- `--tags`: 标签过滤（多个标签用逗号分隔，AND 语义）
- `--path`: 限制到特定路径
- `--json`: JSON 输出
- `--unique`: 按文件去重
- `--full`: 显示完整内容而非预览

### 3. show — 查看文件详细信息

显示特定文件在向量库中的详细信息。

```bash
# 查看文件信息
jcemb show /path/to/file.md

# JSON 输出
jcemb show /path/to/image.png --json
```

**输出信息**:
- 文件路径、相对路径、文件名、文件类型
- 文件 hash、chunk 数量
- 集合 ID、provider、model、向量维度
- 每个 chunk 的 ID、标签、向量长度、标题、内容

**未找到时**: 显示"未找到"（或 `{"found": false}`）

### 4. collection — 管理索引集合

```bash
# 列出所有集合
jcemb collection list

# JSON 格式列出
jcemb collection list --json

# 删除集合（支持 ID 前缀）
jcemb collection del <collection_id> -y
```

### 5. config — 配置

交互式编辑配置：

```bash
jcemb config
```

**环境变量**:
- `JCEMB_DATA_DIR`: 数据目录
- `JCEMB_PROVIDER`: 默认 provider
- `JCEMB_MODEL`: 默认 model
- `OLLAMA_HOST`: Ollama 地址（默认：`http://localhost:11434`）
- `OPENAI_API_KEY`: OpenAI API Key

## 最佳实践

### 索引策略

1. **按项目组织**: 为每个项目创建独立的目录并分别扫描
2. **定期重建**: 对于频繁变动的文档，定期使用 `--force` 重建
3. **合理并发**: 根据机器性能调整 `--concurccy`，避免过载
4. **标签使用**: 在 Markdown 的 YAML front matter 中添加标签，便于过滤

### 查询技巧

1. **自然语言**: 使用完整的句子或问题，而非关键词
2. **路径限制**: 使用 `--path` 缩小搜索范围，提高准确性
3. **标签过滤**: 结合 `--tags` 进行精确过滤
4. **图片搜索**: 描述越具体，结果越准确

### 图片处理

1. **格式支持**: PNG、JPG、JPEG、WebP 等常见格式
2. **图片描述**: 使用 Ollama vision model（如 llava）或 OpenAI vision 生成描述
3. **向量生成**: 图片默认使用 OpenCLIP 生成向量（512 维）
4. **格式检测**: 工具会自动检测图片真实格式，不依赖文件扩展名
5. **Python 环境**: 图片向量化依赖本地 Python 环境，初始化说明参见 [@python_deps.md](python_deps.md)

## 注意事项

### 依赖要求

- **Ollama**: 如果使用默认 provider，需要本地运行 Ollama
  ```bash
  ollama pull bge-m3
  ollama pull llava  # 如需图片描述
  ollama serve
  ```
- **Python**: 图片向量生成需要 Python 3 和相关包。详见 [@python_deps.md](python_deps.md)。
  ```bash
  pip install open_clip_torch torch pillow
  ```

### 数据存储

- 所有数据存储在 `~/.local/share/jcemb/`（Linux/macOS）
- 包含：配置文件、集合注册表、向量数据、索引清单
- 不要手动修改这些文件

### 性能考量

- **首次扫描**: 需要生成所有向量和描述，耗时较长
- **增量扫描**: 只处理变更文件，速度较快
- **查询速度**: 本地 JSON 存储，查询速度取决于数据量
- **内存使用**: 大数据集可能占用较多内存

### 常见问题

1. **Ollama 连接失败**: 检查 Ollama 是否在运行，`OLLAMA_HOST` 是否正确
2. **图片处理失败**: 可能是图片格式问题或 Ollama vision model 崩溃，尝试转换格式或减少并发
3. **未找到结果**: 确认文件已扫描，使用 `jcemb collection list` 查看集合状态
4. **向量维度不匹配**: 不同 provider/model 可能产生不同维度的向量，需保持一致

## 典型工作流

### 建立知识库索引

```bash
# 1. 配置 provider（如需要）
jcemb config

# 2. 扫描文档
jcemb scan ~/notes -r
jcemb scan ~/projects/docs -r

# 3. 验证索引
jcemb collection list

# 4. 搜索测试
jcemb query "how to deploy" -l 5
```

### 图片搜索工作流

```bash
# 1. 扫描图片目录
jcemb scan ~/Pictures -r

# 2. 文本搜图
jcemb query "sunset beach" --file-type image

# 3. 以图搜图
jcemb query ./query.png --file-type image

# 4. 查看图片详情
jcemb show ~/Pictures/sunset.png
```

### 多集合管理

```bash
# 查看所有集合
jcemb collection list

# 清理不需要的测试集合
jcemb collection del <collection_id> -y

# 查询特定集合
jcemb query "API" --path ~/projects/my-api
```

## 与其他工具配合

- **Obsidian**: 导出笔记后使用 `jcemb scan` 建立索引
- **Git**: 在 CI 中自动更新文档索引
- **Alfred/Raycast**: 通过脚本调用 `jcemb query` 实现快速搜索
- **VS Code**: 通过任务或扩展集成语义搜索

## 版本与更新

- 当前版本: `v0.1.0`
- 更新: `git pull && go build -o jcemb .`
- 查看版本: `jcemb version`

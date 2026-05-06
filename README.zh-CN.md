# jcemb

`jcemb` 是一个本地优先的命令行工具，用于将 Markdown 文档向量化到本地向量库，并通过语义搜索进行检索。

它适合个人知识库、项目文档、技术笔记和其他以 Markdown 为主的资料集合。默认情况下，它使用本地 Ollama 服务生成向量，不需要额外运行独立的向量数据库服务。

## 功能特性

- 仅索引 Markdown 文档。
- 统一的全局配置和向量数据存储。
- 默认 embedding provider：Ollama。
- 默认 embedding model：`bge-m3`。
- 支持递归扫描目录。
- 基于文件 hash 和 recipe hash 做增量扫描。
- 下一次 scan 时自动清理已删除或重命名的文件。
- 从 YAML front matter 中提取标签。
- 标签过滤使用 AND 语义。
- 支持文本输出和 JSON 输出。
- 查询结果内置降噪流程：阈值过滤、去重和 MMR 多样性排序。
- 交互式配置管理。

## 环境要求

- Go 1.26.2 或更新版本。
- 本地运行 Ollama，默认地址为 `http://localhost:11434`。
- Ollama 中已安装 `bge-m3` 模型。

安装并准备 Ollama 模型：

```bash
ollama pull bge-m3
ollama serve
```

如果 Ollama 已经作为后台服务运行，只需要执行模型下载命令。

## 安装

### 使用 Homebrew 安装

```bash
brew tap bspiritxp/tap
brew install jcemb
```

### 使用 Scoop 安装

```powershell
scoop bucket add bspiritxp https://github.com/bspiritxp/scoop-bucket
scoop install jcemb
```

### 使用 Go 安装

```bash
go install github.com/bspiritxp/jcemb@latest
```

确保 Go 的二进制目录已经加入 `PATH`：

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### 从源码构建

```bash
git clone https://github.com/bspiritxp/jcemb.git
cd jcemb
go build -o jcemb .
```

在 Windows 上，请显式构建带 `.exe` 后缀的可执行文件，避免 PowerShell 通过
常规可执行文件解析规则运行旧的 `jcemb.exe`：

```powershell
go build -o jcemb.exe .
```

查看命令帮助：

```bash
./jcemb --help
```

## 快速开始

递归向量化一个 Markdown 文档目录：

```bash
jcemb scan /path/to/docs -r
```

查询已向量化的文档：

```bash
jcemb query "how do I configure the gateway?" -l 10
```

输出 JSON：

```bash
jcemb query "deployment checklist" --json
```

强制完整重建：

```bash
jcemb scan /path/to/docs -r --force
```

## 命令

### `scan`

将已注册文件类型写入统一向量库。内置支持 Markdown 和图片。

```bash
jcemb scan [path] [flags]
```

常用参数：

| 参数 | 说明 |
|---|---|
| `-r, --recursive` | 递归扫描子目录。 |
| `-p, --provider` | Embedding provider。默认：来自配置。 |
| `-m, --model` | Embedding model。默认：来自配置。 |
| `-c, --concurccy` | 并发 worker 数量。 |
| `--force` | 即使文件未变化，也强制重新向量化。 |

向量数据和索引清单会存储在统一的全局目录中（例如 Linux 上的 `~/.local/share/jcemb`）。

### `query`

查询统一向量库。

```bash
jcemb query <query-text> [flags]
```

常用参数：

| 参数 | 说明 |
|---|---|
| `--path` | 可选：限制到已索引的文件或目录。 |
| `-l, --limit` | 最大返回结果数。默认：`10`。 |
| `-t, --file-type` | 查询的文件类型。默认：`markdown`；图片搜索使用 `image`。 |
| `--tags` | 必须匹配的标签。多个标签使用 AND 语义。 |
| `-u, --unique` | 按 Markdown 文件去重。 |
| `--full` | 显示完整 chunk 内容，而不是预览片段。 |
| `--json` | 以 JSON 输出最终结果。 |
| `--search-window` | 降噪前的候选窗口大小。`0` 表示自动。 |
| `--threshold-alpha` | 相对 top1 分数的过滤阈值。负数表示禁用。 |
| `--threshold-delta` | 与 top1 分数差距的过滤阈值。负数表示禁用。 |
| `--mmr-lambda` | MMR 相关性/多样性权重。`1.0` 表示禁用 MMR。 |

省略 `--path` 时，`query` 会搜索全局存储中的所有已索引集合。指定
`--path` 时，它可以指向文件或目录；`jcemb` 会解析集合身份，并按相对路径前缀过滤结果，目录路径会包含其所有后代文件。

### `show`

显示指定文件在向量库中的信息：标签、向量长度、集合 ID、provider、model 以及 chunk 详情。

```bash
jcemb show <file-path> [flags]
```

| 参数 | 说明 |
|---|---|
| `--json` | 以 JSON 输出结果。 |

如果文件不在任何已索引的集合中，会显示"未找到"（JSON 模式下输出 `{"found": false}`）。

### `config`

交互式编辑持久化的 `jcemb` 配置。

```bash
jcemb config
```

该命令允许您配置默认的 embedding provider、model 以及其他设置。它需要一个交互式终端。

### `version`

输出 `jcemb` 版本号。

```bash
jcemb version
```

## 标签

标签只从 YAML front matter 中读取：

```markdown
---
tags:
  - architecture
  - gateway
---

# Gateway Notes
```

按标签查询：

```bash
jcemb query "callback flow" --path /path/to/docs --tags architecture,gateway
```

结果必须同时包含所有指定标签。

## 工作原理

1. `scan` 按注册的文件扩展名扫描 Markdown 和图片等文件，并跳过 `.git`、`node_modules` 等目录。
2. Markdown 文档按结构切分为 chunks；图片生成一个描述记录。
3. Markdown chunk 通过配置的 provider/model 生成向量；图片默认用 OpenCLIP 生成向量，也可配置为 Jina CLIP v2，或通过 OpenAI vision 描述再用 OpenAI text embedding 生成向量。
4. 向量记录和版本化索引清单按根目录和文件类型存储在统一的全局存储目录中。
5. `query` 使用已存储的文件类型配置向量化查询输入；`--file-type image` 下，已存在的图片路径会作为图搜图输入。
6. 查询结果经过排序、阈值过滤、可选去重、MMR 多样性排序后，输出为文本或 JSON。

不再创建本地 `.vectordb` 目录。如果在查询过程中检测到旧版 `.vectordb`，`jcemb` 将引导您将该路径重新向量化到统一存储中。

## 开发

运行单元测试：

```bash
go test ./...
```

运行覆盖率测试：

```bash
go test -cover ./...
```

运行集成测试：

```bash
INTEGRATION=1 go test -tags=integration ./...
```

集成测试需要本地运行 Ollama，并准备好对应模型。


## 许可证

`jcemb` 使用 MIT License 发布。详见 [LICENSE](LICENSE)。

# jcemb

`jcemb` 是一个本地优先的命令行工具，用于将 Markdown 文档向量化到本地向量库，并通过语义搜索进行检索。

它适合个人知识库、项目文档、技术笔记和其他以 Markdown 为主的资料集合。默认情况下，它使用本地 Ollama 服务生成向量，不需要额外运行独立的向量数据库服务。

## 功能特性

- 仅索引 Markdown 文档。
- 向量数据存储在 `<root>/.vectordb/`。
- 默认 embedding provider：Ollama。
- 默认 embedding model：`bge-m3`。
- 支持递归扫描目录。
- 基于文件 hash 和 recipe hash 做增量向量化。
- 下一次 embed 时自动清理已删除或重命名的文件。
- 从 YAML front matter 中提取标签。
- 标签过滤使用 AND 语义。
- 支持文本输出和 JSON 输出。
- 查询结果内置降噪流程：阈值过滤、去重和 MMR 多样性排序。

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

查看命令帮助：

```bash
./jcemb --help
```

## 快速开始

递归向量化一个 Markdown 文档目录：

```bash
jcemb embed /path/to/docs -r
```

查询已向量化的文档：

```bash
jcemb query "how do I configure the gateway?" --path /path/to/docs -l 10
```

输出 JSON：

```bash
jcemb query "deployment checklist" --path /path/to/docs --json
```

强制完整重建：

```bash
jcemb embed /path/to/docs -r --force
```

## 命令

### `embed`

将 Markdown 文件写入本地向量库。

```bash
jcemb embed [path] [flags]
```

常用参数：

| 参数 | 说明 |
|---|---|
| `-r, --recursive` | 递归扫描子目录。 |
| `-p, --provider` | Embedding provider。默认：`ollama`。 |
| `-m, --model` | Embedding model。默认：`bge-m3`。 |
| `-c, --concurccy` | 并发 worker 数量。 |
| `--force` | 即使文件未变化，也强制重新向量化。 |
| `-t, --type` | 文档类型。目前仅支持 `md`。 |

向量数据和索引清单会写入：

```text
<path>/.vectordb/
```

### `query`

查询已有的本地向量库。

```bash
jcemb query <query-text> [flags]
```

常用参数：

| 参数 | 说明 |
|---|---|
| `--path` | 用于向上查找最近 `.vectordb` 的文件或目录。 |
| `-l, --limit` | 最大返回结果数。默认：`10`。 |
| `-t, --tags` | 必须匹配的标签。多个标签使用 AND 语义。 |
| `-u, --unique` | 按 Markdown 文件去重。 |
| `--full` | 显示完整 chunk 内容，而不是预览片段。 |
| `--json` | 以 JSON 输出最终结果。 |
| `--search-window` | 降噪前的候选窗口大小。`0` 表示自动。 |
| `--threshold-alpha` | 相对 top1 分数的过滤阈值。负数表示禁用。 |
| `--threshold-delta` | 与 top1 分数差距的过滤阈值。负数表示禁用。 |
| `--mmr-lambda` | MMR 相关性/多样性权重。`1.0` 表示禁用 MMR。 |

`query --path` 可以指向文件或目录。`jcemb` 会从该路径向上查找最近的 `.vectordb`，然后按相对路径前缀过滤结果。

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

1. `embed` 扫描 Markdown 文件，并跳过 `.vectordb`、`.git`、`node_modules` 等目录。
2. 文档按 Markdown 结构切分为 chunks。
3. 通过配置的 provider 和 model 生成 chunk 向量。
4. 向量记录和版本化索引清单存储在 `.vectordb` 下。
5. `query` 使用索引中记录的 provider/model 配置向量化查询文本。
6. 查询结果经过排序、阈值过滤、可选去重、MMR 多样性排序后，输出为文本或 JSON。

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

## 发布

推送版本 tag 后，GitHub Actions 会自动构建 release 包：

```bash
git tag v0.1.0
git push origin v0.1.0
```

Release workflow 会发布 macOS、Linux、Windows 的 `amd64` 和 `arm64` 二进制包，并附带 `SHA256SUMS` 文件。

## 扩展

Provider、splitter 和 vector store 通过包初始化函数自注册：

```go
import "github.com/bspiritxp/jcemb/internal/registry"

func init() {
    registry.MustRegisterProvider("myprovider", factory)
}
```

重复注册会主动 panic。测试中可以使用 `internal/registry` 中对应的 reset helper 清理注册表。

## 许可证

`jcemb` 使用 MIT License 发布。详见 [LICENSE](LICENSE)。

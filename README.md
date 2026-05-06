# jcemb

[中文文档](README.zh-CN.md)

`jcemb` is a local-first command line tool for embedding Markdown documents into
a local vector store and searching them with semantic queries.

It is designed for small personal knowledge bases, project documentation, notes,
and other Markdown-first collections where you want simple local retrieval
without running a separate vector database service.

## Features

- Markdown and image ingestion through scan providers.
- Unified global storage for configuration and vector data.
- Default embedding provider: Ollama.
- Default embedding model: `bge-m3`.
- Recursive directory scanning.
- Incremental rescanning based on file and recipe hashes.
- Cleanup of deleted or renamed files on the next scan.
- YAML front matter tag extraction.
- Tag filtering with AND semantics.
- Text and JSON query output.
- Built-in result denoising with thresholding, deduplication, and MMR.
- Interactive configuration management.

## Requirements

- Go 1.26.2 or newer.
- Ollama running locally at `http://localhost:11434`.
- The `bge-m3` model available in Ollama.
- For image vectors, Python 3 plus model backend packages:
  - default OpenCLIP: `open_clip_torch`, `torch`, `pillow`
  - optional Jina CLIP v2: `transformers`, `torch`, `pillow`, `einops`, `timm`
- For image metadata, an Ollama vision model such as `llava`.

Install and prepare Ollama:

```bash
ollama pull bge-m3
ollama pull llava
ollama serve
```

If Ollama is already running as a background service, only the model pull is
needed.

## Installation

### Install with Homebrew

```bash
brew tap bspiritxp/tap
brew install jcemb
```

### Install with Scoop

```powershell
scoop bucket add bspiritxp https://github.com/bspiritxp/scoop-bucket
scoop install jcemb
```

### Install with Go

```bash
go install github.com/bspiritxp/jcemb@latest
```

Make sure your Go binary directory is on `PATH`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Build from source

```bash
git clone https://github.com/bspiritxp/jcemb.git
cd jcemb
go build -o jcemb .
```

On Windows, build the executable with the `.exe` suffix so PowerShell runs the
fresh binary instead of an older `jcemb.exe` found through normal executable
resolution:

```powershell
go build -o jcemb.exe .
```

Run the local binary:

```bash
./jcemb --help
```

## Quick Start

Scan a directory of Markdown files:

```bash
jcemb scan /path/to/docs -r
```

Query the embedded documents:

```bash
jcemb query "how do I configure the gateway?" -l 10
```

Return JSON output:

```bash
jcemb query "deployment checklist" --json
```

Force a full rebuild:

```bash
jcemb scan /path/to/docs -r --force
```

## Commands

### `scan`

Scan registered file types into the unified vector store. Markdown and images are built in.

```bash
jcemb scan [path] [flags]
```

Common flags:

| Flag | Description |
|---|---|
| `-r, --recursive` | Scan subdirectories recursively. |
| `-p, --provider` | Embedding provider. Default: from config. |
| `-m, --model` | Embedding model. Default: from config. |
| `-c, --concurccy` | Number of concurrent workers. |
| `--force` | Rescan all documents even if unchanged. |

The vector data and manifests are stored in a unified global directory (e.g., `~/.local/share/jcemb` on Linux).

### `query`

Search the unified vector store.

```bash
jcemb query <query-text> [flags]
```

Common flags:

| Flag | Description |
|---|---|
| `--path` | Optional indexed file or directory path to restrict results. |
| `-l, --limit` | Maximum number of results. Default: `10`. |
| `-t, --file-type` | File type to query. Default: `markdown`; use `image` for image search. |
| `--tags` | Required tags filter. Multiple tags use AND semantics. |
| `-u, --unique` | Deduplicate results by Markdown file. |
| `--full` | Show full chunk content instead of a preview. |
| `--json` | Output the final result list as JSON. |
| `--search-window` | Candidate window before denoising. `0` means auto. |
| `--threshold-alpha` | Relative-to-top result cutoff. Negative disables it. |
| `--threshold-delta` | Absolute gap-from-top result cutoff. Negative disables it. |
| `--mmr-lambda` | MMR relevance/diversity weight. `1.0` disables MMR. |

When `--path` is omitted, `query` searches all indexed collections in the
global store. When `--path` points to a file or directory, `jcemb` resolves the
collection identity and filters results by the relative path prefix; directory
paths include all descendants.

### `show`

Show vector store information for a specific file: tags, vector length, collection ID, provider, model, and chunk details.

```bash
jcemb show <file-path> [flags]
```

| Flag | Description |
|---|---|
| `--json` | Output as JSON. |

When the file is not found in any indexed collection, it prints a "not found" message (or `{"found": false}` in JSON mode).

### `config`

Interactively edit the persisted `jcemb` configuration.

```bash
jcemb config
```

This command allows you to configure the default embedding provider, model, and
other settings. It requires an interactive terminal.

Image model settings can also be placed in the config file:

```json
{
  "image": {
    "provider": "openclip",
    "model": "ViT-B-32",
    "pretrained": "laion2b_s34b_b79k",
    "dimensions": 512,
    "device": "auto",
    "python": "python3",
    "vision_model": "llava"
  }
}
```

Use `"provider": "jina-clip"` and `"model": "jinaai/jina-clip-v2"` to switch
image vectors to Jina CLIP v2.

OpenAI can be used for text embeddings, and for image search via vision
description plus text embeddings. The recommended default embedding model is
`text-embedding-3-small` with 1536 dimensions. `text-embedding-3-large` is the
higher-quality option when cost is less important.

```json
{
  "provider": "openai",
  "model": "text-embedding-3-small",
  "vector_dim": 1536,
  "openai": {
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-...",
    "batch_size": 128,
    "timeout": "60s",
    "dimensions": 1536
  },
  "image": {
    "provider": "openai",
    "model": "text-embedding-3-small",
    "dimensions": 1536,
    "vision_model": "gpt-4.1-mini"
  }
}
```

`OPENAI_API_KEY` and `OPENAI_BASE_URL` can be used instead of storing credentials
in the config file. OpenAI embedding models are text-only, so image vectors are
generated from an OpenAI vision description of the image.

### `version`

Print the `jcemb` version.

```bash
jcemb version
```

## Tags

Tags are read only from YAML front matter:

```markdown
---
tags:
  - architecture
  - gateway
---

# Gateway Notes
```

Filter by tags:

```bash
jcemb query "callback flow" --path /path/to/docs --tags architecture,gateway
```

All requested tags must be present on the matched document.

## How It Works

1. `scan` discovers registered file extensions and skips ignored directories
   such as `.git` and `node_modules`.
2. Markdown files are split by Markdown structure; image files are represented
   by one generated description record.
3. Markdown chunks are embedded through the configured provider/model. Images
   use OpenCLIP by default for vectors, optionally Jina CLIP v2, and Ollama
   vision only for metadata.
4. Vector records and versioned manifests are stored by root and file type in a unified global
   storage directory.
5. `query` embeds the search input with the stored file-type config. For
   `--file-type image`, an existing image path is treated as image-to-image search.
6. Results are sorted, thresholded, optionally deduplicated, diversified with
   MMR, and then rendered as text or JSON.

Legacy `.vectordb` directories are no longer created. If a legacy `.vectordb` is
detected during a query, `jcemb` will guide you to rescan the path into the
unified storage.

## Development

Run unit tests:

```bash
go test ./...
```

Run tests with coverage:

```bash
go test -cover ./...
```

Run integration tests:

```bash
INTEGRATION=1 go test -tags=integration ./...
```

Integration tests require a running Ollama instance and the configured model.


## License

`jcemb` is released under the MIT License. See [LICENSE](LICENSE).

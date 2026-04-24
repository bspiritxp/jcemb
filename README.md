# jcemb

`jcemb` is a local-first command line tool for embedding Markdown documents into
a local vector store and searching them with semantic queries.

It is designed for small personal knowledge bases, project documentation, notes,
and other Markdown-first collections where you want simple local retrieval
without running a separate vector database service.

## Features

- Markdown-only document ingestion.
- Local vector store under `<root>/.vectordb/`.
- Default embedding provider: Ollama.
- Default embedding model: `bge-m3`.
- Recursive directory embedding.
- Incremental re-embedding based on file and recipe hashes.
- Cleanup of deleted or renamed files on the next embed.
- YAML front matter tag extraction.
- Tag filtering with AND semantics.
- Text and JSON query output.
- Built-in result denoising with thresholding, deduplication, and MMR.

## Requirements

- Go 1.26.2 or newer.
- Ollama running locally at `http://localhost:11434`.
- The `bge-m3` model available in Ollama.

Install and prepare Ollama:

```bash
ollama pull bge-m3
ollama serve
```

If Ollama is already running as a background service, only the model pull is
needed.

## Installation

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

Run the local binary:

```bash
./jcemb --help
```

## Quick Start

Embed a directory of Markdown files:

```bash
jcemb embed /path/to/docs -r
```

Query the embedded documents:

```bash
jcemb query "how do I configure the gateway?" --path /path/to/docs -l 10
```

Return JSON output:

```bash
jcemb query "deployment checklist" --path /path/to/docs --json
```

Force a full rebuild:

```bash
jcemb embed /path/to/docs -r --force
```

## Commands

### `embed`

Embed Markdown files into a local vector store.

```bash
jcemb embed [path] [flags]
```

Common flags:

| Flag | Description |
|---|---|
| `-r, --recursive` | Scan subdirectories recursively. |
| `-p, --provider` | Embedding provider. Default: `ollama`. |
| `-m, --model` | Embedding model. Default: `bge-m3`. |
| `-c, --concurccy` | Number of concurrent workers. |
| `--force` | Re-embed all documents even if unchanged. |
| `-t, --type` | Document type. Currently only `md` is supported. |

The vector data and manifests are written to:

```text
<path>/.vectordb/
```

### `query`

Search an existing local vector store.

```bash
jcemb query <query-text> [flags]
```

Common flags:

| Flag | Description |
|---|---|
| `--path` | File or directory used to find the nearest `.vectordb`. |
| `-l, --limit` | Maximum number of results. Default: `10`. |
| `-t, --tags` | Required tags. Multiple tags use AND semantics. |
| `-u, --unique` | Deduplicate results by Markdown file. |
| `--full` | Show full chunk content instead of a preview. |
| `--json` | Output the final result list as JSON. |
| `--search-window` | Candidate window before denoising. `0` means auto. |
| `--threshold-alpha` | Relative-to-top result cutoff. Negative disables it. |
| `--threshold-delta` | Absolute gap-from-top result cutoff. Negative disables it. |
| `--mmr-lambda` | MMR relevance/diversity weight. `1.0` disables MMR. |

`query --path` can point to a file or a directory. `jcemb` walks upward to find
the nearest `.vectordb`, then filters results by the relative path prefix.

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

1. `embed` scans Markdown files and skips ignored directories such as
   `.vectordb`, `.git`, and `node_modules`.
2. Documents are split by Markdown structure.
3. Chunks are embedded through the configured provider and model.
4. Vector records and versioned manifests are stored locally under `.vectordb`.
5. `query` embeds the search text with the stored provider/model config.
6. Results are sorted, thresholded, optionally deduplicated, diversified with
   MMR, and then rendered as text or JSON.

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

## Releases

GitHub Actions builds release archives automatically when a version tag is
pushed:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow publishes macOS, Linux, and Windows binaries for `amd64`
and `arm64`, plus a `SHA256SUMS` file.

## Extending

Providers, splitters, and vector stores are registered through package
initializers:

```go
import "github.com/bspiritxp/jcemb/internal/registry"

func init() {
    registry.MustRegisterProvider("myprovider", factory)
}
```

Duplicate registrations panic by design. Tests can reset registries with the
corresponding reset helpers in `internal/registry`.

## License

`jcemb` is released under the MIT License. See [LICENSE](LICENSE).

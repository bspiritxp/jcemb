# AGENTS.md — jcemb

> Compact guidance for OpenCode sessions working on this Go CLI vector-search tool.

## What this repo is

A local-first Go CLI (`jcemb`) that embeds registered file types into a local vector store and queries them with semantic search.

- Built-in file types: Markdown (`.md`) and image (`.png`, `.jpg`, `.jpeg`, `.webp`, `.svg`, etc.).
- Default provider: `ollama`, default model: `bge-m3`.
- Output is stored in the configured global data directory (JSON index + records file, not a real LanceDB server).
- Markdown tags come **only** from YAML front matter. Image tags come from the image scan provider metadata.
- Semantic tags are separate from hard filter tags. They feed `TagVector` and optional query-time tag/content fusion.

## Build & run

```bash
# Compile binary
go build -o jcemb .

# Scan a directory recursively
./jcemb scan /path/to/docs -r

# Query globally across indexed markdown collections
./jcemb query "search text" -l 10

# Query images with text, or use an image path for image-to-image search
./jcemb query "red bicycle" --file-type image
./jcemb query ./query.png --file-type image

# Show vector store info for a specific file
./jcemb show /path/to/docs/readme.md
./jcemb show /path/to/image.png --json

# JSON output
./jcemb query "search text" --path /path/to/docs --json
```

## Test

```bash
# Unit tests only — no external services required
go test ./...

# With coverage
go test -cover ./...

# Integration tests (require external Ollama + INTEGRATION=1)
go test -tags=integration ./...
```

Integration tests are gated by both **build tag** (`//go:build integration`) and **env var** (`INTEGRATION=1`). See `internal/testkit/integration_gate_test.go`.

## Release workflow

GitHub Actions builds release archives automatically when a version tag is
pushed:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow publishes macOS, Linux, and Windows binaries for `amd64`
and `arm64`, plus a `SHA256SUMS` file.

It also updates package manager repositories:

- Homebrew tap: `bspiritxp/homebrew-tap`
- Scoop bucket: `bspiritxp/scoop-bucket`

The workflow needs a `PACKAGE_REPO_TOKEN` repository secret with write access to
both package repositories.

## Architecture

| Package | Responsibility |
|---|---|
| `main.go` | Minimal entrypoint; only calls `cmd.Execute()` |
| `cmd/` | Cobra command layer: flag parsing, thin dispatch. No business logic. |
| `internal/app/` | Service orchestration (`scan`, `query`, `show`) |
| `internal/domain/` | Core contracts: `Document`, `Chunk`, `VectorStore`, `Embedder`, etc. |
| `internal/registry/` | Self-registration factories for provider / splitter / vector store / scan provider / tag extractor |
| `internal/provider/ollama/` | Default embedding provider (HTTP to local Ollama) |
| `internal/scanprovider/` | File-type scan providers (`markdown`, `image`) |
| `internal/splitter/markdown/` | Markdown structural splitter (headings → chunks) |
| `internal/tagextractor/` | Semantic tag extraction helpers and provider implementations used at scan time and query time |
| `internal/storage/lancedb/` | Local vector store adapter (JSON file, not real LanceDB) |
| `internal/index/` | Versioned atomic JSON index (`config.json` + `index.json`) |
| `internal/fs/` | File discovery; skips `.git`, `node_modules`, IDE/tool dirs, and OS recycle-bin / system metadata dirs (case-insensitive) — see `defaultIgnoredDirectoryNames` |
| `internal/metadata/` | YAML front matter extraction |
| `internal/output/` | Text / JSON query result rendering |
| `internal/testkit/` | Fake store, integration gate, test helpers |

## Registry & extension

New providers, splitters, vector stores, scan providers, and tag extractors are added via `init()` self-registration:

```go
import "github.com/bspiritxp/jcemb/internal/registry"

func init() {
    registry.MustRegisterProvider("myprovider", factory)
    registry.MustRegisterTagExtractor("mytags", tagFactory)
}
```

- Duplicate registration **panics**.
- Tests can reset with `registry.ResetProviders()`, `registry.ResetSplitters()`, `registry.ResetVectorStores()`, `registry.ResetTagExtractors()`.

## Key conventions

- **Incrementality**: scan skips unchanged files by comparing `file_hash + recipe_hash`. Use `--force` to rebuild all.
- **Reconcile**: deleted/renamed files are cleaned from both index and vector store on the next scan.
- **Tag filter**: `--tags a,b` means **AND** semantics (result must contain both tags).
- **Tag fusion**: semantic tags are embedded separately from chunk content. `query --tag-weight` controls score blending, `query --no-tag` forces content-only ranking, and `--tags` still filters before fusion.
- **Fallbacks**: query-time tag extraction is skipped for image queries and short text queries under 10 runes, and any extraction failure falls back to content-only ranking.
- **File type filter**: `query --file-type/-t` defaults to `markdown`; use `image` for text-to-image and image-to-image search.
- **Path filter**: `query --path` accepts a file or directory and filters by the relative prefix; directory paths include descendants. Omit `--path` for global search across all indexed collections.
- **Chunk ID stability**: derived from `rel_path + recipe_hash + chunk_index (+ section fingerprint)` so re-embeds are deterministic.
- **Concurrency flag typo**: the CLI flag is `--concurccy` (matches original spec), but internal fields use `concurrency`.

## Query denoise

- Query pipeline: `store window → sort → threshold → unique → MMR → final limit`.
- Defaults: `searchWindow=max(20, 5*limit)`, `thresholdAlpha=0.85`, `thresholdDelta=0.10`, `mmrLambda=0.5`.

| Flag | Meaning |
|---|---|
| `--threshold-alpha` | Relative-to-top1 cutoff; negative disables threshold-alpha rule |
| `--threshold-delta` | Absolute gap-from-top1 cutoff; negative disables threshold-delta rule |
| `--mmr-lambda` | MMR weight; `1.0` disables MMR and preserves score order |
| `--search-window` | Retrieval candidate window; `0` means auto window |

- `--unique` deduplicates by file `rel_path` after thresholding and before MMR.
- `--json` receives the same final result list as text output; these stages do not change the JSON schema, and previews are still truncated.

## External runtime dependencies

- **Ollama** must be running locally at `http://localhost:11434` (default).
- **bge-m3** model must be available (`ollama pull bge-m3`).
- Semantic tag extraction needs a configured tag extractor model when enabled, for example Ollama `qwen2.5:7b` or an OpenAI chat model.
- Image metadata requires an Ollama vision model (default provider option `vision_model` falls back to `llava`).
- Image vectors default to OpenCLIP (`ViT-B-32` + `laion2b_s34b_b79k`, 512 dimensions) through a Python backend. `jina-clip` with `jinaai/jina-clip-v2` is supported by config.
- OpenAI provider uses `/v1/embeddings`; recommended default is `text-embedding-3-small` (1536 dimensions). Image provider `openai` uses vision description + text embedding because OpenAI embedding models are text-only.
- Default vector dimension for bge-m3 is **1024**.

## Files an agent should know

- `go.mod` — Go 1.26.2, uses `cobra`, `testify`, `gomarkdown`, `yaml.v3`.
- `internal/config/defaults.go` — Default config values (Ollama URL, batch size, timeout).
- `.sisyphus/plans/go-cli-vector-tool.md` — Full original implementation plan.

## Gotchas

- `query` does **not** accept `--provider` or `--model` overrides. It reads those from the stored collection config.
- `lancedb` here is a **local JSON-file adapter**, not the real LanceDB server/SDK.
- `scan` discovers registered extensions automatically; do not add new user-facing scan file-type flags.
- Do not put business logic in `cmd/` or `main.go`; keep commands thin.
- Scanner always skips a built-in list of directory names regardless of `.gitignore`: `.git`, `node_modules`, IDE dirs (`.idea`, `.vscode`, `.claude`, `.codex`, `.obsidian`), Windows recycle/system dirs (`$RECYCLE.BIN`, `RECYCLER`, `System Volume Information`), and macOS/Linux trash + metadata dirs (`.Trashes`, `.Trash`, `.Trash-<uid>`, `.fseventsd`, `.Spotlight-V100`, `.DocumentRevisions-V100`, `.TemporaryItems`). Matching is case-insensitive. `.gitignore` rules apply on top of this list, never replace it.

# AGENTS.md

## Project overview
- Go CLI (`module github.com/sacquatella/tomd`, `go 1.23.4`) that converts HTML/PDF/DOCX/PPTX inputs to Markdown with YAML-like metadata front matter.
- Entrypoint: `main.go` -> `cmd.Execute()` (Cobra). Command definitions live in `cmd/`; shared conversion/export/metadata/IA logic in `tools/`; DOCX/PPTX-specific parsing/conversion in `docx2md/` (based on `mattn/docx2md`, see `docx2md/readme.md`).

## Build, test, release
- Build (current platform): `go mod tidy && go build` (README) or `make build-linux-amd|build-linux-arm|build-osx-arm|build-windows` (targets run `go mod tidy` and set `GOOS/GOARCH`; outputs to `bin/...`).
- Test: `make test` or `go test -v ./...`. Tests exist in `tools/*_test.go` and `docx2md/*_test.go`.
- Clean: `make clean`.
- Makefile injects version metadata via ldflags: `-X cmd.Version=${git describe --tags} -X cmd.Build=${git log -1 --format=%H}`.
- Release: `.github/workflows/release.yaml` builds cross-platform binaries on GitHub release creation (`wangyoucao577/go-release-action`) with `-X github.com/sacquatella/tomd/cmd.Version=$GITHUB_REF_NAME`.

## CLI/behavior notes (trust code over README examples)
- Root persistent flags (`cmd/root.go`): `-d/--dir` export directory (default `.`), `-v/--verbose` writes debug logs to `log-tomd.log`, `--ai` (aliases `-i/--ia`) enables image description.
- Subcommand input flags differ by format: `page`/`file` use `-u/--url`/`-f/--file` respectively plus `-c/--cid` and `-k/--unsecure`; `pdf` uses `-p/--pdf`, `docx` uses `-x/--docx`, `pptx` uses `-s/--pptx` (each also takes `-u/--url`, `-c/--cid`). Prefer `tomd <command> --help`/`cmd/*.go` when documenting or changing flags; README examples are not fully consistent with actual flags.
- `file` command reads a JSON list of page/metadata overrides (`site_url`, `title`, `description`, `tags`, `authors`, etc.); see `samples/pages*.json` for shape.
- `docs <directory>` converts local HTML/HTM/PDF/DOCX/PPTX files, with `-r/--recursive` for subdirectories. It skips symlinks and nested export trees, preserves relative paths under `--dir` in per-source `<filename.ext>.tomd/` directories, and refuses existing destinations. Per-file failures do not stop the batch but produce a nonzero exit status. It inherits `--ia` and supports `--cid`, `--language` (PDF/PPTX), `--no-notes` and `--exclude-hidden` (PPTX). DOCX export assets are relative to the Markdown; local HTML images are not copied/rebased.
- `docs s3://bucket[/prefix]` exports S3 objects locally using `tools/s3.go` and AWS SDK v2 (standard AWS credentials/config, region required). `--s3-endpoint`, `--s3-region`, `--s3-path-style` support compatible services. Prefixes are directory-like; `-r` includes nested keys, otherwise listing uses `/` as delimiter. Listing is paginated, downloads are temporary and sequential, metadata/table URLs retain `s3://` provenance. Unsafe portable path mappings and symlinked output ancestors are refused. `GetPageWithSource` separates HTML input from provenance; `GetPage` retains its existing API. HTML sidecar images are not downloaded. S3 tests use fake clients/local HTTP endpoints, never live buckets.
- PDF export uses `tools/pdf.go`, `pdf_layout.go` and `pdf_images.go`: font/position-based heading and paragraph heuristics, two-column ordering, and pdfcpu raster extraction with relative content-hashed assets. Page markers are HTML comments. `--ia` describes extracted raster images with page context; `--language` sets the description language. `GetPDFWithOptions` accepts the shared image describer; `GetPDF` and `ExtractTextFromPDF` remain AI-free. No OCR or vector rendering is performed.
- PDF JPEG 2000 streams are retained as `.jpx` assets with ordinary format-labeled links, without PNG conversion or mask composition. They do not block text extraction and are skipped with warnings for AI descriptions.
- PPTX conversion lives in `docx2md/pptx.go`, independently of the DOCX walker. It emits slide/block provenance, notes, native diagram/chart data and relative image assets. Hidden slides and notes are included by default; `--exclude-hidden` and `--no-notes` omit them. `--language` sets the AI description language. No external renderer or new Go dependency is used.
- DOCX conversion uses `docx2md/docx2md.go`, `docx_options.go` and `docx_images.go`: styles/outline-based headings, paragraphs, lists, tables and relative content-hashed images (DrawingML/VML). `GetDocxWithOptions`/`Docx2mdWithOptions` accept the shared image describer; existing APIs remain AI-free. `--ai --language French` describes supported raster images with neighboring text; failures warn and retain native content. No vector/SmartArt rendering or external image fetching is performed.
- Optional image description (`--ai`/`-i`, HTML/PDF/DOCX/PPTX) uses `tools/ia.go`: `TOMD_AI_PROVIDER=ollama|openai` (default Ollama), `TOMD_MODEL` (Ollama default `llava:7b`; required for OpenAI), `OPENAI_API_KEY`, `OPENAI_BASE_URL` and `OLLAMA_HOST`. Native extraction needs no service. AI tests use local HTTP mocks; `make dependencies` still pulls the default Ollama model.

## Repo conventions
- Preserve existing file/package split: Cobra wiring in `cmd/`, reusable logic in `tools/`, DOCX/PPTX conversion internals in `docx2md/`.
- Sample inputs/fixtures for manual checks live in `samples/` (JSON lists, test documents). Generated/exported artifacts should not be committed unless intentionally updating fixtures.
- Keep guidance in this file high-signal and verified against code/config; update it when commands, flags, or package boundaries change.

# toMD

Simple golang cli to convert various documents to markdown format. It first version support :

- HTML page 
- PDF file
- Docx file
- Pptx file

The CLI can convert a single file or a list of files. HTML and PPTX image descriptions can optionally use [Ollama](https://ollama.com) or OpenAI. All local processing is Go: no LibreOffice, Python, external executable or additional Go dependency is needed.

The conversion generates metadata header with the following fields :

```markdown
---
title: <title>
doc_id: <id>
description: <description>
tags: 
- file
site_url: <doc-url>
authors: 
- me
creation_date: 2025-01-13T18:15:24
last_update_date: 2025-01-13T18:15:24
visibility: Internal
```

These metadata's fields are generated from document information and can be overriden with a json file.


## Usage

Get a web page as a markdown file

```shell
$ tomd page -f <file-url> -d <directory>
```

Get a set of web pages as markdown files (with metadata override)

```shell
$ tomd file -f <json-list> -d <directory>
```

with :
```json
[
  {"site_url":"http://mywebsitepage.mydomain.com/page2", "description":"my page description","title":"","tags":["tag1","tag2"]},
  {"site_url":"https://mywebsitepage.mydomain.com/page4", "description":"my page description","title":""},
  {"site_url":"https://mywebsitepage.mydomain.com/page10", "description":"my page description ","title":"Page 10, xxxxx", "authors": ["author1","author2"]}
]
```

Extract PDF text as markdown file (basic text extraction)
```shell
$ tomd pdf -f <pdf-file> -d <directory>
```

Extract DOCX text as markdown file (basic text extraction)
```shell
$ tomd docx -d <docx-file> -d <directory>
```

Extract PPTX content as RAG-ready Markdown
```shell
$ tomd pptx -s presentation.pptx -d output
```

### PPTX for RAG

- Slides follow the presentation's actual order, including reordered or nonconsecutive slide parts.
- Each slide contains its source filename, presentation number and package-part ID, plus an anchor. Block IDs are emitted as HTML comments (`<!-- block: slide1-3 -->`). Keep these when your indexer needs block-level provenance; retain slide source information when chunking.
- Titles, nested lists, links, tables and presenter notes are extracted. Hidden slides are included and labeled by default. Use `--no-notes` or `--exclude-hidden` to omit them; source slide numbering is preserved.
- Footer, date and slide-number placeholders are omitted. Ordinary repeated text is not deleted heuristically, to avoid losing meaningful content.
- Images are exported under `<markdown-stem>-assets/` alongside the Markdown, with relative links and content-hashed filenames. External image relationships are not fetched.
- Native shapes retain their text and groups. Explicit connectors are extracted when their endpoints are identified; arrowheads determine direction, not shape positions. Charts expose available cached categories/values and number formats. SmartArt exposes readable labels and explicit data-model relationships, not inferred visual arrows.
- `--ia` adds clearly labeled AI-generated image descriptions with surrounding slide/notes context. `--language French` selects a description language; by default the provider is asked to use the source language. Original text is never replaced by an AI summary.
- AI failures issue warnings and retain native extraction. Descriptions are cached in memory for the duration of a conversion (image, language and context must match). Requests have a 90-second total deadline and at most two retries for HTTP 429/5xx.

Limitations: there is no complete PowerPoint renderer or OCR engine. Native diagrams are analyzed structurally, not sent as screenshots. Raster images (PNG/JPEG/GIF/WebP) can be described by a compatible vision model; SVG/EMF/WMF remain exported but are not rasterized. Shape order follows XML order, which may differ from visual reading order. Linked workbooks, animations, embedded objects, arbitrary inherited layout content and complex chart layouts are not rendered/evaluated. Unsupported graphic objects and missing data produce warnings. AI descriptions can be inaccurate and should be treated as generated evidence, not source facts.

## Options 

```shell
Export web pages to markdown files, create metadata header and optionally use llm to describe image in markdown file

Usage:
  tomd [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  docx        Get Docx text content as a markdown file
  file        Get a list of web pages as markdown files
  help        Help about any command
  page        Get a web page as a markdown file
  pdf         Get PDF text content as a markdown file
  pptx        Get pptx text content as a markdown file
  version     Provide tomd version and build number

Flags:
  -d, --dir string   Export page(s) folder, default is current folder (default ".")
  -h, --help         help for tomd
  -i, --ia           Use IA for image description
  -v, --verbose      write debug logs in log-tomd.log file

Use "tomd [command] --help" for more information about a command.
```

### Optional AI providers (HTML and PPTX)

No AI request is made without `--ia`. Ollama remains the default provider, with `llava:7b` as its default model. The Ollama service must be reachable and have the selected model installed; it is not required for native extraction.

```shell
TOMD_AI_PROVIDER=ollama TOMD_MODEL=llama3.2-vision:latest \
  tomd pptx -s presentation.pptx -d output --ia --language French
```

For OpenAI, explicitly select the provider, supply a key and choose an image-capable model available to your account:

```shell
export OPENAI_API_KEY="your-api-key"
export TOMD_AI_PROVIDER="openai"
export TOMD_MODEL="your-vision-capable-model"
tomd pptx -s presentation.pptx -d output --ia --language French
```

`OLLAMA_HOST` overrides the Ollama endpoint (default `http://127.0.0.1:11434`). `OPENAI_BASE_URL` overrides the OpenAI API base (default `https://api.openai.com/v1`); the client uses `/chat/completions`. Credentials are not sent to Ollama, included in Markdown or logged. Provider error bodies are not logged. Enabling OpenAI uploads image content and surrounding source context to that service and may incur charges; use Ollama for local-only descriptions. No automatic fallback between providers occurs.

Tests run offline with synthetic PPTX archives and simulated HTTP providers: `go test ./...`.

## Build

```shell
$ go mod tidy
$ go build
```

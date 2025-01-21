# toMD

Simple golang cli to convert various documents to markdown format. It first version support :

- HTML page 
- PDF file
- Docx file
- Pptx file

The cli can convert a single file or a list of files. It can also use [ollama](https://ollama.com) to describe images in markdown file (only HTML for the moment).

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

Extract PPTX text as markdown file (basic text extraction)
```shell
$ tomd pptx -p <docx-file> -d <directory>
```

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

If `-i` option is used, `ollama` and llm `llava:7b` are used to describe images at the end of the markdown file.
So do not forget to install [ollama](https://ollama.com) and to pull `llava:7b` with `ollama pull llava:7b` before using this option.

You can use another model by setting `TOMD_MODEL` env variable with your target models.

```shell
$ export TOMD_MODEL=llama3.2-vision:latest  tomd page -f <file-url> -d <directory> -i
```

## Build

```shell
$ go mod tidy
$ go build
```

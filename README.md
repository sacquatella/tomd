# toMD

Simple golang cli to convert a web page or a set of web pages to markdown format.

## Usage

Get web page as a markdown file
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

Extract PDF text as markdown file
```shell
$ tomd pdf -f <pdf-file> -d <directory>
```

## Options 

```shell
Export web pages to markdown files, create metadata header and optionally use llm to describe image in markdown file

Usage:
  tomd [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  file        Get a list of web pages as markdown files
  help        Help about any command
  page        Get a web page as a markdown file
  pdf         Get PDF text content as a markdown file
  version     Provide tomd version and build number

Flags:
  -d, --dir string   Export page(s) folder, default is current folder (default ".")
  -h, --help         help for tomd
  -i, --ia           Use IA for image description
  -v, --verbose      write debug logs in log-tomd.log file

Use "tomd [command] --help" for more information about a command.
```

If `-i` option is use, `ollama` and llm `llava:7b` are used to describe images at the end of markdown file. 
So do not forget install [ollama](https://ollama.com) and to pull `llava:7b` with `ollama pull llava:7b` before using this option.
You can use an other model by setting TOMD_MODEL env variable with your target models.

```shell
$ export TOMD_MODEL=llama3.2-vision:latest  tomd page -f <file-url> -d <directory> -i
```



## Build

```shell
$ go mod tidy
$ go build
```

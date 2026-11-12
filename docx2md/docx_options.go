package docx2md

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sacquatella/tomd/tools"
)

// DocxOptions controls extraction. A nil Describer keeps conversion AI-free.
type DocxOptions struct {
	EmbedImages  bool
	AssetsDir    string
	AssetsPrefix string
	Language     string
	Describer    tools.ImageDescriber
	Context      context.Context
	// Warn receives recoverable errors; by default they are written to stderr.
	Warn func(string)
}

func (zf *file) warn(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if zf.opts.Warn != nil {
		zf.opts.Warn(message)
	} else {
		fmt.Fprintln(os.Stderr, "Warning: "+message)
	}
}

func readDocxPart(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("DOCX part %s exceeds size limit", f.Name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Name, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("DOCX part %s exceeds size limit", f.Name)
	}
	return b, nil
}

func openDocx(filename string, opts DocxOptions) (*file, *Node, tools.Metadata, error) {
	r, err := zip.OpenReader(filename)
	if err != nil {
		return nil, nil, tools.Metadata{}, err
	}
	zf := &file{r: r, opts: opts, embed: opts.EmbedImages, list: make(map[string]int)}
	// Close the archive on every failed initialization path.
	ok := false
	defer func() {
		if !ok {
			r.Close()
		}
	}()
	document := findFile(r.File, "word/document.xml")
	if document == nil {
		document = findFile(r.File, "word/document*.xml")
	}
	if document == nil {
		return nil, nil, tools.Metadata{}, fmt.Errorf("incorrect DOCX document: missing document part")
	}
	zf.owner = document.Name
	if zf.opts.Context == nil {
		zf.opts.Context = context.Background()
	}
	if zf.opts.AssetsDir == "" {
		zf.opts.AssetsDir = strings.TrimSuffix(filename, filepath.Ext(filename)) + "-assets"
	}
	if zf.opts.AssetsPrefix == "" {
		zf.opts.AssetsPrefix = filepath.Base(zf.opts.AssetsDir)
	}
	var prop CoreProperties
	parts := []struct {
		name string
		dest any
	}{
		{path.Join(path.Dir(document.Name), "_rels", path.Base(document.Name)+".rels"), &zf.rels},
		{"word/numbering.xml", &zf.num},
		{"docProps/core.xml", &prop},
	}
	for _, part := range parts {
		if f := findFile(r.File, part.name); f != nil {
			b, err := readDocxPart(f, 4<<20)
			if err != nil {
				return nil, nil, tools.Metadata{}, err
			}
			if err := xml.Unmarshal(b, part.dest); err != nil {
				return nil, nil, tools.Metadata{}, fmt.Errorf("parse %s: %w", part.name, err)
			}
		}
	}
	if styles := findFile(r.File, "word/styles.xml"); styles != nil {
		n, err := readFile(styles)
		if err != nil {
			return nil, nil, tools.Metadata{}, err
		}
		zf.loadHeadings(n)
	}
	node, err := readFile(document)
	if err != nil {
		return nil, nil, tools.Metadata{}, err
	}
	meta := tools.Metadata{Title: prop.Title, Description: prop.Description}
	if prop.Creator != "" {
		meta.Authors = []string{prop.Creator}
	}
	ok = true
	return zf, node, meta, nil
}

// Docx2mdWithOptions exports assets beside the input unless AssetsDir is set.
func Docx2mdWithOptions(filename string, opts DocxOptions) (string, tools.Metadata, error) {
	zf, node, meta, err := openDocx(filename, opts)
	if err != nil {
		return "", tools.Metadata{}, err
	}
	defer zf.r.Close()
	var buf bytes.Buffer
	if err := zf.walk(node, &buf); err != nil {
		return "", tools.Metadata{}, err
	}
	return buf.String(), meta, nil
}

// GetDocxWithOptions keeps images beside the exported Markdown with relative links.
func GetDocxWithOptions(filename, sourceURL, customerID, exportDir string, complements tools.Metadata, opts DocxOptions) (tools.Page, error) {
	zf, node, meta, err := openDocx(filename, opts)
	if err != nil {
		return tools.Page{}, err
	}
	defer zf.r.Close()
	header, metadata := tools.BuildFileMetadata(filename, sourceURL, customerID, meta, complements)
	exported := tools.BuildFilename(metadata.Title, exportDir, customerID)
	if err := os.MkdirAll(filepath.Dir(exported), 0755); err != nil {
		return tools.Page{}, err
	}
	zf.opts.AssetsDir = strings.TrimSuffix(exported, filepath.Ext(exported)) + "-assets"
	zf.opts.AssetsPrefix = filepath.Base(zf.opts.AssetsDir)
	var buf bytes.Buffer
	if err := zf.walk(node, &buf); err != nil {
		return tools.Page{}, err
	}
	if err := tools.WriteMarkdownToFile(header+buf.String(), exported); err != nil {
		return tools.Page{}, err
	}
	return tools.Page{PageId: metadata.Doc_id, Url: metadata.Site_url, MdFile: exported}, nil
}

func docxOn(n *Node) bool {
	if n == nil {
		return false
	}
	v := strings.ToLower(nodeAttr(n, "val"))
	return v != "0" && v != "false" && v != "off"
}

func docxTableCell(text string) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\n", "<br>")
	var result strings.Builder
	backslashes := 0
	for _, r := range text {
		if r == '|' && backslashes%2 == 0 {
			result.WriteByte('\\')
		}
		result.WriteRune(r)
		if r == '\\' {
			backslashes++
		} else {
			backslashes = 0
		}
	}
	return result.String()
}

// Context contains visible source text, not deleted revisions or field code.
func docxContext(n *Node) string {
	var text strings.Builder
	var visit func(*Node)
	visit = func(node *Node) {
		switch node.XMLName.Local {
		case "del", "instrText", "delText":
			return
		case "t":
			text.WriteString(xmlText(node))
			return
		case "tab":
			text.WriteByte('\t')
		case "br", "cr":
			text.WriteByte('\n')
		}
		for i := range node.Nodes {
			visit(&node.Nodes[i])
		}
		if node.XMLName.Local == "p" {
			text.WriteByte('\n')
		}
	}
	visit(n)
	return strings.TrimSuffix(text.String(), "\n")
}

func (zf *file) headingLevel(style string) int {
	if level, found := zf.headings[style]; found {
		return level
	}
	if level := customHeadings[style]; level > 0 {
		return level
	}
	for _, prefix := range []string{"heading", "titre"} {
		if strings.HasPrefix(strings.ToLower(style), prefix) {
			level, _ := strconv.Atoi(strings.TrimSpace(style[len(prefix):]))
			if level >= 1 && level <= 6 {
				return level
			}
		}
	}
	return 0
}

func (zf *file) loadHeadings(n *Node) {
	zf.headings = make(map[string]int)
	styles := make(map[string]*Node)
	for _, style := range descendants(n, "style") {
		styles[nodeAttr(style, "styleId")] = style
	}
	var resolve func(string, map[string]bool) int
	resolve = func(id string, seen map[string]bool) int {
		if seen[id] {
			return 0
		}
		seen[id] = true
		style := styles[id]
		if outline := child(child(style, "pPr"), "outlineLvl"); outline != nil {
			level, err := strconv.Atoi(nodeAttr(outline, "val"))
			if err == nil && level >= 0 && level <= 5 {
				return level + 1
			}
			return 0 // outline level 9 explicitly denotes body text.
		}
		if level := zf.headingLevel(id); level > 0 {
			return level
		}
		if level := zf.headingLevel(nodeAttr(child(style, "name"), "val")); level > 0 {
			return level
		}
		if parent := nodeAttr(child(style, "basedOn"), "val"); parent != "" {
			return resolve(parent, seen)
		}
		return 0
	}
	for id := range styles {
		zf.headings[id] = resolve(id, make(map[string]bool))
	}
}

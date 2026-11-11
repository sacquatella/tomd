package docx2md

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sacquatella/tomd/tools"
)

// PptxOptions controls native extraction. Nil Describer means no AI or network
// access. Notes and hidden slides are included by default for exhaustive RAG.
type PptxOptions struct {
	EmbedImages   bool
	AssetsDir     string
	AssetsPrefix  string
	ExcludeNotes  bool
	ExcludeHidden bool
	Language      string
	Describer     tools.ImageDescriber
	Context       context.Context
	// Warn receives recoverable errors. By default warnings go to stderr.
	Warn func(string)
}

type pptxReader struct {
	files        map[string]*zip.File
	opts         PptxOptions
	slideNumbers map[string]int
}

func (p *pptxReader) warn(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if p.opts.Warn != nil {
		p.opts.Warn(message)
	} else {
		fmt.Fprintln(os.Stderr, "Warning: "+message)
	}
}

func (p *pptxReader) read(name string, limit int64) ([]byte, error) {
	f := p.files[name]
	if f == nil {
		return nil, fmt.Errorf("missing PPTX part %s", name)
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("PPTX part %s exceeds size limit", name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("PPTX part %s exceeds size limit", name)
	}
	return b, nil
}

func (p *pptxReader) node(name string) (*Node, error) {
	b, err := p.read(name, 32<<20)
	if err != nil {
		return nil, err
	}
	var n Node
	if err := xml.Unmarshal(b, &n); err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return &n, nil
}

func (p *pptxReader) relationships(owner string) (map[string]Relationship, error) {
	name := path.Join(path.Dir(owner), "_rels", path.Base(owner)+".rels")
	rels := make(map[string]Relationship)
	if p.files[name] == nil {
		return rels, nil
	}
	b, err := p.read(name, 4<<20)
	if err != nil {
		return nil, err
	}
	var parsed Relationships
	if err := xml.Unmarshal(b, &parsed); err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	for _, rel := range parsed.Relationship {
		if _, exists := rels[rel.ID]; exists {
			return nil, fmt.Errorf("duplicate relationship %s in %s", rel.ID, name)
		}
		rels[rel.ID] = rel
	}
	return rels, nil
}

// Package relationships use POSIX paths, never filesystem paths. Targets are
// resolved within the ZIP and cannot select files outside the package.
func pptxTarget(owner string, rel Relationship) (string, error) {
	if strings.EqualFold(rel.TargetMode, "External") {
		return "", fmt.Errorf("external package relationship")
	}
	u, err := url.Parse(rel.Target)
	if err != nil || u.IsAbs() || u.Host != "" || strings.Contains(rel.Target, "\\") {
		return "", fmt.Errorf("invalid package relationship")
	}
	target := u.Path
	if strings.HasPrefix(target, "/") {
		target = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		target = path.Clean(path.Join(path.Dir(owner), target))
	}
	if target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return "", fmt.Errorf("package relationship escapes archive")
	}
	return target, nil
}

func child(n *Node, name string) *Node {
	if n == nil {
		return nil
	}
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == name {
			return &n.Nodes[i]
		}
	}
	return nil
}

func descendants(n *Node, name string) []*Node {
	var result []*Node
	if n == nil {
		return result
	}
	if n.XMLName.Local == name {
		result = append(result, n)
	}
	for i := range n.Nodes {
		result = append(result, descendants(&n.Nodes[i], name)...)
	}
	return result
}

func first(n *Node, name string) *Node {
	if n == nil {
		return nil
	}
	if n.XMLName.Local == name {
		return n
	}
	for i := range n.Nodes {
		if found := first(&n.Nodes[i], name); found != nil {
			return found
		}
	}
	return nil
}

func nodeAttr(n *Node, name string) string {
	if n == nil {
		return ""
	}
	v, _ := attr(n.Attrs, name)
	return v
}

func relationshipID(n *Node, name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attrs {
		if a.Name.Local == name && strings.HasSuffix(a.Name.Space, "/relationships") {
			return a.Value
		}
	}
	return ""
}

func xmlText(n *Node) string {
	if n == nil {
		return ""
	}
	var text string
	// Node.Content contains inner XML; decode entities rather than indexing
	// literal &amp; or &lt; sequences.
	_ = xml.Unmarshal(append(append([]byte("<text>"), n.Content...), []byte("</text>")...), &text)
	return text
}

func plainText(n *Node) string {
	var parts []string
	for _, paragraph := range descendants(n, "p") {
		var s strings.Builder
		for _, t := range descendants(paragraph, "t") {
			s.WriteString(xmlText(t))
		}
		if text := strings.TrimSpace(s.String()); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func inlineMD(s string) string { return escape(s, "\\`*_{}[]<>#~|") }
func oneLine(s string) string  { return strings.Join(strings.Fields(s), " ") }

func blockID(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

func enabled(v string) bool {
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on")
}

func pptxMeta(p *pptxReader, filename string) (tools.Metadata, error) {
	var prop CoreProperties
	if p.files["docProps/core.xml"] != nil {
		b, err := p.read("docProps/core.xml", 4<<20)
		if err != nil {
			return tools.Metadata{}, err
		}
		if err := xml.Unmarshal(b, &prop); err != nil {
			return tools.Metadata{}, fmt.Errorf("parse core properties: %w", err)
		}
	}
	if strings.TrimSpace(prop.Title) == "" {
		prop.Title = strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	}
	meta := tools.Metadata{Title: prop.Title, Description: prop.Description}
	if strings.TrimSpace(prop.Creator) != "" {
		meta.Authors = []string{prop.Creator}
	}
	return meta, nil
}

func openPptx(filename string, options PptxOptions) (*zip.ReadCloser, *pptxReader, error) {
	r, err := zip.OpenReader(filename)
	if err != nil {
		return nil, nil, err
	}
	p := &pptxReader{files: make(map[string]*zip.File), opts: options, slideNumbers: make(map[string]int)}
	for _, f := range r.File {
		if _, exists := p.files[f.Name]; exists {
			r.Close()
			return nil, nil, fmt.Errorf("duplicate PPTX part %s", f.Name)
		}
		p.files[f.Name] = f
	}
	if p.opts.Context == nil {
		p.opts.Context = context.Background()
	}
	return r, p, nil
}

// Pptx2md preserves the public API. Non-embedded images are placed next to the
// input in <input-stem>-assets; exporters should use GetPptxWithOptions instead.
func Pptx2md(filename string, embed bool) (string, tools.Metadata, error) {
	return Pptx2mdWithOptions(filename, PptxOptions{EmbedImages: embed})
}

func Pptx2mdWithOptions(filename string, options PptxOptions) (string, tools.Metadata, error) {
	r, p, err := openPptx(filename, options)
	if err != nil {
		return "", tools.Metadata{}, err
	}
	defer r.Close()
	return p.convert(filename)
}

func (p *pptxReader) convert(filename string) (string, tools.Metadata, error) {
	meta, err := pptxMeta(p, filename)
	if err != nil {
		return "", meta, err
	}
	if p.opts.AssetsDir == "" {
		p.opts.AssetsDir = strings.TrimSuffix(filename, filepath.Ext(filename)) + "-assets"
	}
	if p.opts.AssetsPrefix == "" {
		p.opts.AssetsPrefix = filepath.Base(p.opts.AssetsDir)
	}
	presentation, err := p.node("ppt/presentation.xml")
	if err != nil {
		return "", meta, err
	}
	rels, err := p.relationships("ppt/presentation.xml")
	if err != nil {
		return "", meta, err
	}
	var slides []string
	for _, id := range descendants(child(presentation, "sldIdLst"), "sldId") {
		rel, ok := rels[relationshipID(id, "id")]
		if !ok || !strings.HasSuffix(rel.Type, "/slide") {
			return "", meta, fmt.Errorf("missing slide relationship")
		}
		target, err := pptxTarget("ppt/presentation.xml", rel)
		if err != nil {
			return "", meta, err
		}
		slides = append(slides, target)
		p.slideNumbers[target] = len(slides)
	}
	if len(slides) == 0 {
		return "", meta, fmt.Errorf("presentation contains no slides")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\n", inlineMD(oneLine(meta.Title)))
	for i, name := range slides {
		if err := p.opts.Context.Err(); err != nil {
			return "", meta, err
		}
		text, err := p.slide(filename, name, i+1)
		if err != nil {
			return "", meta, fmt.Errorf("slide %d (%s): %w", i+1, name, err)
		}
		out.WriteString(text)
		if err := p.opts.Context.Err(); err != nil {
			return "", meta, err
		}
	}
	return out.String(), meta, nil
}

func GetPptxWithOptions(filename, sourceURL, customerID, exportDir string, complements tools.Metadata, options PptxOptions) (tools.Page, error) {
	r, p, err := openPptx(filename, options)
	if err != nil {
		return tools.Page{}, err
	}
	defer r.Close()
	meta, err := pptxMeta(p, filename)
	if err != nil {
		return tools.Page{}, err
	}
	header, metadata := tools.BuildFileMetadata(filename, sourceURL, customerID, meta, complements)
	exported := tools.BuildFilename(metadata.Title, exportDir, customerID)
	p.opts.AssetsDir = strings.TrimSuffix(exported, filepath.Ext(exported)) + "-assets"
	p.opts.AssetsPrefix = filepath.Base(p.opts.AssetsDir)
	markdown, _, err := p.convert(filename)
	if err != nil {
		return tools.Page{}, err
	}
	if err := os.MkdirAll(filepath.Dir(exported), 0755); err != nil {
		return tools.Page{}, err
	}
	if err := tools.WriteMarkdownToFile(header+markdown, exported); err != nil {
		return tools.Page{}, err
	}
	return tools.Page{PageId: metadata.Doc_id, Url: metadata.Site_url, MdFile: exported}, nil
}

func placeholderType(shape, layout *Node) string {
	ph := first(shape, "ph")
	if ph == nil {
		return ""
	}
	if t := nodeAttr(ph, "type"); t != "" {
		return t
	}
	if inherited := layoutPlaceholder(shape, layout); inherited != nil {
		if t := nodeAttr(first(inherited, "ph"), "type"); t != "" {
			return t
		}
	}
	// PresentationML defaults an untyped placeholder to an object placeholder.
	return "obj"
}

func layoutPlaceholder(shape, layout *Node) *Node {
	ph := first(shape, "ph")
	if ph == nil || layout == nil {
		return nil
	}
	idx := nodeAttr(ph, "idx")
	if idx == "" {
		idx = "0"
	}
	for _, s := range descendants(layout, "sp") {
		candidate := first(s, "ph")
		if candidate == nil {
			continue
		}
		other := nodeAttr(candidate, "idx")
		if other == "" {
			other = "0"
		}
		if idx == other {
			return s
		}
	}
	return nil
}

func excludedPlaceholder(t string) bool {
	return t == "ftr" || t == "dt" || t == "sldNum" || t == "hdr"
}

func (p *pptxReader) slide(filename, name string, number int) (string, error) {
	n, err := p.node(name)
	if err != nil {
		return "", err
	}
	hidden := nodeAttr(n, "show") == "0" || nodeAttr(n, "show") == "false"
	if hidden && p.opts.ExcludeHidden {
		return "", nil
	}
	rels, err := p.relationships(name)
	if err != nil {
		return "", err
	}
	var layout, master *Node
	for _, id := range sortedRelIDs(rels) {
		rel := rels[id]
		if strings.HasSuffix(rel.Type, "/slideLayout") {
			target, e := pptxTarget(name, rel)
			if e == nil {
				layout, e = p.node(target)
			}
			if e != nil {
				p.warn("slide %d: layout unavailable: %v", number, e)
			} else {
				layoutRels, e := p.relationships(target)
				if e != nil {
					p.warn("slide %d: layout relationships unavailable: %v", number, e)
				}
				for _, id := range sortedRelIDs(layoutRels) {
					if !strings.HasSuffix(layoutRels[id].Type, "/slideMaster") {
						continue
					}
					masterTarget, e := pptxTarget(target, layoutRels[id])
					if e == nil {
						master, e = p.node(masterTarget)
					}
					if e != nil {
						p.warn("slide %d: master unavailable: %v", number, e)
					}
				}
			}
		}
	}
	var title string
	var titleShape *Node
	for _, shape := range descendants(first(n, "spTree"), "sp") {
		t := placeholderType(shape, layout)
		if t == "title" || t == "ctrTitle" {
			title = oneLine(plainText(child(shape, "txBody")))
			if title != "" {
				titleShape = shape
				break
			}
		}
	}
	if title == "" {
		title = "Untitled"
	}
	var out strings.Builder
	slideID := blockID(strings.TrimSuffix(path.Base(name), ".xml"))
	fmt.Fprintf(&out, "<a id=\"%s\"></a>\n\n", slideID)
	fmt.Fprintf(&out, "## Slide %d — %s\n\n", number, inlineMD(title))
	fmt.Fprintf(&out, "Source: %s | Slide: %d | ID: %s | Part: %s\n\n", inlineMD(filepath.Base(filename)), number, inlineMD(slideID), inlineMD(name))
	if hidden {
		out.WriteString("Hidden slide: yes\n\n")
	}
	contextText := plainText(n)
	if !p.opts.ExcludeNotes {
		// Notes are also useful context when describing an otherwise terse image.
		for _, id := range sortedRelIDs(rels) {
			if strings.HasSuffix(rels[id].Type, "/notesSlide") {
				target, e := pptxTarget(name, rels[id])
				if e == nil {
					if notes, e := p.node(target); e == nil {
						contextText += "\n" + plainText(notes)
					}
				}
			}
		}
	}
	labels := make(map[string]string)
	for _, kind := range []string{"sp", "pic", "graphicFrame", "grpSp"} {
		for _, shape := range descendants(first(n, "spTree"), kind) {
			if excludedPlaceholder(placeholderType(shape, layout)) {
				continue
			}
			id := nodeAttr(first(shape, "cNvPr"), "id")
			label := oneLine(plainText(child(shape, "txBody")))
			if label == "" {
				label = nodeAttr(first(shape, "cNvPr"), "descr")
			}
			if label == "" {
				label = nodeAttr(first(shape, "cNvPr"), "name")
			}
			if id != "" && label != "" {
				labels[id] = label
			}
		}
	}
	var render func(*Node)
	render = func(tree *Node) {
		if tree == nil {
			return
		}
		for i := range tree.Nodes {
			block := &tree.Nodes[i]
			id := nodeAttr(first(block, "cNvPr"), "id")
			if id == "" {
				id = strconv.Itoa(i + 1)
			}
			marker := fmt.Sprintf("<!-- block: %s-%s -->\n", slideID, blockID(id))
			switch block.XMLName.Local {
			case "grpSp":
				if groupName := nodeAttr(child(child(block, "nvGrpSpPr"), "cNvPr"), "name"); groupName != "" {
					fmt.Fprintf(&out, "%s### Group: %s\n\n", marker, inlineMD(oneLine(groupName)))
				}
				render(block)
			case "sp":
				if block == titleShape || excludedPlaceholder(placeholderType(block, layout)) {
					continue
				}
				styleName := "otherStyle"
				switch placeholderType(block, layout) {
				case "body", "obj", "subTitle":
					styleName = "bodyStyle"
				case "title", "ctrTitle":
					styleName = "titleStyle"
				}
				style := child(child(master, "txStyles"), styleName)
				masterText := &Node{Nodes: []Node{{XMLName: xml.Name{Local: "lstStyle"}}}}
				if style != nil {
					masterText.Nodes[0].Nodes = style.Nodes
				}
				text := p.text(child(block, "txBody"), child(layoutPlaceholder(block, layout), "txBody"), name, rels, masterText)
				if text != "" {
					out.WriteString(marker + text + "\n\n")
				}
			case "pic":
				out.WriteString(marker)
				out.WriteString(p.picture(block, name, rels, contextText))
			case "cxnSp":
				start := nodeAttr(first(block, "stCxn"), "id")
				end := nodeAttr(first(block, "endCxn"), "id")
				if labels[start] == "" || labels[end] == "" {
					out.WriteString(marker + "Connector: endpoints not explicitly identified.\n\n")
					p.warn("slide %d: connector endpoints could not be resolved", number)
					continue
				}
				arrow := "—"
				head := nodeAttr(first(block, "headEnd"), "type")
				tail := nodeAttr(first(block, "tailEnd"), "type")
				hasHead, hasTail := head != "" && head != "none", tail != "" && tail != "none"
				if hasHead && hasTail {
					arrow = "↔"
				} else if hasHead {
					arrow = "←"
				} else if hasTail {
					arrow = "→"
				}
				fmt.Fprintf(&out, "%sRelation: %s %s %s\n\n", marker, inlineMD(labels[start]), arrow, inlineMD(labels[end]))
				if text := p.text(child(block, "txBody"), nil, name, rels); text != "" {
					out.WriteString(text + "\n\n")
				}
			case "graphicFrame":
				out.WriteString(marker)
				if table := first(block, "tbl"); table != nil {
					out.WriteString(p.table(table, name, rels))
				} else if chart := first(block, "chart"); chart != nil {
					out.WriteString(p.relatedGraphic(name, rels[relationshipID(chart, "id")], false))
				} else if diagram := first(block, "relIds"); diagram != nil {
					out.WriteString(p.relatedGraphic(name, rels[relationshipID(diagram, "dm")], true))
				} else {
					out.WriteString("Unsupported graphic object: no native extraction available.\n\n")
					p.warn("slide %d: unsupported graphic object", number)
				}
			case "AlternateContent":
				choice := child(block, "Choice")
				if choice == nil {
					choice = child(block, "Fallback")
				}
				if choice != nil {
					render(choice)
				}
			case "contentPart":
				out.WriteString("Unsupported embedded content.\n\n")
				p.warn("slide %d: unsupported embedded content", number)
			}
		}
	}
	render(first(n, "spTree"))
	if !p.opts.ExcludeNotes {
		for _, id := range sortedRelIDs(rels) {
			if !strings.HasSuffix(rels[id].Type, "/notesSlide") {
				continue
			}
			target, e := pptxTarget(name, rels[id])
			var notes *Node
			if e == nil {
				notes, e = p.node(target)
			}
			if e != nil {
				p.warn("slide %d: notes unavailable: %v", number, e)
				out.WriteString("Presenter notes unavailable.\n\n")
				continue
			}
			noteRels, e := p.relationships(target)
			if e != nil {
				p.warn("slide %d: notes relationships unavailable", number)
				noteRels = nil
			}
			var text []string
			for _, shape := range descendants(notes, "sp") {
				t := nodeAttr(first(shape, "ph"), "type")
				if excludedPlaceholder(t) || t == "sldImg" {
					continue
				}
				if s := p.text(child(shape, "txBody"), nil, target, noteRels); s != "" {
					text = append(text, s)
				}
			}
			if len(text) > 0 {
				out.WriteString("### Presenter notes\n\n" + strings.Join(text, "\n\n") + "\n\n")
			}
		}
	}
	out.WriteString("---\n\n")
	return out.String(), nil
}

func sortedRelIDs(rels map[string]Relationship) []string {
	ids := make([]string, 0, len(rels))
	for id := range rels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (p *pptxReader) link(owner string, rel Relationship) string {
	if strings.EqualFold(rel.TargetMode, "External") {
		u, err := url.Parse(rel.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
			return ""
		}
		return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "\n", "", "\r", "").Replace(rel.Target)
	}
	target, err := pptxTarget(owner, rel)
	if err == nil {
		if number := p.slideNumbers[target]; number != 0 {
			return "#" + blockID(strings.TrimSuffix(path.Base(target), ".xml"))
		}
	}
	return ""
}

func (p *pptxReader) run(n *Node, owner string, rels map[string]Relationship, defaults *Node) string {
	var out strings.Builder
	for i := range n.Nodes {
		c := &n.Nodes[i]
		switch c.XMLName.Local {
		case "t":
			out.WriteString(inlineMD(xmlText(c)))
		case "tab":
			out.WriteString("    ")
		case "br":
			out.WriteString("<br>")
		}
	}
	text := out.String()
	if strings.TrimSpace(text) == "" {
		return text
	}
	props := child(n, "rPr")
	style := func(key string) string {
		if v := nodeAttr(props, key); v != "" {
			return v
		}
		return nodeAttr(defaults, key)
	}
	core := strings.TrimSpace(text)
	leading := text[:len(text)-len(strings.TrimLeft(text, " \t\n\r"))]
	trailing := text[len(strings.TrimRight(text, " \t\n\r")):]
	if enabled(style("b")) {
		core = "**" + core + "**"
	}
	if enabled(style("i")) {
		core = "*" + core + "*"
	}
	if strike := style("strike"); strike == "sngStrike" || strike == "dblStrike" {
		core = "~~" + core + "~~"
	}
	if link := child(props, "hlinkClick"); link != nil {
		if target := p.link(owner, rels[relationshipID(link, "id")]); target != "" {
			core = "[" + core + "](" + target + ")"
		}
	}
	return leading + core + trailing
}

func paragraphProps(txBody, inherited, para *Node, level int, fallbacks ...*Node) []*Node {
	name := fmt.Sprintf("lvl%dpPr", level+1)
	props := []*Node{child(para, "pPr"), child(child(txBody, "lstStyle"), name), child(child(inherited, "lstStyle"), name)}
	if inherited != nil {
		for i := range inherited.Nodes {
			ps := &inherited.Nodes[i]
			if ps.XMLName.Local != "p" {
				continue
			}
			pr := child(ps, "pPr")
			inheritedLevel, _ := strconv.Atoi(nodeAttr(pr, "lvl"))
			if inheritedLevel == level {
				props = append(props, pr)
				break
			}
		}
	}
	for _, fallback := range fallbacks {
		props = append(props, child(child(fallback, "lstStyle"), name))
	}
	return props
}

func (p *pptxReader) text(txBody, inherited *Node, owner string, rels map[string]Relationship, fallbacks ...*Node) string {
	if txBody == nil {
		return ""
	}
	var lines []string
	counters := make(map[int]int)
	for i := range txBody.Nodes {
		para := &txBody.Nodes[i]
		if para.XMLName.Local != "p" {
			continue
		}
		level, _ := strconv.Atoi(nodeAttr(child(para, "pPr"), "lvl"))
		if level < 0 {
			level = 0
		}
		if level > 8 {
			level = 8
		}
		properties := paragraphProps(txBody, inherited, para, level, fallbacks...)
		defaults := &Node{}
		var bullet *Node
		for _, pr := range properties {
			if def := child(pr, "defRPr"); def != nil {
				for _, a := range def.Attrs {
					if _, exists := attr(defaults.Attrs, a.Name.Local); !exists {
						defaults.Attrs = append(defaults.Attrs, a)
					}
				}
			}
			if bullet == nil && pr != nil {
				for j := range pr.Nodes {
					switch pr.Nodes[j].XMLName.Local {
					case "buNone", "buChar", "buAutoNum", "buBlip":
						bullet = &pr.Nodes[j]
					}
				}
			}
		}
		var s strings.Builder
		for j := range para.Nodes {
			n := &para.Nodes[j]
			switch n.XMLName.Local {
			case "r", "fld":
				s.WriteString(p.run(n, owner, rels, defaults))
			case "br":
				s.WriteString("<br>")
			}
		}
		text := strings.TrimSpace(s.String())
		if text == "" {
			continue
		}
		prefix := ""
		if bullet != nil && bullet.XMLName.Local != "buNone" {
			prefix = strings.Repeat("    ", level) + "- "
			if bullet.XMLName.Local == "buAutoNum" {
				if counters[level] == 0 {
					counters[level], _ = strconv.Atoi(nodeAttr(bullet, "startAt"))
					if counters[level] < 1 {
						counters[level] = 1
					}
				}
				prefix = strings.Repeat("    ", level) + strconv.Itoa(counters[level]) + ". "
				counters[level]++
			}
			for l := range counters {
				if l > level {
					delete(counters, l)
				}
			}
		} else {
			clear(counters)
		}
		lines = append(lines, prefix+text)
	}
	return strings.Join(lines, "\n")
}

func (p *pptxReader) table(table *Node, owner string, rels map[string]Relationship) string {
	var rows [][]string
	width := 0
	merged := false
	for _, tr := range table.Nodes {
		if tr.XMLName.Local != "tr" {
			continue
		}
		var row []string
		for i := range tr.Nodes {
			tc := &tr.Nodes[i]
			if tc.XMLName.Local != "tc" {
				continue
			}
			for _, key := range []string{"gridSpan", "rowSpan", "hMerge", "vMerge"} {
				if v := nodeAttr(tc, key); v != "" && v != "0" && (v != "1" || strings.HasSuffix(key, "Merge")) {
					merged = true
				}
			}
			text := p.text(child(tc, "txBody"), nil, owner, rels)
			row = append(row, strings.ReplaceAll(text, "\n", "<br>"))
		}
		if len(row) > width {
			width = len(row)
		}
		rows = append(rows, row)
	}
	if width == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("### Table\n\n")
	// Do not invent a semantic header when PowerPoint doesn't declare one.
	header := first(table, "tblPr")
	if !enabled(nodeAttr(header, "firstRow")) {
		cols := make([]string, width)
		for i := range cols {
			cols[i] = fmt.Sprintf("Column %d", i+1)
		}
		rows = append([][]string{cols}, rows...)
	}
	for i, row := range rows {
		out.WriteString("|")
		for j := 0; j < width; j++ {
			value := ""
			if j < len(row) {
				value = row[j]
			}
			out.WriteString(" " + value + " |")
		}
		out.WriteString("\n")
		if i == 0 {
			out.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
		}
	}
	if merged {
		out.WriteString("\nMerged cells: content retained in its source cell; spans are not represented in Markdown.\n")
	}
	out.WriteString("\n")
	return out.String()
}

func (p *pptxReader) picture(block *Node, owner string, rels map[string]Relationship, contextText string) string {
	blip := first(block, "blip")
	rel, ok := rels[relationshipID(blip, "embed")]
	if !ok {
		p.warn("%s: missing or external image relationship", owner)
		return "Image unavailable (missing or external relationship).\n\n"
	}
	target, err := pptxTarget(owner, rel)
	if err != nil {
		p.warn("%s: invalid image relationship", owner)
		return "Image unavailable (invalid relationship).\n\n"
	}
	data, err := p.read(target, 20<<20)
	if err != nil {
		p.warn("%s: image unavailable: %v", owner, err)
		return "Image unavailable.\n\n"
	}
	props := first(block, "cNvPr")
	label := nodeAttr(props, "descr")
	if label == "" {
		label = nodeAttr(props, "title")
	}
	if label == "" {
		label = nodeAttr(props, "name")
	}
	if label == "" {
		label = "Image"
	}
	mime := http.DetectContentType(data)
	// DetectContentType does not recognize all Office image formats.
	if strings.EqualFold(path.Ext(target), ".svg") {
		mime = "image/svg+xml"
	}
	var source string
	if p.opts.EmbedImages {
		source = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	} else {
		hash := sha256.Sum256(data)
		ext := strings.ToLower(path.Ext(target))
		if len(ext) > 10 || strings.ContainsAny(ext, "/\\") {
			ext = ".bin"
		}
		asset := fmt.Sprintf("image-%x%s", hash[:12], ext)
		if err := os.MkdirAll(p.opts.AssetsDir, 0755); err != nil {
			p.warn("cannot create image directory: %v", err)
			return "Image export unavailable.\n\n"
		}
		if err := os.WriteFile(filepath.Join(p.opts.AssetsDir, asset), data, 0644); err != nil {
			p.warn("cannot export image: %v", err)
			return "Image export unavailable.\n\n"
		}
		source = (&url.URL{Path: path.Join(filepath.ToSlash(p.opts.AssetsPrefix), asset)}).EscapedPath()
	}
	text := fmt.Sprintf("![%s](%s)\n\n", inlineMD(oneLine(label)), source)
	if p.opts.Describer != nil {
		description, err := p.opts.Describer.Describe(p.opts.Context, data, mime, p.opts.Language, contextText)
		if err != nil {
			p.warn("%s: AI image description unavailable: %v", owner, err)
			text += "AI-generated description unavailable; native content retained.\n\n"
		} else {
			text += "AI-generated visual description:\n\n" + description + "\n\n"
		}
	}
	return text
}

func (p *pptxReader) relatedGraphic(owner string, rel Relationship, diagram bool) string {
	target, err := pptxTarget(owner, rel)
	var n *Node
	if err == nil {
		n, err = p.node(target)
	}
	if err != nil {
		p.warn("%s: graphic data unavailable: %v", owner, err)
		return "Graphic data unavailable.\n\n"
	}
	if diagram {
		return p.diagram(n)
	}
	return p.chart(n)
}

func (p *pptxReader) diagram(n *Node) string {
	var out strings.Builder
	out.WriteString("### Diagram (native SmartArt data)\n\n")
	labels := make(map[string]string)
	for _, point := range descendants(n, "pt") {
		text := oneLine(plainText(point))
		if text != "" {
			labels[nodeAttr(point, "modelId")] = text
			fmt.Fprintf(&out, "- %s\n", inlineMD(text))
		}
	}
	for _, conn := range descendants(n, "cxn") {
		from, to := labels[nodeAttr(conn, "srcId")], labels[nodeAttr(conn, "destId")]
		if from != "" && to != "" {
			fmt.Fprintf(&out, "\nStructural relation (%s): %s → %s\n", inlineMD(nodeAttr(conn, "type")), inlineMD(from), inlineMD(to))
		}
	}
	if len(labels) == 0 {
		out.WriteString("No readable labels available.\n")
		p.warn("SmartArt has no readable labels")
	}
	out.WriteString("\nRelationships above describe the data model, not inferred visual arrows. Layout is not rendered.\n\n")
	return out.String()
}

func cacheValues(n *Node) map[int]string {
	values := make(map[int]string)
	for _, pt := range descendants(n, "pt") {
		index, err := strconv.Atoi(nodeAttr(pt, "idx"))
		if err == nil {
			values[index] = xmlText(child(pt, "v"))
		}
	}
	return values
}

func (p *pptxReader) chart(n *Node) string {
	var out strings.Builder
	out.WriteString("### Chart (native cached data)\n\n")
	if title := plainText(first(n, "title")); title != "" {
		out.WriteString(inlineMD(oneLine(title)) + "\n\n")
	}
	count := 0
	for i, series := range descendants(n, "ser") {
		name := xmlText(first(child(series, "tx"), "v"))
		if name == "" {
			name = fmt.Sprintf("Series %d", i+1)
		}
		categories := child(series, "cat")
		if categories == nil {
			categories = child(series, "xVal")
		}
		valueNode := child(series, "val")
		if valueNode == nil {
			valueNode = child(series, "yVal")
		}
		cats, values := cacheValues(categories), cacheValues(valueNode)
		if len(values) == 0 {
			continue
		}
		count++
		fmt.Fprintf(&out, "Series: %s\n\n", inlineMD(oneLine(name)))
		if format := xmlText(first(valueNode, "formatCode")); format != "" {
			fmt.Fprintf(&out, "Source number format: %s\n\n", inlineMD(oneLine(format)))
		}
		out.WriteString("| Category / X | Value / Y |\n| --- | --- |\n")
		indices := make([]int, 0, len(values))
		for idx := range values {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		for _, idx := range indices {
			category := cats[idx]
			if category == "" {
				category = fmt.Sprintf("Point %d", idx+1)
			}
			fmt.Fprintf(&out, "| %s | %s |\n", inlineMD(oneLine(category)), inlineMD(oneLine(values[idx])))
		}
		out.WriteString("\n")
	}
	if count == 0 {
		out.WriteString("No cached values available; linked workbooks are not evaluated.\n\n")
		p.warn("chart has no cached values")
	}
	out.WriteString("Chart values are cached source data and may differ from an externally updated workbook.\n\n")
	return out.String()
}

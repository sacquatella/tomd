package docx2md

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"github.com/sacquatella/tomd/tools"
)

// UnmarshalXML unmarshals the XML element into a Node.
func (n *Node) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	n.Attrs = start.Attr
	type node Node

	return d.DecodeElement((*node)(n), &start)
}

// escape escapes the characters in a string using the given set of characters.
func escape(s, set string) string {
	replacer := []string{}
	for _, r := range []rune(set) {
		rs := string(r)
		replacer = append(replacer, rs, `\`+rs)
	}
	return strings.NewReplacer(replacer...).Replace(s)
}

// attr returns the value of the attribute with the given name.
func attr(attrs []xml.Attr, name string) (string, bool) {
	for _, attr := range attrs {
		if attr.Name.Local == name {
			return attr.Value, true
		}
	}
	return "", false
}

// walk traverses the XML tree and writes the content to the writer.
func (zf *file) walk(node *Node, w io.Writer) error {
	if zf.opts.Context != nil {
		if err := zf.opts.Context.Err(); err != nil {
			return err
		}
	}
	switch node.XMLName.Local {
	case "body":
		for i := range node.Nodes {
			if i > 0 && child(child(&node.Nodes[i-1], "pPr"), "numPr") != nil && child(child(&node.Nodes[i], "pPr"), "numPr") == nil {
				fmt.Fprintln(w)
			}
			var contextParts []string
			for j := max(0, i-1); j <= min(len(node.Nodes)-1, i+1); j++ {
				contextParts = append(contextParts, docxContext(&node.Nodes[j]))
			}
			contextRunes := []rune(strings.Join(contextParts, "\n"))
			zf.contextText = string(contextRunes[:min(6000, len(contextRunes))])
			if err := zf.walk(&node.Nodes[i], w); err != nil {
				return err
			}
		}
		zf.contextText = ""
	case "hyperlink":
		var cbuf bytes.Buffer
		for _, n := range node.Nodes {
			if err := zf.walk(&n, &cbuf); err != nil {
				return err
			}
		}
		target := ""
		if id, ok := attr(node.Attrs, "id"); ok {
			for _, rel := range zf.rels.Relationship {
				if id == rel.ID {
					target = rel.Target
					break
				}
			}
		}
		if anchor := nodeAttr(node, "anchor"); anchor != "" {
			target = "#" + anchor
		}
		if target == "" {
			fmt.Fprint(w, cbuf.String())
		} else {
			fmt.Fprintf(w, "[%s](%s)", cbuf.String(), escape(target, "() "))
		}
	case "t":
		fmt.Fprint(w, inlineMD(xmlText(node)))
	case "tab":
		fmt.Fprint(w, "\t")
	case "br", "cr":
		fmt.Fprint(w, "  \n")
	case "del", "instrText", "delText":
		// Deleted revisions and field instructions are not visible source text.
	case "pPr":
		// Traitement des propriétés de paragraphe
		level := zf.headingLevel(nodeAttr(child(node, "pStyle"), "val"))
		if outline := child(node, "outlineLvl"); outline != nil {
			level = 0
			if value, err := strconv.Atoi(nodeAttr(outline, "val")); err == nil && value >= 0 && value <= 5 {
				level = value + 1
			}
		}
		if level > 0 {
			fmt.Fprint(w, strings.Repeat("#", level)+" ")
		}
		for _, n := range node.Nodes {
			switch n.XMLName.Local {
			case "ind":
				if child(node, "numPr") != nil {
					continue
				}
				if left, ok := attr(n.Attrs, "left"); ok {
					if i, err := strconv.Atoi(left); err == nil && i > 0 {
						fmt.Fprint(w, strings.Repeat("  ", min(i/360, 10)))
					}
				}
			case "numPr":
				numID := ""
				ilvl := "0"
				numFmt := ""
				start := 1
				ind := 0
				for _, nn := range n.Nodes {
					if nn.XMLName.Local == "numId" {
						if val, ok := attr(nn.Attrs, "val"); ok {
							numID = val
						}
					}
					if nn.XMLName.Local == "ilvl" {
						if val, ok := attr(nn.Attrs, "val"); ok {
							ilvl = val
						}
					}
				}
				for _, num := range zf.num.Num {
					if numID != num.NumID {
						continue
					}
					for _, abnum := range zf.num.AbstractNum {
						if abnum.AbstractNumID != num.AbstractNumID.Val {
							continue
						}
						for _, ablvl := range abnum.Lvl {
							if ablvl.Ilvl != ilvl {
								continue
							}
							if i, err := strconv.Atoi(ablvl.Start.Val); err == nil {
								start = i
							}
							if i, err := strconv.Atoi(ablvl.PPr.Ind.Left); err == nil {
								ind = i / 360
							}
							numFmt = ablvl.NumFmt.Val
							break
						}
						break
					}
					break
				}

				if level, err := strconv.Atoi(ilvl); err == nil && level >= 0 && level < 10 {
					ind = level
				}
				fmt.Fprint(w, strings.Repeat("  ", min(max(ind, 0), 10)))
				// A new parent item restarts its deeper list levels.
				level, _ := strconv.Atoi(ilvl)
				for key := range zf.list {
					if strings.HasPrefix(key, numID+":") {
						deeper, _ := strconv.Atoi(strings.TrimPrefix(key, numID+":"))
						if deeper > level {
							delete(zf.list, key)
						}
					}
				}
				switch numFmt {
				case "decimal", "aiueoFullWidth":
					key := numID + ":" + ilvl
					cur, ok := zf.list[key]
					if !ok {
						zf.list[key] = start
					} else {
						zf.list[key] = cur + 1
					}
					fmt.Fprintf(w, "%d. ", zf.list[key])
				case "bullet":
					fmt.Fprint(w, "* ")
				}
			}
		}
	case "tbl":
		// Traitement des tableaux
		var rows [][]string
		for _, tr := range node.Nodes {
			if tr.XMLName.Local != "tr" {
				continue
			}
			var cols []string
			isHeaderRow := false
			for _, tc := range tr.Nodes {
				if tc.XMLName.Local != "tc" {
					continue
				}
				var cbuf bytes.Buffer
				if err := zf.walk(&tc, &cbuf); err != nil {
					return err
				}
				content := docxTableCell(cbuf.String())

				// Vérifiez si cette cellule appartient à une ligne d'entête
				for _, tcPr := range tc.Nodes {
					if tcPr.XMLName.Local == "tcPr" {
						for _, attr := range tcPr.Attrs {
							if attr.Name.Local == "val" && attr.Value == "Header" {
								isHeaderRow = true
							}
						}
					}
				}
				if isHeaderRow {
					cols = append(cols, "**"+content+"**") // Contenu de l'entête en gras
				} else {
					cols = append(cols, content)
				}
			}
			rows = append(rows, cols)
		}

		// Gestion de la largeur des colonnes et affichage
		maxcol := 0
		for _, cols := range rows {
			if len(cols) > maxcol {
				maxcol = len(cols)
			}
		}
		widths := make([]int, maxcol)
		for i := range widths {
			widths[i] = 3
		}
		for _, row := range rows {
			for i := 0; i < maxcol; i++ {
				if i < len(row) {
					width := runewidth.StringWidth(row[i])
					if widths[i] < width {
						widths[i] = width
					}
				}
			}
		}
		for i, row := range rows {
			if i == 0 {
				// Afficher la première ligne
				for j := 0; j < maxcol; j++ {
					fmt.Fprint(w, "|")
					if j < len(row) {
						width := runewidth.StringWidth(row[j])
						fmt.Fprint(w, row[j])
						fmt.Fprint(w, strings.Repeat(" ", widths[j]-width))
					} else {
						fmt.Fprint(w, strings.Repeat(" ", widths[j]))
					}
				}
				fmt.Fprint(w, "|\n")

				// Ligne de séparation après le header
				for j := 0; j < maxcol; j++ {
					fmt.Fprint(w, "|")
					fmt.Fprint(w, strings.Repeat("-", widths[j]))
				}
				fmt.Fprint(w, "|\n")
			} else {
				// Lignes normales du tableau
				for j := 0; j < maxcol; j++ {
					fmt.Fprint(w, "|")
					if j < len(row) {
						width := runewidth.StringWidth(row[j])
						fmt.Fprint(w, row[j])
						fmt.Fprint(w, strings.Repeat(" ", widths[j]-width))
					} else {
						fmt.Fprint(w, strings.Repeat(" ", widths[j]))
					}
				}
				fmt.Fprint(w, "|\n")
			}
		}
		fmt.Fprint(w, "\n")
	case "r":
		// Traitement des chaines en gras, italique et barré
		bold := false
		italic := false
		strike := false
		link := false
		RelationId := ""
		for _, n := range node.Nodes {
			if n.XMLName.Local != "rPr" {
				continue
			}
			// Loop arround subnode XMLName to get bold, italic and strike for pptx context
			if _, exist := attr(n.Attrs, "b"); exist {
				bold = true
			}
			if _, exist := attr(n.Attrs, "i"); exist {
				italic = true
			}
			if _, exist := attr(n.Attrs, "strike"); exist {
				strike = true
			}

			// Loop arround subnode XMLName to get bold, italic and strike for docx context
			for _, nn := range n.Nodes {
				switch nn.XMLName.Local {
				case "b":
					bold = docxOn(&nn)
				case "i":
					italic = docxOn(&nn)
				case "strike":
					strike = docxOn(&nn)
				case "hlinkClick":
					link = true
					RelationId, _ = attr(nn.Attrs, "id")
				}
			}
		}
		if first(node, "drawing") != nil || first(node, "pict") != nil {
			bold, italic, strike = false, false, false
		}
		var cbuf bytes.Buffer
		for _, n := range node.Nodes {
			if err := zf.walk(&n, &cbuf); err != nil {
				return err
			}
		}
		text := cbuf.String()
		if bold || italic || strike {
			trimmed := strings.TrimSpace(text)
			if trimmed != "" {
				left := text[:len(text)-len(strings.TrimLeftFunc(text, unicode.IsSpace))]
				right := text[len(strings.TrimRightFunc(text, unicode.IsSpace)):]
				if italic {
					trimmed = "*" + trimmed + "*"
				}
				if bold {
					trimmed = "**" + trimmed + "**"
				}
				if strike {
					trimmed = "~~" + trimmed + "~~"
				}
				text = left + trimmed + right
			}
		}
		if link {
			for _, rel := range zf.rels.Relationship {
				if RelationId == rel.ID {
					text = "[" + text + "](" + escape(rel.Target, "() ") + ")"
					break
				}
			}
		}
		fmt.Fprint(w, text)
	case "p":
		style := strings.ToLower(tools.RemoveAccents(nodeAttr(child(child(node, "pPr"), "pStyle"), "val")))
		if strings.Contains(style, "preformat") || strings.Contains(style, "prformat") || style == "code" || style == "sourcecode" || style == "codeblock" {
			// Do not escape Markdown characters inside preformatted source code.
			for _, line := range strings.Split(strings.TrimSuffix(docxContext(node), "\n"), "\n") {
				fmt.Fprintln(w, "    "+line)
			}
			fmt.Fprintln(w)
			return nil
		}
		previousContext := zf.contextText
		if zf.contextText == "" {
			zf.contextText = docxContext(node)
		}
		var paragraph bytes.Buffer
		for _, n := range node.Nodes {
			if err := zf.walk(&n, &paragraph); err != nil {
				return err
			}
		}
		zf.contextText = previousContext
		if strings.TrimSpace(paragraph.String()) == "" {
			return nil
		}
		fmt.Fprint(w, paragraph.String())
		fmt.Fprintln(w)
		if first(child(node, "pPr"), "numPr") == nil {
			fmt.Fprintln(w)
		}
	case "drawing", "pict":
		return zf.drawing(node, w)
	case "pic":
		return zf.drawing(node, w)
	case "Fallback":
		// Traitement des fallback
	case "AlternateContent":
		branch := child(node, "Choice")
		if branch == nil || (first(branch, "blip") == nil && first(branch, "txbxContent") == nil) {
			branch = child(node, "Fallback")
		}
		if branch != nil {
			for i := range branch.Nodes {
				if err := zf.walk(&branch.Nodes[i], w); err != nil {
					return err
				}
			}
		}
	case "txbxContent":
		// Text boxes retain native Markdown rather than becoming code blocks.
		var cbuf bytes.Buffer
		for _, n := range node.Nodes {
			if err := zf.walk(&n, &cbuf); err != nil {
				return err
			}
		}
		fmt.Fprint(w, "\n\n"+cbuf.String())
	default:
		for _, n := range node.Nodes {
			if err := zf.walk(&n, w); err != nil {
				return err
			}
		}
	}

	return nil
}

// readFile
func readFile(f *zip.File) (*Node, error) {
	b, err := readDocxPart(f, 32<<20)
	if err != nil {
		return nil, err
	}

	var node Node
	err = xml.Unmarshal(b, &node)
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func findFile(files []*zip.File, target string) *zip.File {
	for _, f := range files {
		if ok, _ := path.Match(target, f.Name); ok {
			return f
		}
	}
	return nil
}

// Docx2md return a markdown string from a docx file
func Docx2md(arg string, embed bool) (string, tools.Metadata, error) {
	return Docx2mdWithOptions(arg, DocxOptions{EmbedImages: embed})
}

// GetDocx convert a docx file to markdown and add metadata header
func GetDocx(docxPath string, url string, customerId string, exportDir string, complements tools.Metadata) (tools.Page, error) {
	return GetDocxWithOptions(docxPath, url, customerId, exportDir, complements, DocxOptions{})
}

// GetPptx convert a pptx file to markdown and add metadata header
func GetPptx(pptxPath string, url string, customerId string, exportDir string, complements tools.Metadata) (tools.Page, error) {
	return GetPptxWithOptions(pptxPath, url, customerId, exportDir, complements, PptxOptions{})
}

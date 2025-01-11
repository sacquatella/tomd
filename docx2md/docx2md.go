package docx2md

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	log "github.com/sirupsen/logrus"

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

// extract img and générate markdown image tag with it decription
func (zf *file) extract(rel *Relationship, w io.Writer, desc string) error {

	description := strings.ReplaceAll(desc, "\n", "")

	err := os.MkdirAll(filepath.Dir(rel.Target), 0755)
	if err != nil {
		return err
	}
	for _, f := range zf.r.File {
		log.Infof("File: %s\n", f.Name)
		log.Infof("Target: %s\n", rel.Target)
		// replace ../ by ppt/ in rel.Target for pptx case
		pptxTarget := strings.Replace(rel.Target, "../", "ppt/", 1)
		if f.Name != "word/"+rel.Target && f.Name != pptxTarget {
			log.Infof("Not Match")
			continue
		}
		log.Infof("Match Found compute image Name: %s\n and rel.Target %s\n", f.Name, rel.Target)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		b := make([]byte, f.UncompressedSize64)
		n, err := rc.Read(b)
		if err != nil && err != io.EOF {
			return err
		}
		if zf.embed {
			fmt.Fprintf(w, "![%s](data:image/png;base64,%s)",
				description, base64.StdEncoding.EncodeToString(b[:n]))
		} else {
			err = os.WriteFile(rel.Target, b, 0644)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "![%s](%s)", description, escape(rel.Target, "()"))
		}
		break
	}
	return nil
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
	switch node.XMLName.Local {
	case "hyperlink", "hlinkClick":
		// Traitement des hyperliens
		fmt.Fprint(w, "[")
		var cbuf bytes.Buffer
		for _, n := range node.Nodes {
			if err := zf.walk(&n, &cbuf); err != nil {
				return err
			}
		}
		fmt.Fprint(w, escape(cbuf.String(), "[]"))
		fmt.Fprint(w, "]")

		fmt.Fprint(w, "(")
		if id, ok := attr(node.Attrs, "id"); ok {
			for _, rel := range zf.rels.Relationship {
				if id == rel.ID {
					fmt.Fprint(w, escape(rel.Target, "()"))
					break
				}
			}
		}
		fmt.Fprint(w, ")")
	case "t":
		// Traitement du texte
		fmt.Fprint(w, string(node.Content))
	case "pPr":
		// Traitement des propriétés de paragraphe
		code := false
		for _, n := range node.Nodes {
			switch n.XMLName.Local {
			case "ind":
				if left, ok := attr(n.Attrs, "left"); ok {
					if i, err := strconv.Atoi(left); err == nil && i > 0 {
						fmt.Fprint(w, strings.Repeat("  ", i/360))
					}
				}
			case "pStyle":
				if val, ok := attr(n.Attrs, "val"); ok {
					log.Infof("Style found: %s\n", val) // Debug
					if strings.HasPrefix(val, "Heading") {
						if i, err := strconv.Atoi(val[7:]); err == nil && i > 0 {
							fmt.Fprint(w, strings.Repeat("#", i)+" ")
						}
					} else if level, found := customHeadings[val]; found {
						fmt.Fprint(w, strings.Repeat("#", level)+" ")
					} else {
						log.Infof("Unrecognized style: %s\n", val)
					}
				}
			case "numPr":
				numID := ""
				ilvl := ""
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

				fmt.Fprint(w, strings.Repeat("  ", ind))
				switch numFmt {
				case "decimal", "aiueoFullWidth":
					key := fmt.Sprintf("%s:%d", numID, ind)
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
		if code {
			fmt.Fprint(w, "`")
		}
		for _, n := range node.Nodes {
			if err := zf.walk(&n, w); err != nil {
				return err
			}
		}
		if code {
			fmt.Fprint(w, "`")
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
				content := strings.TrimSpace(cbuf.String())

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
						fmt.Fprint(w, escape(row[j], "|"))
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
						fmt.Fprint(w, escape(row[j], "|"))
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
		// Traitement des runs
		bold := false
		italic := false
		strike := false
		for _, n := range node.Nodes {
			if n.XMLName.Local != "rPr" {
				continue
			}
			for _, nn := range n.Nodes {
				switch nn.XMLName.Local {
				case "b":
					bold = true
				case "i":
					italic = true
				case "strike":
					strike = true
				}
			}
		}
		if strike {
			fmt.Fprint(w, "~~")
		}
		if bold {
			fmt.Fprint(w, "**")
		}
		if italic {
			fmt.Fprint(w, "*")
		}
		var cbuf bytes.Buffer
		for _, n := range node.Nodes {
			if err := zf.walk(&n, &cbuf); err != nil {
				return err
			}
		}
		fmt.Fprint(w, escape(cbuf.String(), `*~\`))
		if italic {
			fmt.Fprint(w, "*")
		}
		if bold {
			fmt.Fprint(w, "**")
		}
		if strike {
			fmt.Fprint(w, "~~")
		}
	case "p":
		// Traitement des paragraphes
		for _, n := range node.Nodes {
			if err := zf.walk(&n, w); err != nil {
				return err
			}
		}
		fmt.Fprintln(w)
	/*case "blip":
	// Traitement des images
	if id, ok := attr(node.Attrs, "embed"); ok {
		for _, rel := range zf.rels.Relationship {
			if id != rel.ID {
				continue
			}
			log.Infof("Blip Image found target is: %s\n", rel.Target) // Debug
			log.Infof("Blip Image found text %+v\n", rel)             // Debug
			if err := zf.extract(&rel, w, ""); err != nil {
				return err
			}
		}
	}*/
	case "pic":
		// manage images get image and description
		var imageDesc string
		for _, n := range node.Nodes {
			if n.XMLName.Local != "blipFill" && n.XMLName.Local != "nvPicPr" {
				continue
			}
			// loop arround subnode
			for _, nn := range n.Nodes {
				//var imageTitle, imageDesc string
				// get description
				if nn.XMLName.Local == "cNvPr" {
					//imageTitle, _ = attr(n.Attrs, "title")
					imageDesc, _ = attr(nn.Attrs, "descr")
				}
				// get imgage
				if nn.XMLName.Local == "blip" {
					if id, ok := attr(nn.Attrs, "embed"); ok {
						for _, rel := range zf.rels.Relationship {
							if id != rel.ID {
								continue
							}
							log.Infof("Blip Image found target is: %s\n", rel.Target) // Debug
							log.Infof("Blip Image found text %+v\n", rel)             // Debug
							if err := zf.extract(&rel, w, imageDesc); err != nil {
								return err
							}
						}
					}
				}
			}

		}
	case "Fallback":
		// Traitement des fallback
	case "txbxContent":
		// Traitement du contenu des boîtes de texte
		var cbuf bytes.Buffer
		for _, n := range node.Nodes {
			if err := zf.walk(&n, &cbuf); err != nil {
				return err
			}
		}
		fmt.Fprintln(w, "\n```\n"+cbuf.String()+"```")
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
	rc, err := f.Open()
	defer rc.Close()

	b, _ := io.ReadAll(rc)
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

	r, err := zip.OpenReader(arg)
	if err != nil {
		return "", tools.Metadata{}, err
	}
	defer r.Close()

	var rels Relationships
	var num Numbering
	var prop CoreProperties

	for _, f := range r.File {
		switch f.Name {
		case "word/_rels/document.xml.rels", "word/_rels/document2.xml.rels":
			rc, err := f.Open()
			defer rc.Close()

			b, _ := io.ReadAll(rc)
			if err != nil {
				return "", tools.Metadata{}, err
			}

			err = xml.Unmarshal(b, &rels)
			if err != nil {
				return "", tools.Metadata{}, err
			}
		case "word/numbering.xml":
			rc, err := f.Open()
			defer rc.Close()

			b, _ := io.ReadAll(rc)
			if err != nil {
				return "", tools.Metadata{}, err
			}

			err = xml.Unmarshal(b, &num)
			if err != nil {
				return "", tools.Metadata{}, err
			}
		case "docProps/core.xml":
			rc, err := f.Open()
			defer rc.Close()

			b, _ := io.ReadAll(rc)
			if err != nil {
				return "", tools.Metadata{}, err
			}

			err = xml.Unmarshal(b, &prop)
			if err != nil {
				return "", tools.Metadata{}, err
			}

		}
	}

	f := findFile(r.File, "word/document*.xml")
	if f == nil {
		return "", tools.Metadata{}, errors.New("incorrect document")
	}
	node, err := readFile(f)
	if err != nil {
		return "", tools.Metadata{}, err
	}

	var buf bytes.Buffer
	zf := &file{
		r:     r,
		rels:  rels,
		num:   num,
		embed: embed,
		list:  make(map[string]int),
	}
	err = zf.walk(node, &buf)
	if err != nil {
		return "", tools.Metadata{}, err
	}
	//fmt.Print(buf.String())
	log.Infof("Properties Title : %s\n", prop.Title)
	var authors []string

	meta := tools.Metadata{Title: prop.Title, Description: prop.Description, Authors: append(authors, prop.Creator)}

	return buf.String(), meta, nil
}

// Pptx2md convert a pptx file to markdown and add metadata header
func Pptx2md(pptxPath string, embed bool) (string, tools.Metadata, error) {
	// Ouvrir le fichier PPTX
	r, err := zip.OpenReader(pptxPath)
	if err != nil {
		return "", tools.Metadata{}, err
	}
	defer r.Close()

	// Initialiser les variables pour les relations et les propriétés
	var rels Relationships
	var prop CoreProperties

	// Lire les fichiers nécessaires dans le fichier PPTX
	for _, f := range r.File {
		log.Debugf("File: %s\n", f.Name)
		switch f.Name {
		//case "ppt/_rels/presentation.xml.rels", "ppt/slides/_rels/slide*.xml.rels":
		//case "ppt/slides/_rels/slide*.xml.rels":
		case "ppt/slides/_rels/slide1.xml.rels", "ppt/slides/_rels/slide2.xml.rels", "ppt/slides/_rels/slide3.xml.rels":
			rc, err := f.Open()
			defer rc.Close()
			if err != nil {
				return "", tools.Metadata{}, err
			}
			b, _ := io.ReadAll(rc)
			err = xml.Unmarshal(b, &rels)
			if err != nil {
				return "", tools.Metadata{}, err
			}
		case "docProps/core.xml":
			rc, err := f.Open()
			defer rc.Close()
			if err != nil {
				return "", tools.Metadata{}, err
			}
			b, _ := io.ReadAll(rc)
			err = xml.Unmarshal(b, &prop)
			if err != nil {
				return "", tools.Metadata{}, err
			}
		}
	}

	// Parcourir tous les fichiers de slides
	var buf bytes.Buffer
	for i := 1; ; i++ {
		slideName := fmt.Sprintf("ppt/slides/slide%d.xml", i)
		f := findFile(r.File, slideName)
		if f == nil {
			break
		}
		node, err := readFile(f)
		if err != nil {
			return "", tools.Metadata{}, err
		}

		// Convertir le contenu en Markdown
		zf := &file{
			r:     r,
			rels:  rels,
			embed: false,
			list:  make(map[string]int),
		}
		err = zf.walk(node, &buf)
		if err != nil {
			return "", tools.Metadata{}, err
		}
		buf.WriteString("\n---\n") // Séparateur entre les slides
	}

	// Ajouter les métadonnées
	var authors []string
	meta := tools.Metadata{Title: prop.Title, Description: prop.Description, Authors: append(authors, prop.Creator)}
	markdown := buf.String()

	return markdown, meta, nil
}

// GetDocx convert a docx file to markdown and add metadata header
func GetDocx(docxPath string, url string, customerId string, exportDir string, complements tools.Metadata) (tools.Page, error) {

	markdown, meta, err := Docx2md(docxPath, false)
	tools.CheckError(err)

	// Add metadata header to markdown with title , doc_id,description , tags, site_url, authors, creation_date, last_update
	metadata, metaDatas := tools.BuildFileMetadata(docxPath, url, customerId, meta, complements)

	// Add metadata header to markdown
	markdown = metadata + markdown

	exportedFile := exportDir + "/" + customerId + "-" + tools.BuildFilename(metaDatas.Title)
	// Écrire le Markdown dans un fichier
	err = tools.WriteMarkdownToFile(markdown, exportedFile)
	tools.CheckError(err)

	return tools.Page{PageId: metaDatas.Doc_id, Url: metaDatas.Site_url, MdFile: exportedFile}, nil
}

// GetPptx convert a pptx file to markdown and add metadata header
func GetPptx(pptxPath string, url string, customerId string, exportDir string, complements tools.Metadata) (tools.Page, error) {

	markdown, meta, err := Pptx2md(pptxPath, false)
	tools.CheckError(err)

	// Add metadata header to markdown with title , doc_id,description , tags, site_url, authors, creation_date, last_update
	metadata, metaDatas := tools.BuildFileMetadata(pptxPath, url, customerId, meta, complements)

	// Add metadata header to markdown
	markdown = metadata + markdown

	exportedFile := exportDir + "/" + customerId + "-" + tools.BuildFilename(metaDatas.Title)
	// Écrire le Markdown dans un fichier
	err = tools.WriteMarkdownToFile(markdown, exportedFile)
	tools.CheckError(err)

	return tools.Page{PageId: metaDatas.Doc_id, Url: metaDatas.Site_url, MdFile: exportedFile}, nil
}

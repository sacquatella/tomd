package docx2md

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (zf *file) extract(rel *Relationship, w io.Writer, desc string) error {
	target, err := pptxTarget(zf.owner, *rel)
	if err != nil {
		zf.warn("DOCX image relationship %s ignored: %v", rel.ID, err)
		return nil
	}
	f := findFile(zf.r.File, target)
	if f == nil {
		zf.warn("DOCX image %s is missing", target)
		return nil
	}
	data, err := readDocxPart(f, 20<<20)
	if err != nil {
		zf.warn("DOCX image %s unavailable: %v", target, err)
		return nil
	}
	mime := http.DetectContentType(data)
	if strings.EqualFold(path.Ext(target), ".svg") && (strings.HasPrefix(mime, "text/") || mime == "application/octet-stream") {
		mime = "image/svg+xml"
	}
	source := ""
	if zf.embed {
		source = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	} else {
		// Only a content hash and a whitelisted extension reach the filesystem.
		ext := strings.ToLower(path.Ext(target))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".emf", ".wmf", ".tif", ".tiff", ".bmp":
		default:
			ext = ".bin"
		}
		name := fmt.Sprintf("%x%s", sha256.Sum256(data), ext)
		if err := os.MkdirAll(zf.opts.AssetsDir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(zf.opts.AssetsDir, name), data, 0644); err != nil {
			return err
		}
		source = (&url.URL{Path: path.Join(filepath.ToSlash(zf.opts.AssetsPrefix), name)}).String()
	}
	fmt.Fprintf(w, "![%s](%s)", inlineMD(oneLine(desc)), escape(source, "()"))
	if zf.opts.Describer == nil {
		return nil
	}
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		zf.warn("DOCX image %s retained but not described: unsupported vision format %s (no rasterizer)", target, mime)
		return nil
	}
	contextText := zf.contextText
	if desc != "" {
		contextText += "\nImage alternative text: " + desc
	}
	description, err := zf.opts.Describer.Describe(zf.opts.Context, data, mime, zf.opts.Language, contextText)
	if err != nil {
		if cancelErr := zf.opts.Context.Err(); cancelErr != nil {
			return cancelErr
		}
		zf.warn("DOCX image description unavailable for %s: %v", target, err)
		fmt.Fprint(w, "\n\nAI-generated description unavailable; native content retained.\n\n")
		return nil
	}
	fmt.Fprintf(w, "\n\nAI-generated visual description:\n\n%s\n\n", description)
	return nil
}

func (zf *file) drawing(node *Node, w io.Writer) error {
	desc := ""
	for _, name := range []string{"docPr", "cNvPr"} {
		if props := first(node, name); props != nil {
			for _, key := range []string{"descr", "title"} {
				if value := nodeAttr(props, key); value != "" {
					desc = value
					break
				}
			}
		}
		if desc != "" {
			break
		}
	}
	var images, textBoxes []*Node
	var collect func(*Node)
	collect = func(n *Node) {
		switch n.XMLName.Local {
		case "txbxContent":
			textBoxes = append(textBoxes, n)
			return // Images inside text boxes are handled by their own walker.
		case "blip", "imagedata":
			images = append(images, n)
		}
		for i := range n.Nodes {
			collect(&n.Nodes[i])
		}
	}
	collect(node)
	for _, image := range images {
		id := relationshipID(image, "embed")
		if id == "" {
			id = relationshipID(image, "id") // Legacy VML images.
		}
		if id == "" {
			zf.warn("DOCX linked image ignored (external images are not fetched)")
			continue
		}
		found := false
		for i := range zf.rels.Relationship {
			rel := &zf.rels.Relationship[i]
			if rel.ID == id {
				found = true
				if err := zf.extract(rel, w, desc); err != nil {
					return err
				}
				break
			}
		}
		if !found {
			zf.warn("DOCX image relationship %s is missing", id)
		}
	}
	for _, textBox := range textBoxes {
		if err := zf.walk(textBox, w); err != nil {
			return err
		}
	}
	if len(images) == 0 && len(textBoxes) == 0 {
		zf.warn("DOCX drawing has no embedded image; native vector diagrams are not rendered")
	}
	return nil
}

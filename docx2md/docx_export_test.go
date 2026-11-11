package docx2md

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sacquatella/tomd/tools"
)

const docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" xmlns:v="urn:schemas-microsoft-com:vml" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`

func docxParagraph(text string) string { return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>` }

func docxPicture(id string) string {
	return `<w:r><w:rPr><w:b/></w:rPr><w:drawing><wp:inline><wp:docPr id="1" descr="Flux [A] vers B"/><a:graphic><a:graphicData><pic:pic><pic:nvPicPr><pic:cNvPr id="2"/></pic:nvPicPr><pic:blipFill><a:blip r:embed="` + id + `"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`
}

func docxFixture(t *testing.T, body string, parts map[string]string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "document.docx")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	all := map[string]string{"word/document.xml": `<w:document ` + docxNS + `><w:body>` + body + `</w:body></w:document>`}
	for name, data := range parts {
		all[name] = data
	}
	var names []string
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		part, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(all[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}

func docxImageParts(t *testing.T) map[string]string {
	t.Helper()
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"word/_rels/document.xml.rels": relsXML(relXML("img1", "image", "media/image.png")),
		"word/media/image.png":         string(png),
	}
}

func TestDocxStructure(t *testing.T) {
	body := `<w:p><w:pPr><w:pStyle w:val="Custom"/></w:pPr><w:r><w:t>Titre &amp; texte</w:t></w:r></w:p>` +
		`<w:p><w:r><w:rPr><w:b w:val="0"/><w:i w:val="false"/><w:strike w:val="off"/></w:rPr><w:t>[texte] &lt;test&gt; *a*</w:t><w:tab/><w:t>suite</w:t><w:br/><w:t>fin</w:t></w:r></w:p>` +
		`<w:p><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve"> gras </w:t></w:r><w:hyperlink r:id="link"><w:r><w:t>Lien</w:t></w:r></w:hyperlink></w:p>` +
		`<w:p><w:del><w:r><w:delText>Supprimé</w:delText></w:r></w:del><w:ins><w:r><w:t>Conservé</w:t></w:r></w:ins><w:r><w:instrText>PRIVATE FIELD</w:instrText></w:r></w:p>` +
		`<w:p><w:pPr><w:outlineLvl w:val="2"/></w:pPr><w:r><w:t>Direct</w:t></w:r></w:p>`
	filename := docxFixture(t, body, map[string]string{
		"word/styles.xml":              `<w:styles ` + docxNS + `><w:style w:styleId="Base"><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style><w:style w:styleId="Custom"><w:basedOn w:val="Base"/></w:style></w:styles>`,
		"word/_rels/document.xml.rels": relsXML(relXML("link", "hyperlink", "https://example.org/path")),
	})
	md, _, err := Docx2md(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Titre & texte\n\n", `\[texte\] \<test\> \*a\*`, "\tsuite  \nfin", " **gras** [Lien](https://example.org/path)", "Conservé", "### Direct"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	for _, unwanted := range []string{"&amp;", "Supprimé", "PRIVATE FIELD", "**[texte]"} {
		if strings.Contains(md, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, md)
		}
	}
}

func TestDocxTablesAndLists(t *testing.T) {
	list := func(level, text string) string {
		return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + level + `"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	}
	body := list("0", "Premier") + list("1", "Enfant") + list("0", "Second") +
		`<w:tbl><w:tr><w:tc>` + docxParagraph("A|B") + `</w:tc><w:tc/></w:tr><w:tr><w:tc>` + docxParagraph("Ligne 1") + docxParagraph("Ligne 2") + `</w:tc></w:tr></w:tbl>`
	filename := docxFixture(t, body, map[string]string{
		"word/numbering.xml": `<w:numbering ` + docxNS + `><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="3"/><w:numFmt w:val="decimal"/><w:pPr><w:ind w:left="1440"/></w:pPr></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`,
	})
	md, _, err := Docx2md(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(md, "3. Premier\n  * Enfant\n4. Second\n") {
		t.Errorf("bad list:\n%s", md)
	}
	if !strings.Contains(md, `A\|B`) || strings.Contains(md, `A\\|B`) {
		t.Errorf("bad pipe escaping:\n%s", md)
	}
	if !strings.Contains(md, "Ligne 1<br><br>Ligne 2") {
		t.Errorf("bad multiline cell:\n%s", md)
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "|-") && !strings.Contains(line, "|---|") {
			t.Errorf("empty column separator: %s", line)
		}
	}
}

func TestDocxExportAssets(t *testing.T) {
	t.Setenv("TOMD_AI_PROVIDER", "invalid-provider-not-used-without-ai")
	parts := docxImageParts(t)
	parts["docProps/core.xml"] = `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Mon document</dc:title><dc:creator>Auteur</dc:creator></cp:coreProperties>`
	filename := docxFixture(t, `<w:p>`+docxPicture("img1")+docxPicture("img1")+`</w:p>`, parts)
	exportDir := filepath.Join(t.TempDir(), "nouveau dossier")
	page, err := GetDocx(filename, "https://example.org", "docx", exportDir, tools.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(page.MdFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "title: Mon document") || !strings.Contains(string(md), "- Auteur") {
		t.Errorf("missing metadata: %s", md)
	}
	assetsDir := strings.TrimSuffix(page.MdFile, ".md") + "-assets"
	assets, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 {
		t.Fatalf("want one deduplicated asset, got %d", len(assets))
	}
	data, err := os.ReadFile(filepath.Join(assetsDir, assets[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte(parts["word/media/image.png"])) {
		t.Fatal("image bytes changed")
	}
	link := "![Flux \\[A\\] vers B](" + filepath.Base(assetsDir) + "/" + assets[0].Name() + ")"
	if strings.Count(string(md), link) != 2 {
		t.Errorf("bad image links: %s", md)
	}
	if strings.Contains(string(md), "AI-generated") || strings.Contains(string(md), "**![") {
		t.Errorf("unexpected image formatting: %s", md)
	}
	if _, err := os.Stat(strings.TrimSuffix(filename, ".docx") + "-assets"); !os.IsNotExist(err) {
		t.Fatal("assets were written beside source")
	}
}

type docxDescriber struct {
	calls                       int
	contextText, language, mime string
	err                         error
}

func (d *docxDescriber) Describe(ctx context.Context, image []byte, mime, language, contextText string) (string, error) {
	d.calls++
	d.contextText, d.language, d.mime = contextText, language, mime
	return "A envoie les données vers B.", d.err
}

func TestDocxImageDescriptions(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			filename := docxFixture(t, docxParagraph("Contexte précédent")+`<w:p>`+docxPicture("img1")+`</w:p>`+docxParagraph("Légende suivante"), docxImageParts(t))
			d := &docxDescriber{}
			if fail {
				d.err = errors.New("offline")
			}
			var warnings []string
			md, _, err := Docx2mdWithOptions(filename, DocxOptions{EmbedImages: true, Describer: d, Language: "French", Warn: func(s string) { warnings = append(warnings, s) }})
			if err != nil {
				t.Fatal(err)
			}
			if d.calls != 1 || d.language != "French" || d.mime != "image/png" {
				t.Fatalf("bad describer call: %+v", d)
			}
			for _, want := range []string{"Contexte précédent", "Légende suivante", "Flux [A] vers B"} {
				if !strings.Contains(d.contextText, want) {
					t.Errorf("missing %q in AI context %q", want, d.contextText)
				}
			}
			if !strings.Contains(md, "data:image/png;base64,") || strings.Contains(md, `!\[`) {
				t.Errorf("image missing or escaped: %s", md)
			}
			if fail {
				if len(warnings) != 1 || !strings.Contains(md, "description unavailable; native content retained") {
					t.Errorf("bad fallback: %s, %v", md, warnings)
				}
			} else if !strings.Contains(md, "AI-generated visual description:\n\nA envoie") {
				t.Errorf("description missing: %s", md)
			}
		})
	}
}

func TestDocxImageSafetyAndLegacy(t *testing.T) {
	for _, tc := range []struct{ name, target, mode string }{
		{"traversal", "../../outside.png", ""},
		{"external", "https://example.org/image.png", "External"},
		{"missing", "media/missing.png", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := map[string]string{"word/_rels/document.xml.rels": relsXML(`<Relationship Id="img1" Target="` + tc.target + `" TargetMode="` + tc.mode + `"/>`)}
			filename := docxFixture(t, docxParagraph("Texte conservé")+`<w:p>`+docxPicture("img1")+`</w:p>`, parts)
			d := &docxDescriber{}
			var warnings []string
			md, _, err := Docx2mdWithOptions(filename, DocxOptions{Describer: d, Warn: func(s string) { warnings = append(warnings, s) }})
			if err != nil {
				t.Fatal(err)
			}
			if d.calls != 0 || len(warnings) != 1 || !strings.Contains(md, "Texte conservé") || strings.Contains(md, "![") {
				t.Errorf("unsafe image: %s %v", md, warnings)
			}
		})
	}
	filename := docxFixture(t, `<w:p><mc:AlternateContent><mc:Choice Requires="v"><w:r><w:pict><v:shape/></w:pict></w:r></mc:Choice><mc:Fallback><w:r><w:pict><v:shape><v:imagedata r:id="img1"/></v:shape></w:pict></w:r></mc:Fallback></mc:AlternateContent></w:p>`, docxImageParts(t))
	md, _, err := Docx2md(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(md, "data:image/png;base64,") != 1 {
		t.Errorf("legacy fallback missing/duplicated: %s", md)
	}
}

func TestDocxUnsupportedImage(t *testing.T) {
	parts := map[string]string{
		"word/_rels/document.xml.rels": relsXML(relXML("img1", "image", "media/image.svg")),
		"word/media/image.svg":         `<svg xmlns="http://www.w3.org/2000/svg"/>`,
	}
	filename := docxFixture(t, `<w:p>`+docxPicture("img1")+`</w:p>`, parts)
	d := &docxDescriber{}
	warnings := 0
	md, _, err := Docx2mdWithOptions(filename, DocxOptions{Describer: d, Warn: func(string) { warnings++ }})
	if err != nil {
		t.Fatal(err)
	}
	if d.calls != 0 || warnings != 1 || !strings.Contains(md, ".svg)") {
		t.Errorf("vector image lost or sent to AI: %s", md)
	}
}

func TestDocxTextBoxImage(t *testing.T) {
	body := `<w:p><w:r><w:pict><v:shape><w:txbxContent>` + docxParagraph("Dans la zone de texte") + `<w:p>` + docxPicture("img1") + `</w:p></w:txbxContent></v:shape></w:pict></w:r></w:p>`
	filename := docxFixture(t, body, docxImageParts(t))
	md, _, err := Docx2md(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(md, "data:image/png;base64,") != 1 || !strings.Contains(md, "Dans la zone de texte") || strings.Contains(md, "```") {
		t.Errorf("text box content lost, duplicated or turned into code: %s", md)
	}
}

func TestDocxPartSizeLimit(t *testing.T) {
	f := &zip.File{FileHeader: zip.FileHeader{Name: "word/media/large.png", UncompressedSize64: 21 << 20}}
	if _, err := readDocxPart(f, 20<<20); err == nil {
		t.Fatal("oversized ZIP parts must be rejected before reading")
	}
}

func TestDocxPreformattedCode(t *testing.T) {
	body := `<w:p><w:pPr><w:pStyle w:val="PrformatHTML"/></w:pPr><w:r><w:t>func main() {</w:t><w:br/><w:tab/><w:t>value := "*test*"</w:t><w:br/><w:t>}</w:t></w:r></w:p>`
	filename := docxFixture(t, body, nil)
	md, _, err := Docx2md(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	if md != "    func main() {\n    \tvalue := \"*test*\"\n    }\n\n" {
		t.Errorf("code altered: %q", md)
	}
}

func TestDocxAIProviders(t *testing.T) {
	for _, provider := range []string{"ollama", "openai"} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost {
					t.Errorf("method %s", r.Method)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["model"] != "vision-test" {
					t.Error("wrong model")
				}
				w.Header().Set("Content-Type", "application/json")
				if provider == "ollama" {
					if r.URL.Path != "/api/generate" {
						t.Error("wrong Ollama endpoint")
					}
					_, _ = w.Write([]byte(`{"response":"Description locale"}`))
				} else {
					if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
						t.Error("wrong OpenAI request")
					}
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Description distante"}}]}`))
				}
			}))
			defer server.Close()
			d, err := tools.NewImageDescriber(tools.AIConfig{Provider: provider, Model: "vision-test", APIKey: "test-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			filename := docxFixture(t, `<w:p>`+docxPicture("img1")+docxPicture("img1")+`</w:p>`, docxImageParts(t))
			md, _, err := Docx2mdWithOptions(filename, DocxOptions{EmbedImages: true, Describer: d})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || strings.Count(md, "AI-generated visual description:") != 2 {
				t.Errorf("bad caching/descriptions: %d, %s", calls, md)
			}
			if strings.Contains(md, "test-key") {
				t.Fatal("API key leaked")
			}
		})
	}
}

func TestDocxErrorsAndCustomAssets(t *testing.T) {
	if _, err := GetDocx("missing.docx", "", "docx", t.TempDir(), tools.Metadata{}); err == nil {
		t.Fatal("expected missing-file error")
	}
	filename := docxFixture(t, "", map[string]string{"word/document.xml": "<invalid>"})
	if _, _, err := Docx2md(filename, true); err == nil {
		t.Fatal("expected XML error")
	}
	filename = docxFixture(t, docxParagraph("Text"), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Docx2mdWithOptions(filename, DocxOptions{Context: ctx}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	filename = docxFixture(t, `<w:p>`+docxPicture("img1")+`</w:p>`, docxImageParts(t))
	assets := filepath.Join(t.TempDir(), "mes images")
	md, _, err := Docx2mdWithOptions(filename, DocxOptions{AssetsDir: assets, AssetsPrefix: "mes images"})
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(md, "](mes%20images/")
	if start < 0 {
		t.Fatalf("expected URL-encoded relative link: %s", md)
	}
	link := md[start+2 : strings.Index(md[start:], ")")+start]
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(assets), filepath.FromSlash(u.Path))); err != nil {
		t.Fatal(err)
	}
}

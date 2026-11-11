package docx2md

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sacquatella/tomd/tools"
)

const pptxNS = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:dgm="http://schemas.openxmlformats.org/drawingml/2006/diagram"`

func testPptx(t *testing.T, parts map[string]string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "presentation.pptx")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		part, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(parts[name])); err != nil {
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

func relXML(id, kind, target string) string {
	return fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/%s" Target="%s"/>`, id, kind, target)
}

func relsXML(content string) string {
	return `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + content + `</Relationships>`
}

func slideXML(content string) string {
	return `<p:sld ` + pptxNS + `><p:cSld><p:spTree>` + content + `</p:spTree></p:cSld></p:sld>`
}

func shapeXML(id, placeholder, paragraphs string) string {
	return `<p:sp><p:nvSpPr><p:cNvPr id="` + id + `" name="Shape ` + id + `"/><p:nvPr>` + placeholder + `</p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/>` + paragraphs + `</p:txBody></p:sp>`
}

func paraXML(text string) string { return `<a:p><a:r><a:t>` + text + `</a:t></a:r></a:p>` }

func baseParts(order ...int) map[string]string {
	ids, rels := "", ""
	parts := map[string]string{}
	for i, number := range order {
		ids += fmt.Sprintf(`<p:sldId id="%d" r:id="r%d"/>`, 256+i, i+1)
		rels += relXML(fmt.Sprintf("r%d", i+1), "slide", fmt.Sprintf("slides/slide%d.xml", number))
		parts[fmt.Sprintf("ppt/slides/slide%d.xml", number)] = slideXML(shapeXML("2", `<p:ph type="title"/>`, paraXML(fmt.Sprintf("Title %d", number))))
	}
	parts["ppt/presentation.xml"] = `<p:presentation ` + pptxNS + `><p:sldIdLst>` + ids + `</p:sldIdLst></p:presentation>`
	parts["ppt/_rels/presentation.xml.rels"] = relsXML(rels)
	return parts
}

func TestPptxPresentationOrderAndGaps(t *testing.T) {
	parts := baseParts(9, 1, 4, 7)
	filename := testPptx(t, parts)
	md, meta, err := Pptx2md(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "presentation" {
		t.Errorf("fallback title = %s", meta.Title)
	}
	previous := -1
	for i, number := range []int{9, 1, 4, 7} {
		index := strings.Index(md, fmt.Sprintf("## Slide %d — Title %d", i+1, number))
		if index <= previous {
			t.Fatalf("slides missing or out of order:\n%s", md)
		}
		previous = index
	}
}

func TestPptxTextListsLinksAndNotes(t *testing.T) {
	parts := baseParts(1, 2)
	paragraphs := `<a:p><a:r><a:rPr b="0" i="false" strike="noStrike"/><a:t>A &amp; B</a:t></a:r><a:br/><a:r><a:rPr b="1"/><a:t> bold </a:t></a:r></a:p>` +
		`<a:p><a:pPr><a:buChar char="•"/></a:pPr><a:r><a:t>Parent</a:t></a:r></a:p>` +
		`<a:p><a:pPr lvl="1"><a:buAutoNum type="arabicPeriod" startAt="3"/></a:pPr><a:r><a:t>Child</a:t></a:r></a:p>` +
		`<a:p><a:pPr lvl="1"><a:buAutoNum type="arabicPeriod"/></a:pPr><a:r><a:rPr><a:hlinkClick r:id="web"/></a:rPr><a:t>Link</a:t></a:r></a:p>` +
		`<a:p><a:r><a:rPr><a:hlinkClick r:id="next"/></a:rPr><a:t>Next</a:t></a:r></a:p>`
	parts["ppt/slides/slide1.xml"] = slideXML(shapeXML("2", `<p:ph type="title"/>`, paraXML("Architecture")) + shapeXML("3", "", paragraphs) + shapeXML("4", `<p:ph type="ftr"/>`, paraXML("Repeated footer")))
	parts["ppt/slides/_rels/slide1.xml.rels"] = relsXML(relXML("notes", "notesSlide", "../notesSlides/notesSlide1.xml") + `<Relationship Id="web" Type="hyperlink" Target="https://example.com/a(b)" TargetMode="External"/>` + relXML("next", "slide", "slide2.xml"))
	parts["ppt/notesSlides/notesSlide1.xml"] = slideXML(shapeXML("1", `<p:ph type="body"/>`, paraXML("Asynchronous processing")) + shapeXML("2", `<p:ph type="sldNum"/>`, paraXML("99")))
	filename := testPptx(t, parts)
	md, _, err := Pptx2md(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"A & B<br> **bold**", "- Parent", "    3. Child", "    4. [Link](https://example.com/a%28b%29)", "[Next](#slide2)", `<a id="slide2"></a>`, "### Presenter notes", "Asynchronous processing", "block: slide1-3"} {
		if !strings.Contains(md, expected) {
			t.Errorf("missing %q:\n%s", expected, md)
		}
	}
	for _, unwanted := range []string{"Repeated footer", "99", "**A", "&amp;"} {
		if strings.Contains(md, unwanted) {
			t.Errorf("unexpected %q", unwanted)
		}
	}
	md, _, err = Pptx2mdWithOptions(filename, PptxOptions{EmbedImages: true, ExcludeNotes: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "Asynchronous processing") {
		t.Error("notes not excluded")
	}
}

type testDescriber struct {
	calls   int
	fail    bool
	context string
}

func (d *testDescriber) Describe(ctx context.Context, image []byte, mime, lang, source string) (string, error) {
	d.calls++
	d.context = source
	if d.fail {
		return "", errors.New("service unavailable")
	}
	return "Portal sends requests to a worker.", nil
}

func TestPptxImagesUseSlideRelationshipsAndExportDir(t *testing.T) {
	parts := baseParts(1, 2, 3, 4)
	for _, number := range []int{1, 4} {
		parts[fmt.Sprintf("ppt/slides/slide%d.xml", number)] = slideXML(`<p:pic><p:nvPicPr><p:cNvPr id="3" name="Diagram" descr="Flow"/></p:nvPicPr><p:blipFill><a:blip r:embed="rImg"/></p:blipFill></p:pic>` + shapeXML("5", "", paraXML(fmt.Sprintf("Context %d", number))))
		parts[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", number)] = relsXML(relXML("rImg", "image", fmt.Sprintf("../media/image%d.png", number)))
		parts[fmt.Sprintf("ppt/media/image%d.png", number)] = fmt.Sprintf("\x89PNG\r\n\x1a\nimage%d", number)
	}
	filename := testPptx(t, parts)
	d := &testDescriber{}
	md, _, err := Pptx2mdWithOptions(filename, PptxOptions{EmbedImages: true, Describer: d})
	if err != nil {
		t.Fatal(err)
	}
	if d.calls != 2 || !strings.Contains(d.context, "Context 4") {
		t.Fatalf("image descriptions: calls=%d, context=%s", d.calls, d.context)
	}
	if strings.Count(md, "data:image/png;base64,") != 2 {
		t.Error("embed flag ignored or fourth slide image lost")
	}
	if !strings.Contains(md, "AI-generated visual description:") {
		t.Error("AI provenance missing")
	}
	exportDir := filepath.Join(t.TempDir(), "nested", "export")
	page, err := GetPptxWithOptions(filename, "", "test", exportDir, tools.Metadata{}, PptxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(page.MdFile)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := filepath.Glob(filepath.Join(exportDir, "*-assets", "*.png"))
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected 2 images under output dir, got %v", assets)
	}
	for _, asset := range assets {
		relative, _ := filepath.Rel(exportDir, asset)
		if !strings.Contains(string(data), filepath.ToSlash(relative)) {
			t.Errorf("image link missing: %s", relative)
		}
	}
	if _, err := os.Stat(strings.TrimSuffix(filename, ".pptx") + "-assets"); !os.IsNotExist(err) {
		t.Error("export wrote assets next to source")
	}
	d.fail = true
	var warnings []string
	md, _, err = Pptx2mdWithOptions(filename, PptxOptions{EmbedImages: true, Describer: d, Warn: func(s string) { warnings = append(warnings, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || !strings.Contains(md, "Context 4") || !strings.Contains(md, "description unavailable") {
		t.Error("AI failure should retain native content and warn")
	}
}

func TestPptxNativeTableDiagramAndChart(t *testing.T) {
	parts := baseParts(1)
	cell := func(text string) string { return `<a:tc><a:txBody>` + paraXML(text) + `</a:txBody></a:tc>` }
	table := `<p:graphicFrame><a:graphic><a:graphicData><a:tbl><a:tblPr firstRow="1"/><a:tr>` + cell("Name") + cell("Value") + `</a:tr><a:tr>` + cell("A|B") + cell("2") + `</a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
	connector := `<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="8"/><p:cNvCxnSpPr><a:stCxn id="3"/><a:endCxn id="4"/></p:cNvCxnSpPr></p:nvCxnSpPr><p:spPr><a:ln><a:tailEnd type="triangle"/></a:ln></p:spPr></p:cxnSp>`
	parts["ppt/slides/slide1.xml"] = slideXML(shapeXML("3", "", paraXML("Portal")) + shapeXML("4", "", paraXML("Worker")) + connector + table + `<p:graphicFrame><a:graphic><a:graphicData><c:chart r:id="chart"/></a:graphicData></a:graphic></p:graphicFrame><p:graphicFrame><a:graphic><a:graphicData><dgm:relIds r:dm="diagram"/></a:graphicData></a:graphic></p:graphicFrame>`)
	parts["ppt/slides/_rels/slide1.xml.rels"] = relsXML(relXML("chart", "chart", "../charts/chart1.xml") + relXML("diagram", "diagramData", "../diagrams/data1.xml"))
	parts["ppt/charts/chart1.xml"] = `<c:chartSpace ` + pptxNS + `><c:chart><c:plotArea><c:barChart><c:ser><c:tx><c:v>Revenue</c:v></c:tx><c:cat><c:strLit><c:pt idx="0"><c:v>2025</c:v></c:pt></c:strLit></c:cat><c:val><c:numLit><c:formatCode>0.00</c:formatCode><c:pt idx="0"><c:v>42</c:v></c:pt></c:numLit></c:val></c:ser></c:barChart></c:plotArea></c:chart></c:chartSpace>`
	parts["ppt/diagrams/data1.xml"] = `<dgm:dataModel ` + pptxNS + `><dgm:ptLst><dgm:pt modelId="a"><dgm:t>` + paraXML("Input") + `</dgm:t></dgm:pt><dgm:pt modelId="b"><dgm:t>` + paraXML("Output") + `</dgm:t></dgm:pt></dgm:ptLst><dgm:cxnLst><dgm:cxn srcId="a" destId="b" type="parOf"/></dgm:cxnLst></dgm:dataModel>`
	md, _, err := Pptx2md(testPptx(t, parts), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Relation: Portal → Worker", "| Name | Value |", "| --- | --- |", `A\|B`, "Series: Revenue", "| 2025 | 42 |", "Input → Output", "not inferred visual arrows"} {
		if !strings.Contains(md, expected) {
			t.Errorf("missing %q:\n%s", expected, md)
		}
	}
}

func TestPptxHiddenAndLayoutInheritance(t *testing.T) {
	parts := baseParts(1, 2)
	parts["ppt/slides/slide1.xml"] = strings.Replace(slideXML(shapeXML("2", `<p:ph idx="1"/>`, paraXML("Inherited title"))+shapeXML("3", `<p:ph idx="2"/>`, paraXML("Inherited bullet"))), "<p:sld ", `<p:sld show="0" `, 1)
	parts["ppt/slides/_rels/slide1.xml.rels"] = relsXML(relXML("layout", "slideLayout", "../slideLayouts/slideLayout1.xml"))
	parts["ppt/slideLayouts/slideLayout1.xml"] = slideXML(shapeXML("1", `<p:ph idx="1" type="title"/>`, "") + strings.Replace(shapeXML("2", `<p:ph idx="2" type="body"/>`, ""), `<a:lstStyle/>`, `<a:lstStyle><a:lvl1pPr><a:buChar char="•"/></a:lvl1pPr></a:lstStyle>`, 1))
	parts["ppt/slides/slide2.xml"] = slideXML(shapeXML("5", `<p:ph idx="3"/>`, paraXML("Master bullet")))
	parts["ppt/slides/_rels/slide2.xml.rels"] = relsXML(relXML("layout", "slideLayout", "../slideLayouts/slideLayout1.xml"))
	parts["ppt/slideLayouts/slideLayout1.xml"] = strings.Replace(parts["ppt/slideLayouts/slideLayout1.xml"], "</p:spTree>", shapeXML("3", `<p:ph idx="3"/>`, "")+"</p:spTree>", 1)
	parts["ppt/slideLayouts/_rels/slideLayout1.xml.rels"] = relsXML(relXML("master", "slideMaster", "../slideMasters/slideMaster1.xml"))
	parts["ppt/slideMasters/slideMaster1.xml"] = `<p:sldMaster ` + pptxNS + `><p:txStyles><p:bodyStyle><a:lvl1pPr><a:buChar char="•"/></a:lvl1pPr></p:bodyStyle></p:txStyles></p:sldMaster>`
	filename := testPptx(t, parts)
	md, _, err := Pptx2md(filename, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "## Slide 1 — Inherited title") || !strings.Contains(md, "- Inherited bullet") || !strings.Contains(md, "Hidden slide: yes") {
		t.Fatalf("inheritance missing:\n%s", md)
	}
	if !strings.Contains(md, "- Master bullet") {
		t.Errorf("master list style not inherited:\n%s", md)
	}
	md, _, err = Pptx2mdWithOptions(filename, PptxOptions{EmbedImages: true, ExcludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "Inherited title") || !strings.Contains(md, "## Slide 2") {
		t.Errorf("hidden filtering changed source slide numbering:\n%s", md)
	}
}

func TestPptxMalformedAndUnsafeRelationships(t *testing.T) {
	for _, target := range []string{"../../../escape.png", "https://example.com/image.png", `..\media\image.png`} {
		if _, err := pptxTarget("ppt/slides/slide1.xml", Relationship{Target: target}); err == nil {
			t.Errorf("accepted unsafe target %q", target)
		}
	}
	if target, err := pptxTarget("ppt/slides/slide1.xml", Relationship{Target: "../media/image.png"}); err != nil || target != "ppt/media/image.png" {
		t.Fatalf("valid relative relationship: %q %v", target, err)
	}
	parts := baseParts(1)
	parts["ppt/slides/slide1.xml"] = "<broken"
	if _, _, err := Pptx2md(testPptx(t, parts), true); err == nil {
		t.Error("malformed XML accepted")
	}
	parts = baseParts(1)
	delete(parts, "ppt/slides/slide1.xml")
	if _, _, err := Pptx2md(testPptx(t, parts), true); err == nil {
		t.Error("missing slide silently ignored")
	}
}

func TestPptxSample(t *testing.T) {
	md, _, err := Pptx2md("../samples/test.pptx", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "## Slide 1") {
		t.Fatal("sample has no slide content")
	}
	for _, expected := range []string{"## Slide 4", "- One", "- Two", "- Three", "| Colonne 1 |", "data:image/png;base64,"} {
		if !strings.Contains(md, expected) {
			t.Errorf("sample lost %q", expected)
		}
	}
}

func TestPptxRunWhitespaceAndFalseStyles(t *testing.T) {
	var n Node
	if err := xml.Unmarshal([]byte(`<r><rPr b="0" i="0" strike="noStrike"/><t> literal </t></r>`), &n); err != nil {
		t.Fatal(err)
	}
	p := &pptxReader{}
	if got := p.run(&n, "", nil, nil); got != " literal " {
		t.Errorf("got %q", got)
	}
}

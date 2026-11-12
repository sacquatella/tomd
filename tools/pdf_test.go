package tools

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

// TestExtractTextFromPDF_ValidPdf test reads a PDF file and converts it to Markdown
func TestExtractTextFromPDF_ValidPdf(t *testing.T) {

	pdfFile := "../samples/test.pdf"
	expectedMarkdown := "Exemple de texte en HTML"

	result, err := ExtractTextFromPDF(pdfFile)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.Contains(result, expectedMarkdown) {
		t.Errorf("expected contains %s, but not in %s", expectedMarkdown, result)
	}

}

func writeTestPDF(t *testing.T, contents string, streams ...string) string {
	t.Helper()
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents %s >>", contents),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	for _, stream := range streams {
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	return writePDFObjects(t, objects)
}

func writePDFObjects(t *testing.T, objects []string) string {
	t.Helper()
	var document strings.Builder
	document.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for index, object := range objects {
		offsets[index] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&document, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	path := filepath.Join(t.TempDir(), "test.pdf")
	if err := os.WriteFile(path, []byte(document.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractTextFromPDF_ContentStreams(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		streams  []string
		want     string
	}{
		{"single", "5 0 R", []string{"BT /F1 12 Tf 72 720 Td (Hello) Tj 0 -14 Td (World) Tj ET"}, "Hello World"},
		{"multiple", "[5 0 R 6 0 R]", []string{"BT /F1 12 Tf 72 720 Td (Hello) Tj\n", "0 -14 Td (World) Tj ET"}, "Hello World"},
		{"blank", "null", nil, ""},
		{"empty array", "[]", nil, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := ExtractTextFromPDF(writeTestPDF(t, test.contents, test.streams...))
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if result != "" {
					t.Fatalf("expected empty page, got %q", result)
				}
			} else if !strings.Contains(result, "<!-- Page 1 -->") || !strings.Contains(result, test.want) {
				t.Fatalf("expected page heading and %q, got %q", test.want, result)
			}
		})
	}
}

func TestPDFHeadingsAndParagraphs(t *testing.T) {
	texts := []pdf.Text{
		{S: "Document title", FontSize: 24, X: 72, Y: 720, W: 160},
		{S: "First paragraph wraps across", FontSize: 12, X: 72, Y: 680, W: 180},
		{S: "two lines.", FontSize: 12, X: 72, Y: 666, W: 60},
		{S: "Another para-", FontSize: 12, X: 72, Y: 636, W: 80},
		{S: "graph.", FontSize: 12, X: 72, Y: 622, W: 40},
		{S: "1. Section", FontSize: 12, Font: "Helvetica-Bold", X: 72, Y: 592, W: 80},
		{S: "Section body.", FontSize: 12, X: 72, Y: 570, W: 80},
		{S: "1.1 Subsection", FontSize: 12, X: 72, Y: 540, W: 80},
		{S: "Subsection body.", FontSize: 12, X: 72, Y: 520, W: 90},
	}
	got := renderPDFText(texts)
	want := "# Document title\n\nFirst paragraph wraps across two lines.\n\nAnother paragraph.\n\n## 1. Section\n\nSection body.\n\n### 1.1 Subsection\n\nSubsection body.\n\n"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPDFColumnOrder(t *testing.T) {
	texts := []pdf.Text{{S: "Document title", FontSize: 24, X: 72, Y: 720, W: 400}}
	for index := 0; index < 4; index++ {
		texts = append(texts,
			pdf.Text{S: fmt.Sprintf("Left%d", index), FontSize: 12, X: 72, Y: 680 - float64(index)*14, W: 228},
			pdf.Text{S: fmt.Sprintf("Right%d", index), FontSize: 12, X: 322, Y: 680 - float64(index)*14, W: 228},
		)
	}
	want := "# Document title\n\nLeft0 Left1 Left2 Left3\n\nRight0 Right1 Right2 Right3\n\n"
	if got := renderPDFText(texts); got != want {
		t.Fatalf("expected column reading order %q, got %q", want, got)
	}
}

func TestGetPDF_Images(t *testing.T) {
	pixels := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255}
	compress := func(data []byte) []byte {
		var buffer bytes.Buffer
		writer := zlib.NewWriter(&buffer)
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	stream := func(dictionary string, data []byte) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dictionary, len(data), data)
	}
	tests := []struct {
		name                             string
		jpeg, mask, form, only, repeated bool
	}{
		{name: "PNG"},
		{name: "JPEG", jpeg: true},
		{name: "transparent", mask: true},
		{name: "nested form", form: true},
		{name: "image only", only: true},
		{name: "repeated image", repeated: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, filter := compress(pixels), "FlateDecode"
			if test.jpeg {
				bitmap := image.NewRGBA(image.Rect(0, 0, 2, 2))
				for index := 0; index < 4; index++ {
					bitmap.Set(index%2, index/2, color.RGBA{R: pixels[index*3], G: pixels[index*3+1], B: pixels[index*3+2], A: 255})
				}
				var buffer bytes.Buffer
				if err := jpeg.Encode(&buffer, bitmap, nil); err != nil {
					t.Fatal(err)
				}
				data, filter = buffer.Bytes(), "DCTDecode"
			}
			mask := ""
			if test.mask {
				mask = "/SMask 7 0 R"
			}
			objects := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> /XObject << /Im1 5 0 R >> >> /Contents 6 0 R >>",
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
				stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Filter /%s %s", filter, mask), data),
			}
			content := "BT /F1 24 Tf 72 740 Td (Title) Tj /F1 12 Tf 0 -90 Td (Before image.) Tj 0 -120 Td (After image.) Tj ET\n"
			if test.only {
				content = ""
			}
			if test.form {
				objects[2] = strings.Replace(objects[2], "/Im1 5 0 R", "/Fm1 7 0 R", 1)
				content += "q 1 0 0 1 72 560 cm /Fm1 Do Q"
			} else {
				content += "q 120 0 0 60 72 560 cm /Im1 Do Q"
			}
			if test.repeated {
				content += "\nq 120 0 0 60 72 300 cm /Im1 Do Q"
			}
			objects = append(objects, stream("", []byte(content)))
			if test.mask {
				objects = append(objects, stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceGray /Filter /FlateDecode", compress([]byte{255, 128, 0, 255})))
			}
			if test.form {
				objects = append(objects, stream("/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Matrix [120 0 0 60 0 0] /Resources << /XObject << /Inner 5 0 R >> >>", []byte("/Inner Do")))
			}
			path := writePDFObjects(t, objects)
			exportDir := filepath.Join(t.TempDir(), "with spaces")
			if err := os.MkdirAll(exportDir, 0755); err != nil {
				t.Fatal(err)
			}
			page, err := GetPDF(path, "", "pdf", exportDir, Metadata{})
			if err != nil {
				t.Fatal(err)
			}
			markdown, err := os.ReadFile(page.MdFile)
			if err != nil {
				t.Fatal(err)
			}
			links := regexp.MustCompile(`!\[Image\]\(([^)]+)\)`).FindAllStringSubmatch(string(markdown), -1)
			wantCount := 1
			if test.repeated {
				wantCount = 2
			}
			if len(links) != wantCount {
				t.Fatalf("expected %d image links, got %s", wantCount, markdown)
			}
			link, err := url.Parse(links[0][1])
			if err != nil || link.IsAbs() || filepath.IsAbs(link.Path) {
				t.Fatalf("expected relative image link, got %q", links[0][1])
			}
			asset := filepath.Join(exportDir, filepath.FromSlash(link.Path))
			file, err := os.Open(asset)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			bitmap, _, err := image.Decode(file)
			if err != nil || bitmap.Bounds().Dx() != 2 || bitmap.Bounds().Dy() != 2 {
				t.Fatalf("expected decodable 2x2 image, got %v", err)
			}
			if test.mask {
				_, _, _, alpha := bitmap.At(1, 0).RGBA()
				if alpha != 128*257 {
					t.Fatalf("expected preserved alpha, got %d", alpha)
				}
			}
			entries, err := os.ReadDir(filepath.Dir(asset))
			if err != nil || len(entries) != 1 {
				t.Fatalf("expected one deduplicated asset, got %v, %v", entries, err)
			}
			if !test.only {
				before := strings.Index(string(markdown), "Before image.")
				after := strings.Index(string(markdown), "After image.")
				position := strings.Index(string(markdown), "![Image]")
				if before < 0 || after < 0 || !(before < position && position < after) {
					t.Fatalf("image not placed between paragraphs: %s", markdown)
				}
			}
		})
	}
}

func TestExtractTextFromPDF_Errors(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "invalid.pdf")
	if err := os.WriteFile(invalid, []byte("not a PDF"), 0644); err != nil {
		t.Fatal(err)
	}
	malformed := writeTestPDF(t, "5 0 R", "BT /F1 12 Tf 72 720 Td (Partial) Tj /F1 Tf ET")
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.pdf"), invalid, malformed} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			result, err := ExtractTextFromPDF(path)
			if err == nil || result != "" {
				t.Fatalf("expected error without partial output, got %q, %v", result, err)
			}
			exportDir := t.TempDir()
			if _, err := GetPDF(path, "", "pdf", exportDir, Metadata{}); err == nil {
				t.Fatal("expected GetPDF to return extraction error")
			}
			entries, err := os.ReadDir(exportDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("expected no exported files, got %v, %v", entries, err)
			}
		})
	}
}

func TestGetPDF_WriteError(t *testing.T) {
	exportDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(exportDir, nil, 0644); err != nil {
		t.Fatal(err)
	}
	path := writeTestPDF(t, "5 0 R", "BT /F1 12 Tf 72 720 Td (Hello) Tj ET")
	if _, err := GetPDF(path, "", "pdf", exportDir, Metadata{}); err == nil {
		t.Fatal("expected Markdown write error")
	}
}

func TestGetPDFWithOptions_AI(t *testing.T) {
	for _, test := range []struct {
		name, provider string
		enabled, fail  bool
	}{
		{name: "disabled", provider: "ollama"},
		{name: "ollama", provider: "ollama", enabled: true},
		{name: "openai", provider: "openai", enabled: true},
		{name: "failure", provider: "ollama", enabled: true, fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				var payload struct {
					Prompt   string `json:"prompt"`
					Messages []struct {
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				prompt := payload.Prompt
				if test.provider == "openai" && len(payload.Messages) > 0 && len(payload.Messages[0].Content) > 0 {
					prompt = payload.Messages[0].Content[0].Text
				}
				if !strings.Contains(prompt, "French") || !strings.Contains(prompt, "Page 1") || !strings.Contains(prompt, "Titre One") {
					t.Errorf("missing language or PDF context: %s", prompt)
				}
				if test.fail {
					writer.WriteHeader(http.StatusUnauthorized)
					return
				}
				if test.provider == "openai" {
					fmt.Fprint(writer, `{"choices":[{"message":{"content":"Description du schema."}}]}`)
				} else {
					fmt.Fprint(writer, `{"response":"Description du schema."}`)
				}
			}))
			defer server.Close()
			options := PDFOptions{Language: "French"}
			var warnings []string
			options.Warn = func(message string) { warnings = append(warnings, message) }
			if test.enabled {
				describer, err := NewImageDescriber(AIConfig{Provider: test.provider, Model: "vision-test", APIKey: "test-key", BaseURL: server.URL})
				if err != nil {
					t.Fatal(err)
				}
				options.Describer = describer
			}
			page, err := GetPDFWithOptions("../samples/test.pdf", "", "pdf", t.TempDir(), Metadata{}, options)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(page.MdFile)
			if err != nil {
				t.Fatal(err)
			}
			markdown := string(data)
			if !strings.Contains(markdown, "# Titre One") || strings.Count(markdown, "![Image]") != 2 {
				t.Fatalf("native content missing: %s", markdown)
			}
			if !test.enabled {
				if calls != 0 || strings.Contains(markdown, "AI-generated") {
					t.Fatal("AI was used while disabled")
				}
			} else if test.fail {
				if calls == 0 || len(warnings) == 0 || strings.Count(markdown, "AI-generated description unavailable; native content retained.") != 2 {
					t.Fatalf("missing AI failure fallback: calls=%d, warnings=%v, markdown=%s", calls, warnings, markdown)
				}
			} else if calls != 1 || len(warnings) != 0 || strings.Count(markdown, "AI-generated visual description:\n\nDescription du schema.") != 2 {
				t.Fatalf("missing descriptions or cache: calls=%d, warnings=%v, markdown=%s", calls, warnings, markdown)
			}
		})
	}
}

func TestGetPDF_JPEG2000(t *testing.T) {
	// JPX payloads are opaque to extraction: no JPEG 2000 decoder is needed.
	jpx := []byte("\x00\x00\x00\x0cjP  \r\n\x87\nopaque JPEG 2000 payload")
	stream := func(attributes string, data []byte) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", attributes, len(data), data)
	}
	input := writePDFObjects(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> /XObject << /JPX 5 0 R /RGB 7 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Filter /JPXDecode", jpx),
		stream("", []byte("BT /F1 12 Tf 72 720 Td (Native text survives JPEG 2000.) Tj ET\nq 60 0 0 60 72 600 cm /JPX Do Q\nq 60 0 0 60 72 500 cm /RGB Do Q\nq 60 0 0 60 72 400 cm /JPX Do Q")),
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceRGB", []byte{255, 0, 0}),
	})
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("AI=%t", enabled), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				fmt.Fprint(w, `{"response":"Compatible image description."}`)
			}))
			defer server.Close()
			var warnings []string
			options := PDFOptions{Warn: func(message string) { warnings = append(warnings, message) }}
			if enabled {
				describer, err := NewImageDescriber(AIConfig{Provider: "ollama", Model: "test", BaseURL: server.URL})
				if err != nil {
					t.Fatal(err)
				}
				options.Describer = describer
			}
			exportDir := filepath.Join(t.TempDir(), "output with spaces")
			if err := os.MkdirAll(exportDir, 0755); err != nil {
				t.Fatal(err)
			}
			var page Page
			var err error
			if enabled {
				page, err = GetPDFWithOptions(input, "", "pdf", exportDir, Metadata{}, options)
			} else {
				page, err = GetPDF(input, "", "pdf", exportDir, Metadata{})
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(page.MdFile)
			if err != nil {
				t.Fatal(err)
			}
			markdown := string(data)
			if !strings.Contains(markdown, "Native text survives JPEG 2000.") || !strings.Contains(markdown, "<!-- Page 1 -->") {
				t.Fatalf("native text missing: %s", markdown)
			}
			links := regexp.MustCompile(`\[Image \(JPEG 2000, original \.jpx\)\]\(([^)]+)\)`).FindAllStringSubmatch(markdown, -1)
			if len(links) != 2 || links[0][1] != links[1][1] || strings.Count(markdown, "![Image]") != 1 || strings.Contains(markdown, "![Image (JPEG 2000") {
				t.Fatalf("expected two deduplicated JPX links and one inline compatible image: %s", markdown)
			}
			link, err := url.Parse(links[0][1])
			if err != nil || link.IsAbs() || filepath.IsAbs(link.Path) || filepath.Ext(link.Path) != ".jpx" {
				t.Fatalf("expected relative JPX link: %q, %v", links[0][1], err)
			}
			asset := filepath.Join(exportDir, filepath.FromSlash(link.Path))
			original, err := os.ReadFile(asset)
			if err != nil || !bytes.Equal(original, jpx) {
				t.Fatalf("original JPX bytes were not retained: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(asset))
			if err != nil || len(entries) != 2 {
				t.Fatalf("expected one JPX and one PNG asset: %v, %v", entries, err)
			}
			if enabled {
				if calls != 1 || len(warnings) != 2 || strings.Count(markdown, "Compatible image description.") != 1 {
					t.Fatalf("unexpected AI behavior: calls=%d, warnings=%v, Markdown=%s", calls, warnings, markdown)
				}
				for _, warning := range warnings {
					if !strings.Contains(warning, "page 1") || !strings.Contains(warning, "JPEG 2000") || !strings.Contains(warning, filepath.Base(asset)) {
						t.Errorf("imprecise warning: %s", warning)
					}
				}
			} else if calls != 0 || strings.Contains(markdown, "AI-generated") {
				t.Fatal("AI was used while disabled")
			}
		})
	}
}

func TestGetPDFWithOptions_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	exportDir := t.TempDir()
	_, err := GetPDFWithOptions("../samples/test.pdf", "", "pdf", exportDir, Metadata{}, PDFOptions{Context: ctx})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	entries, err := os.ReadDir(exportDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected no files after cancellation, got %v, %v", entries, err)
	}
}

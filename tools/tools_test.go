package tools

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetPageLocalTitleFallback(t *testing.T) {
	for _, tt := range []struct {
		name, title, override, want string
	}{
		{"missing title", "", "", "local-page"},
		{"blank title", "  ", "", "local-page"},
		{"document title", "Document title", "", "Document title"},
		{"override", "Document title", "Override title", "Override title"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "local-page.html")
			if err := os.WriteFile(input, []byte("<html><head><title>"+tt.title+"</title></head><body><h1>Content</h1></body></html>"), 0600); err != nil {
				t.Fatal(err)
			}
			page, err := GetPage(input, "docs", t.TempDir(), Metadata{Title: tt.override}, "", false)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(page.MdFile)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "title: "+tt.want+"\n") || !strings.Contains(string(data), "# Content") {
				t.Fatalf("unexpected Markdown: %s", data)
			}
		})
	}
}

func TestGetPageReturnsErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := GetPage(filepath.Join(dir, "missing.html"), "docs", dir, Metadata{}, "", false); err == nil {
		t.Fatal("expected missing input error")
	}
	input := filepath.Join(dir, "input.html")
	if err := os.WriteFile(input, []byte("<html><body>Content</body></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := GetPage(input, "docs", filepath.Join(dir, "missing-output"), Metadata{}, "", false); err == nil {
		t.Fatal("expected write error")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()
	if _, err := GetPage(server.URL, "docs", dir, Metadata{}, "", false); err == nil {
		t.Fatal("expected HTTP status error")
	}
	server.Close()
	if _, err := GetPage(server.URL, "docs", dir, Metadata{}, "", false); err == nil {
		t.Fatal("expected HTTP connection error")
	}
}

func TestGetPageWithSource(t *testing.T) {
	input := filepath.Join(t.TempDir(), "local-page.html")
	if err := os.WriteFile(input, []byte("<html><body><h1>Remote content</h1></body></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	source := "s3://bucket/reports/local-page.html"
	page, err := GetPageWithSource(input, source, "docs", t.TempDir(), Metadata{}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(page.MdFile)
	if err != nil {
		t.Fatal(err)
	}
	if page.Url != source || !strings.Contains(string(data), "site_url: "+source+"\n") ||
		!strings.Contains(string(data), "title: local-page\n") || !strings.Contains(string(data), "# Remote content") ||
		strings.Contains(string(data), filepath.Dir(input)) {
		t.Fatalf("local input or remote provenance was not preserved: page=%+v, Markdown=%s", page, data)
	}
}

func TestGetPageAIUsesImageDescriptionAsAltText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":"A red square\nwith a blue border."}`))
	}))
	defer server.Close()
	t.Setenv("TOMD_AI_PROVIDER", "ollama")
	t.Setenv("TOMD_MODEL", "test-vision")
	t.Setenv("OLLAMA_HOST", server.URL)

	dir := t.TempDir()
	bitmap := image.NewRGBA(image.Rect(0, 0, 2, 2))
	bitmap.Set(0, 0, color.RGBA{R: 255, A: 255})
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, bitmap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "square.png"), imageData.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "page.html")
	if err := os.WriteFile(input, []byte(`<html><head><title>Image page</title></head><body><p>Example</p><img src="square.png" alt="Original alt"></body></html>`), 0600); err != nil {
		t.Fatal(err)
	}

	page, err := GetPage(input, "docs", dir, Metadata{}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(page.MdFile)
	if err != nil {
		t.Fatal(err)
	}
	markdown := string(data)
	if !strings.Contains(markdown, "![A red square with a blue border.](square.png)") {
		t.Fatalf("AI description was not used as image alt text: %s", markdown)
	}
	if strings.Contains(markdown, "Original alt") || strings.Contains(markdown, "Image description (AI-generated)") {
		t.Fatalf("unexpected old alt text or separate description section: %s", markdown)
	}
}

func TestBuildFilename_ValidTitle(t *testing.T) {
	title := "Valid Title"
	dir := "some/dir"
	id := "123"
	expected := "some/dir/123-valid-title.md"
	result := BuildFilename(title, dir, id)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestBuildFilename_TitleWithSpecialChars(t *testing.T) {
	title := "Title/With/Special'Chars"
	dir := "some/dir"
	id := "123"
	expected := "some/dir/123-title-with-special-chars.md"
	result := BuildFilename(title, dir, id)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestBuildFilename_EmptyTitle(t *testing.T) {
	title := ""
	dir := "some/dir"
	id := "123"
	expected := "some/dir/123-.md"
	result := BuildFilename(title, dir, id)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestReplaceAllMultipleChars_RemovesConsecutiveChars(t *testing.T) {
	input := "aa---bb__cc==dd"
	expected := "a-b_c=d"
	result := ReplaceAllMultipleChars(input)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

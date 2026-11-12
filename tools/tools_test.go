package tools

import (
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

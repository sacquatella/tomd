package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func executeDocs(t *testing.T, ctx context.Context, output string, args ...string) (string, string, error) {
	t.Helper()
	root := &cobra.Command{Use: "tomd", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("dir", output, "Export directory")
	root.PersistentFlags().Bool("ia", false, "Describe images")
	root.AddCommand(newDocsCommand())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"docs"}, args...))
	err := root.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

func writeDocsFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func docsHTML(body string) []byte {
	return []byte("<!doctype html><html><head><title>Shared title</title></head><body><p>" + body + "</p></body></html>")
}

func writeDocsZIP(t *testing.T, name string, parts map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
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
	writeDocsFile(t, name, buf.Bytes())
}

func docsPPTXParts() map[string]string {
	const ns = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	shape := func(text string) string {
		return `<p:sp><p:nvSpPr><p:cNvPr id="2" name="Body"/><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp>`
	}
	slide := func(attrs, text string) string {
		return `<p:sld ` + ns + attrs + `><p:cSld><p:spTree>` + shape(text) + `</p:spTree></p:cSld></p:sld>`
	}
	rel := func(id, kind, target string) string {
		return fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/%s" Target="%s"/>`, id, kind, target)
	}
	rels := func(content string) string {
		return `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + content + `</Relationships>`
	}
	return map[string]string{
		"docProps/core.xml":                `<coreProperties><title>Shared title</title></coreProperties>`,
		"ppt/presentation.xml":             `<p:presentation ` + ns + `><p:sldIdLst><p:sldId id="256" r:id="r1"/><p:sldId id="257" r:id="r2"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels":  rels(rel("r1", "slide", "slides/slide1.xml") + rel("r2", "slide", "slides/slide2.xml")),
		"ppt/slides/slide1.xml":            slide("", "Visible slide content"),
		"ppt/slides/slide2.xml":            slide(` show="0"`, "Hidden slide content"),
		"ppt/slides/_rels/slide1.xml.rels": rels(rel("notes", "notesSlide", "../notesSlides/notesSlide1.xml")),
		"ppt/notesSlides/notesSlide1.xml":  slide("", "Presenter note content"),
	}
}

func readDocsMarkdown(t *testing.T, destination string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(destination, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one Markdown file in %s, got %v", destination, files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("---\n")) {
		t.Fatalf("missing metadata in %s: %s", files[0], data)
	}
	return string(data)
}

func assertDocsSummary(t *testing.T, stdout string, converted, failures int) {
	t.Helper()
	want := fmt.Sprintf("Converted: %d; errors: %d\n", converted, failures)
	if !strings.HasSuffix(stdout, want) {
		t.Fatalf("expected summary %q, got %q", want, stdout)
	}
	if got := strings.Count(stdout, " -> "); got != converted {
		t.Fatalf("expected %d conversion lines, got %d: %s", converted, got, stdout)
	}
	rows := 0
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "|") {
			rows++
		}
	}
	if rows != converted+1 {
		t.Fatalf("expected one table header and %d result rows, got %d: %s", converted, rows, stdout)
	}
	for _, header := range []string{"Page ID", "Url", "Markdown files"} {
		if !strings.Contains(stdout, header) {
			t.Errorf("missing table header %q: %s", header, stdout)
		}
	}
}

func assertDocsAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no output at %s, got %v", path, err)
	}
}

func TestDocsMixedFormatsAndIsolatedOutputs(t *testing.T) {
	source, output := t.TempDir(), t.TempDir()
	markers := map[string]string{
		"Shared title.HTML": "HTML unique content",
		"Shared title.HTM":  "HTM unique content",
		"Other name.html":   "Other HTML unique content",
		"Shared title.PDF":  "<!-- Page 1 -->",
		"Shared title.DOCX": "DOCX unique content",
		"Shared title.PPTX": "Visible slide content",
	}
	for _, name := range []string{"Shared title.HTML", "Shared title.HTM", "Other name.html"} {
		writeDocsFile(t, filepath.Join(source, name), docsHTML(markers[name]))
	}
	pdf, err := os.ReadFile(filepath.Join("..", "samples", "test.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	writeDocsFile(t, filepath.Join(source, "Shared title.PDF"), pdf)
	writeDocsZIP(t, filepath.Join(source, "Shared title.DOCX"), map[string]string{
		"word/document.xml": `<document><body><p><r><t>DOCX unique content</t></r></p></body></document>`,
		"docProps/core.xml": `<coreProperties><title>Shared title</title></coreProperties>`,
	})
	writeDocsZIP(t, filepath.Join(source, "Shared title.PPTX"), docsPPTXParts())
	stdout, stderr, err := executeDocs(t, context.Background(), output, source, "--cid", "client_1-test")
	if err != nil {
		t.Fatalf("conversion failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	assertDocsSummary(t, stdout, len(markers), 0)
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(markers) {
		t.Fatalf("expected isolated source directories, got %v", entries)
	}
	for name, marker := range markers {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(output, name+".tomd")
			markdown := readDocsMarkdown(t, destination)
			for _, want := range []string{"title: Shared title\n", "doc_id: CLIENT_1-TEST_SHAREDTITLE\n", marker} {
				if !strings.Contains(markdown, want) {
					t.Errorf("missing %q in %s", want, markdown)
				}
			}
			mdPath := filepath.Join(destination, "client_1-test-shared-title.md")
			if !strings.Contains(stdout, name+" -> "+mdPath+"\n") {
				t.Errorf("missing isolated output mapping in %s", stdout)
			}
			found := false
			for _, line := range strings.Split(stdout, "\n") {
				if strings.HasPrefix(line, "|") && strings.Contains(line, mdPath) {
					found = strings.Contains(line, "CLIENT_1-TEST_SHAREDTITLE") && strings.Contains(line, filepath.Join(source, name))
				}
			}
			if !found {
				t.Errorf("missing table row with page ID, source URL and Markdown file for %s: %s", name, stdout)
			}
		})
	}
}

func TestDocsRecursive(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		t.Run(fmt.Sprintf("recursive=%t", recursive), func(t *testing.T) {
			source, output := t.TempDir(), t.TempDir()
			writeDocsFile(t, filepath.Join(source, "root.html"), docsHTML("Root content"))
			nested := filepath.Join("nested", "deeper", "child.htm")
			writeDocsFile(t, filepath.Join(source, nested), docsHTML("Nested content"))
			args := []string{source}
			if recursive {
				args = append(args, "--recursive")
			}
			stdout, stderr, err := executeDocs(t, context.Background(), output, args...)
			if err != nil {
				t.Fatalf("%v: %s", err, stderr)
			}
			count := 1
			if recursive {
				count++
				if md := readDocsMarkdown(t, filepath.Join(output, nested+".tomd")); !strings.Contains(md, "Nested content") {
					t.Fatalf("nested input content missing: %s", md)
				}
			} else {
				assertDocsAbsent(t, filepath.Join(output, "nested"))
			}
			readDocsMarkdown(t, filepath.Join(output, "root.html.tomd"))
			assertDocsSummary(t, stdout, count, 0)
		})
	}
}

func TestDocsSkipsUnsupportedAndSymlinks(t *testing.T) {
	source, output, external := t.TempDir(), t.TempDir(), t.TempDir()
	writeDocsFile(t, filepath.Join(source, "valid.html"), docsHTML("Valid content"))
	writeDocsFile(t, filepath.Join(source, "unsupported.txt"), docsHTML("Unsupported content"))
	writeDocsFile(t, filepath.Join(source, "no-extension"), docsHTML("Unsupported content"))
	writeDocsFile(t, filepath.Join(external, "external.html"), docsHTML("External content"))
	for name, target := range map[string]string{
		"file-link.html": filepath.Join(source, "valid.html"),
		"dir-link":       external,
		"broken.html":    filepath.Join(external, "missing.html"),
		"cycle":          source,
	} {
		if err := os.Symlink(target, filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, err := executeDocs(t, context.Background(), output, source, "-r")
	if err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	assertDocsSummary(t, stdout, 1, 0)
	readDocsMarkdown(t, filepath.Join(output, "valid.html.tomd"))
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 1 || entries[0].Name() != "valid.html.tomd" {
		t.Fatalf("unexpected outputs: %v, %v", entries, err)
	}
}

func TestDocsSkipsNestedExportDirectory(t *testing.T) {
	source := t.TempDir()
	output := filepath.Join(source, "nested", "exports")
	writeDocsFile(t, filepath.Join(source, "valid.html"), docsHTML("Valid content"))
	ignored := filepath.Join(output, "already-exported.html")
	original := docsHTML("Must not be traversed")
	writeDocsFile(t, ignored, original)
	stdout, stderr, err := executeDocs(t, context.Background(), output, source, "--recursive")
	if err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	assertDocsSummary(t, stdout, 1, 0)
	readDocsMarkdown(t, filepath.Join(output, "valid.html.tomd"))
	assertDocsAbsent(t, filepath.Join(output, "nested"))
	data, err := os.ReadFile(ignored)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("nested export input changed: %q, %v", data, err)
	}
}

func TestDocsRefusesExistingOutputAndContinues(t *testing.T) {
	source, output := t.TempDir(), t.TempDir()
	writeDocsFile(t, filepath.Join(source, "a-existing.html"), docsHTML("Replacement content"))
	writeDocsFile(t, filepath.Join(source, "z-valid.html"), docsHTML("Later valid content"))
	destination := filepath.Join(output, "a-existing.html.tomd")
	existing := filepath.Join(destination, "docs-shared-title.md")
	original := []byte("Original Markdown must remain unchanged\n")
	writeDocsFile(t, existing, original)
	stdout, _, err := executeDocs(t, context.Background(), output, source)
	if err == nil || !strings.Contains(err.Error(), "a-existing.html") || !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected existing destination error, got %v", err)
	}
	assertDocsSummary(t, stdout, 1, 1)
	data, err := os.ReadFile(existing)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("existing Markdown changed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 {
		t.Fatalf("existing destination changed: %v, %v", entries, err)
	}
	if md := readDocsMarkdown(t, filepath.Join(output, "z-valid.html.tomd")); !strings.Contains(md, "Later valid content") {
		t.Fatalf("later input was not converted: %s", md)
	}
}

func TestDocsExportIntoSourceAndPreserveSecondRun(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		t.Run(fmt.Sprintf("recursive=%t", recursive), func(t *testing.T) {
			source := t.TempDir()
			input := filepath.Join(source, "rapport.html")
			original := docsHTML("In-place conversion content")
			writeDocsFile(t, input, original)
			args := []string{source}
			if recursive {
				args = append(args, "--recursive")
			}
			stdout, stderr, err := executeDocs(t, context.Background(), source, args...)
			if err != nil {
				t.Fatalf("in-place conversion failed: %v, stderr: %s", err, stderr)
			}
			assertDocsSummary(t, stdout, 1, 0)
			destination := filepath.Join(source, "rapport.html.tomd")
			markdown := readDocsMarkdown(t, destination)
			if !strings.Contains(markdown, "In-place conversion content") {
				t.Fatalf("source content missing: %s", markdown)
			}
			mdPath := filepath.Join(destination, "docs-shared-title.md")
			if !strings.Contains(stdout, "rapport.html -> "+mdPath+"\n") {
				t.Fatalf("missing original source name in output: %s", stdout)
			}
			preserved := []byte(markdown + "\nManual edit must survive a second run\n")
			writeDocsFile(t, mdPath, preserved)
			stdout, _, err = executeDocs(t, context.Background(), source, args...)
			if !errors.Is(err, os.ErrExist) || !strings.Contains(err.Error(), "rapport.html") {
				t.Fatalf("expected existing destination error on second run, got %v", err)
			}
			assertDocsSummary(t, stdout, 0, 1)
			for path, want := range map[string][]byte{input: original, mdPath: preserved} {
				data, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, want) {
					t.Errorf("file changed at %s: %q, %v", path, data, err)
				}
			}
			entries, err := os.ReadDir(source)
			if err != nil || len(entries) != 2 {
				t.Fatalf("unexpected in-place outputs: %v, %v", entries, err)
			}
			entries, err = os.ReadDir(destination)
			if err != nil || len(entries) != 1 {
				t.Fatalf("second run changed destination entries: %v, %v", entries, err)
			}
		})
	}
}

func TestDocsRecursiveSourceDirectoryOutputCollision(t *testing.T) {
	source := t.TempDir()
	input := filepath.Join(source, "a.html")
	original := docsHTML("Must not replace the colliding directory")
	writeDocsFile(t, input, original)
	collision := filepath.Join(source, "a.html.tomd")
	child := filepath.Join(collision, "child.html")
	childOriginal := docsHTML("Nested source still converts")
	writeDocsFile(t, child, childOriginal)
	existing := filepath.Join(collision, "docs-shared-title.md")
	preserved := []byte("Existing source directory content must survive\n")
	writeDocsFile(t, existing, preserved)
	stdout, _, err := executeDocs(t, context.Background(), source, source, "--recursive")
	if !errors.Is(err, os.ErrExist) || !strings.Contains(err.Error(), "a.html") {
		t.Fatalf("expected directory collision error, got %v", err)
	}
	assertDocsSummary(t, stdout, 1, 1)
	for path, want := range map[string][]byte{input: original, child: childOriginal, existing: preserved} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, want) {
			t.Errorf("collision changed %s: %q, %v", path, data, err)
		}
	}
	destination := filepath.Join(collision, "child.html.tomd")
	if md := readDocsMarkdown(t, destination); !strings.Contains(md, "Nested source still converts") {
		t.Fatalf("nested source was not converted after collision: %s", md)
	}
	relative := filepath.Join("a.html.tomd", "child.html")
	if !strings.Contains(stdout, relative+" -> "+filepath.Join(destination, "docs-shared-title.md")+"\n") {
		t.Fatalf("missing original nested source name in output: %s", stdout)
	}
	entries, err := os.ReadDir(collision)
	if err != nil || len(entries) != 3 {
		t.Fatalf("unexpected colliding directory entries: %v, %v", entries, err)
	}
}

func TestDocsInvalidDocumentsContinue(t *testing.T) {
	source, output := t.TempDir(), t.TempDir()
	for _, name := range []string{"a-invalid.pdf", "b-invalid.docx"} {
		writeDocsFile(t, filepath.Join(source, name), []byte("not a valid document"))
	}
	writeDocsFile(t, filepath.Join(source, "z-valid.html"), docsHTML("Survived both failures"))
	stdout, _, err := executeDocs(t, context.Background(), output, source)
	if err == nil {
		t.Fatal("expected joined conversion errors")
	}
	for _, name := range []string{"a-invalid.pdf", "b-invalid.docx"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not identify %s: %v", name, err)
		}
		files, globErr := filepath.Glob(filepath.Join(output, name+".tomd", "*.md"))
		if globErr != nil || len(files) != 0 {
			t.Errorf("failed input produced Markdown: %v, %v", files, globErr)
		}
	}
	assertDocsSummary(t, stdout, 1, 2)
	if md := readDocsMarkdown(t, filepath.Join(output, "z-valid.html.tomd")); !strings.Contains(md, "Survived both failures") {
		t.Fatalf("valid input was not converted after errors: %s", md)
	}
}

func TestDocsValidation(t *testing.T) {
	source := t.TempDir()
	file := filepath.Join(source, "file.html")
	writeDocsFile(t, file, docsHTML("Not a directory"))
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, "accepts 1 arg(s), received 0"},
		{"extra argument", []string{source, source}, "accepts 1 arg(s), received 2"},
		{"missing path", []string{filepath.Join(source, "missing")}, "no such file or directory"},
		{"not a directory", []string{file}, "is not a directory"},
	}
	for _, cid := range []string{"", "../escape", "a/b", `a\b`, "with space", ".", "a:b"} {
		tests = append(tests, struct {
			name string
			args []string
			want string
		}{"unsafe cid " + cid, []string{source, "--cid=" + cid}, "--cid must contain only"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "not-created")
			stdout, _, err := executeDocs(t, context.Background(), output, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
			if stdout != "" {
				t.Errorf("unexpected conversion output: %s", stdout)
			}
			assertDocsAbsent(t, output)
		})
	}
}

func TestDocsEmptyDirectory(t *testing.T) {
	output := t.TempDir()
	stdout, stderr, err := executeDocs(t, context.Background(), output, t.TempDir())
	if err != nil || stderr != "" {
		t.Fatalf("empty directory failed: %v, stderr: %s", err, stderr)
	}
	assertDocsSummary(t, stdout, 0, 0)
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty directory created outputs: %v, %v", entries, err)
	}
}

func TestDocsCancellation(t *testing.T) {
	source, output := t.TempDir(), t.TempDir()
	writeDocsFile(t, filepath.Join(source, "valid.html"), docsHTML("Must not convert"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stdout, _, err := executeDocs(t, ctx, output, source, "--recursive")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if stdout != "" {
		t.Errorf("canceled traversal produced output: %s", stdout)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled command created outputs: %v, %v", entries, err)
	}
}

func TestDocsPPTXFlags(t *testing.T) {
	for _, tt := range []struct {
		name          string
		flags         []string
		notes, hidden bool
	}{
		{"defaults", nil, true, true},
		{"no notes", []string{"--no-notes"}, false, true},
		{"exclude hidden", []string{"--exclude-hidden"}, true, false},
		{"both", []string{"--no-notes", "--exclude-hidden"}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, output := t.TempDir(), t.TempDir()
			writeDocsZIP(t, filepath.Join(source, "slides.pptx"), docsPPTXParts())
			stdout, stderr, err := executeDocs(t, context.Background(), output, append([]string{source}, tt.flags...)...)
			if err != nil {
				t.Fatalf("%v: %s", err, stderr)
			}
			assertDocsSummary(t, stdout, 1, 0)
			markdown := readDocsMarkdown(t, filepath.Join(output, "slides.pptx.tomd"))
			if !strings.Contains(markdown, "Visible slide content") {
				t.Fatalf("visible slide missing: %s", markdown)
			}
			if got := strings.Contains(markdown, "Presenter note content"); got != tt.notes {
				t.Errorf("notes present = %t, want %t: %s", got, tt.notes, markdown)
			}
			if got := strings.Contains(markdown, "Hidden slide content"); got != tt.hidden {
				t.Errorf("hidden slide present = %t, want %t: %s", got, tt.hidden, markdown)
			}
		})
	}
}

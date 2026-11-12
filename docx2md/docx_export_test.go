package docx2md

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sacquatella/tomd/tools"
)

func writeDocxArchive(t *testing.T, filename, target, mode string, image []byte, corruptEntry string) {
	t.Helper()
	entries := []struct {
		name string
		data []byte
	}{
		{"word/document.xml", []byte(`<document><body><p><r><drawing><pic><nvPicPr><cNvPr descr="sample"/></nvPicPr><blipFill><blip embed="rId1"/></blipFill></pic></drawing></r></p></body></document>`)},
		{"word/_rels/document.xml.rels", []byte(fmt.Sprintf(`<Relationships><Relationship Id="rId1" Target="%s" TargetMode="%s"/></Relationships>`, target, mode))},
		{"word/numbering.xml", []byte(`<numbering/>`)},
		{"docProps/core.xml", []byte(`<coreProperties/>`)},
		{"word/media/image1.png", image},
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, entry := range entries {
		f, err := w.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if corruptEntry != "" {
		r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range r.File {
			if f.Name == corruptEntry {
				offset, err := f.DataOffset()
				if err != nil {
					t.Fatal(err)
				}
				data[offset] ^= 1
			}
		}
	}
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGetDocxInvalidInput(t *testing.T) {
	input := filepath.Join(t.TempDir(), "invalid.docx")
	if err := os.WriteFile(input, []byte("not a ZIP archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := GetDocx(input, "", "", t.TempDir(), tools.Metadata{}); err == nil {
		t.Fatal("expected an error for invalid DOCX")
	}
}

func TestGetDocxIsolatedAssets(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	cwdImage := filepath.Join(cwd, "media", "image1.png")
	imageBefore, imageBeforeErr := os.ReadFile(cwdImage)
	if imageBeforeErr != nil && !os.IsNotExist(imageBeforeErr) {
		t.Fatal(imageBeforeErr)
	}
	inputDir, exportDir := t.TempDir(), t.TempDir()
	images := [][]byte{bytes.Repeat([]byte("first image"), 16384), []byte("second image")}
	var assets []string
	for i, stem := range []string{"first document", "second"} {
		input := filepath.Join(inputDir, stem+".docx")
		writeDocxArchive(t, input, "media/image1.png", "", images[i], "")
		page, err := GetDocx(input, "", "test", exportDir, tools.Metadata{})
		if err != nil {
			t.Fatal(err)
		}
		markdown, err := os.ReadFile(page.MdFile)
		if err != nil {
			t.Fatal(err)
		}
		link := (&url.URL{Path: stem + "-assets/media/image1.png"}).EscapedPath()
		if !strings.Contains(string(markdown), "![sample]("+link+")") {
			t.Fatalf("missing valid relative image link %q in %s", link, markdown)
		}
		relative, err := url.PathUnescape(link)
		if err != nil {
			t.Fatal(err)
		}
		assets = append(assets, filepath.Join(filepath.Dir(page.MdFile), filepath.FromSlash(relative)))
	}
	for i, asset := range assets {
		data, err := os.ReadFile(asset)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, images[i]) {
			t.Fatalf("asset %s was truncated or overwritten", asset)
		}
	}
	after, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("export created entries in the working directory")
	}
	imageAfter, imageAfterErr := os.ReadFile(cwdImage)
	if imageAfterErr != nil && !os.IsNotExist(imageAfterErr) {
		t.Fatal(imageAfterErr)
	}
	if (imageBeforeErr == nil) != (imageAfterErr == nil) || !bytes.Equal(imageBefore, imageAfter) {
		t.Fatal("export wrote to media/image1.png in the working directory")
	}
	if current, err := os.Getwd(); err != nil || current != cwd {
		t.Fatalf("working directory changed: %q, %v", current, err)
	}
}

func TestDocxRejectsUnsafeImageTargets(t *testing.T) {
	for _, target := range []string{"../image.png", "media/../../image.png", "/tmp/image.png", `..\image.png`, `C:\image.png`, "C:/image.png", `\\server\image.png`, "https://example.com/image.png"} {
		t.Run(target, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "unsafe.docx")
			writeDocxArchive(t, input, target, "", []byte("image"), "")
			if _, err := GetDocx(input, "", "", t.TempDir(), tools.Metadata{}); err == nil {
				t.Fatal("expected unsafe image target error")
			}
		})
	}
	t.Run("external", func(t *testing.T) {
		input := filepath.Join(t.TempDir(), "external.docx")
		writeDocxArchive(t, input, "media/image1.png", "External", []byte("image"), "")
		if _, _, err := Docx2md(input, true); err == nil {
			t.Fatal("expected external image target error")
		}
	})
}

func TestDocxCorruptEntries(t *testing.T) {
	for _, entry := range []string{"word/document.xml", "word/_rels/document.xml.rels", "word/numbering.xml", "docProps/core.xml", "word/media/image1.png"} {
		t.Run(entry, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "corrupt.docx")
			writeDocxArchive(t, input, "media/image1.png", "", []byte("image"), entry)
			if _, err := GetDocx(input, "", "", t.TempDir(), tools.Metadata{}); !errors.Is(err, zip.ErrChecksum) {
				t.Fatalf("expected ZIP checksum error, got %v", err)
			}
		})
	}
}

func TestReadFileOpenError(t *testing.T) {
	input := filepath.Join(t.TempDir(), "unsupported.docx")
	writeDocxArchive(t, input, "media/image1.png", "", []byte("image"), "")
	r, err := zip.OpenReader(input)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	f := r.File[0]
	f.Method = 0xffff
	if _, err := readFile(f); !errors.Is(err, zip.ErrAlgorithm) {
		t.Fatalf("expected unsupported compression error, got %v", err)
	}
}

func TestDocxEmbeddedImage(t *testing.T) {
	input := filepath.Join(t.TempDir(), "embedded.docx")
	image := bytes.Repeat([]byte("image"), 16384)
	writeDocxArchive(t, input, "media/image1.png", "", image, "")
	markdown, _, err := Docx2md(input, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown, "![sample](data:image/png;base64,"+base64.StdEncoding.EncodeToString(image)+")") {
		t.Fatal("missing complete embedded image")
	}
}

func TestGetDocxWriteError(t *testing.T) {
	input := filepath.Join(t.TempDir(), "valid.docx")
	writeDocxArchive(t, input, "media/image1.png", "", []byte("image"), "")
	exportDir := t.TempDir()
	// Asset extraction succeeds, but the Markdown destination is a directory.
	if err := os.Mkdir(filepath.Join(exportDir, "test-valid.md"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := GetDocx(input, "", "test", exportDir, tools.Metadata{}); err == nil {
		t.Fatal("expected Markdown write error")
	}
}

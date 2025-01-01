package tools

import (
	"strings"
	"testing"
)

// TestExtractTextFromPDF_ValidPdf test reads a PDF file and converts it to Markdown
func TestExtractTextFromPDF_ValidPdf(t *testing.T) {

	pdfFile := "../samples/test.pdf"
	expectedMarkdown := "Exemple de texte en HTML\nTitre Two"

	result, err := ExtractTextFromPDF(pdfFile)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !strings.Contains(result, expectedMarkdown) {
		t.Errorf("expected contains %s, but not in %s", expectedMarkdown, result)
	}

}

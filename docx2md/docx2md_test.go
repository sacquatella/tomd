package docx2md

import (
	"testing"
)

// TestEscape test escape function
func TestEscape(t *testing.T) {
	tests := []struct {
		input  string
		escape string
		want   string
	}{
		{input: `\`, escape: `\`, want: `\\`},
		{input: `\`, escape: ``, want: `\`},
		{input: `\`, escape: `-`, want: `\`},
		{input: `\\`, escape: `\`, want: `\\\\`},
		{input: `\200`, escape: `\`, want: `\\200`},
	}
	for _, test := range tests {
		got := escape(test.input, test.escape)
		if got != test.want {
			t.Fatalf("want %v, but %v:", test.want, got)
		}
	}
}

// TestDocxToMd_ValidDocx test valid docx
func TestDocxToMd_ValidDocx(t *testing.T) {

	docxfile := "../samples/test.docx"
	expectedMarkdown := "../samples/test-docx.md"
	embed := false

	result, err := Docx2md(docxfile, embed)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != expectedMarkdown {
		t.Errorf("expected %s, got %s", expectedMarkdown, result)
	}
}

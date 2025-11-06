package tools

import (
	"fmt"
	"os"
	"strings"

	"github.com/rsc/pdf"
)

func GetPDF(pdfPath string, url string, customerId string, exportDir string, complements Metadata) (Page, error) {

	markdown, err := ExtractTextFromPDF(pdfPath)
	CheckError(err)

	// Add metadata header to markdown with title , doc_id,description , tags, site_url, authors, creation_date, last_update
	metadata, metaDatas := BuildFileMetadata(pdfPath, url, customerId, Metadata{}, complements)

	// Add metadata header to markdown
	markdown = metadata + markdown

	//exportedFile := exportDir + "/" + customerId + "-" + metaDatas.Title + ".md"
	exportedFile := BuildFilename(metaDatas.Title, exportDir, customerId)

	// Écrire le Markdown dans un fichier
	err = WriteMarkdownToFile(markdown, exportedFile)
	CheckError(err)

	return Page{PageId: metaDatas.Doc_id, Url: metaDatas.Site_url, MdFile: exportedFile}, nil
}

// ExtractTextFromPDF extract text from a PDF.
func ExtractTextFromPDF(pdfPath string) (string, error) {
	// Ouvrir le fichier PDF
	file, err := os.Open(pdfPath)
	if err != nil {
		return "", fmt.Errorf("can't open PDF file   : %w", err)
	}
	defer file.Close()

	// Read PDF document
	filestat, _ := file.Stat()
	reader, err := pdf.NewReader(file, filestat.Size())
	if err != nil {
		return "", fmt.Errorf("can't read and parse PDF file : %w", err)
	}

	var textBuilder strings.Builder

	// parse pages ti get text
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.Content().Text == nil {
			continue
		}

		text := page.V.RawString()
		fmt.Printf("Page %d : %s\n", i, text)

		textBuilder.WriteString(fmt.Sprintf("# Page %d\n\n", i)) //  Add Title for each page

		content := page.Content()
		line := 0.0  // Manage line break base on Y position
		nextX := 0.0 //  X position of next text
		for _, text := range content.Text {
			if text.Y != line {
				textBuilder.WriteString("\n")
			}
			line = text.Y
			if nextX < text.X {
				textBuilder.WriteString(" ")
			}
			textBuilder.WriteString(text.S)
			nextX = (text.X + text.W) + (text.W)*0.1
		}
		textBuilder.WriteString("\n\n") //  Add a line break between pages
	}

	return textBuilder.String(), nil
}

// WriteMarkdownToFile  writes markdown content to a file.
func WriteMarkdownToFile(markdown, outputPath string) error {
	return os.WriteFile(outputPath, []byte(markdown), 0644)
}

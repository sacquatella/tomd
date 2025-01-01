package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rsc/pdf"
)

func GetPDF(pdfPath string, url string, customerId string, exportDir string, complements Metadata) (Page, error) {

	markdown, err := ExtractTextFromPDF(pdfPath)
	CheckError(err)

	// Add metadata header to markdown with title , doc_id,description , tags, site_url, authors, creation_date, last_update
	metadata, metaDatas := BuildPDFMetadata(pdfPath, url, customerId, complements)

	// Add metadata header to markdown
	markdown = metadata + markdown

	exportedFile := exportDir + "/" + customerId + "-" + metaDatas.Title + ".md"
	// Écrire le Markdown dans un fichier
	err = WriteMarkdownToFile(markdown, exportedFile)
	CheckError(err)

	return Page{PageId: metaDatas.Doc_id, Url: metaDatas.Site_url, MdFile: exportedFile}, nil
}

// BuildPDFMetadata build metadata for a PDF file.
func BuildPDFMetadata(pdfPath string, url string, prefix string, complement Metadata) (string, Metadata) {
	var metaData Metadata

	metaData.Title = strings.ReplaceAll(filepath.Base(pdfPath), filepath.Ext(pdfPath), "")
	// Build doc_id as TITLE in UPPERCASE WITHOUT SPACE
	// set doc_id as prefix + "_" + content.ID
	doc_id := strings.ReplaceAll(strings.ToUpper(metaData.Title), " ", "")
	metaData.Doc_id = strings.ToUpper(prefix + "_" + doc_id)

	// Add metadata header to markdown with title , doc_id,description , tags, site_url, authors, creation_date, last_update
	metaData.Tags = append(metaData.Tags, "pdf")
	// set site_url
	metaData.Site_url = url
	// add new authors if complement.authors is not empty
	if len(complement.Authors) > 0 {
		for _, author := range complement.Authors {
			metaData.Authors = append(metaData.Authors, author)
		}
	}
	// build metadata tag list
	var taglist string
	for _, tag := range metaData.Tags {
		taglist += "\n" + "- " + tag
	}
	// build authors list
	var authorslist string
	for _, author := range metaData.Authors {
		authorslist += "\n" + "- " + author
	}

	pageMetadata := fmt.Sprintf("---\ntitle: %s\ndoc_id: %s\ndescription: %s\ntags: %s\nsite_url: %s\nauthors: %s\ncreation_date: %s\nlast_update_date: %s\nvisibility: %s\n---\n",
		metaData.Title,
		metaData.Doc_id,
		metaData.Description,
		taglist,
		metaData.Site_url,
		authorslist,
		metaData.Creation_date,    // date should be in ISO 8601 format without seconds
		metaData.Last_update_date, // date should be in ISO 8601 format without seconds
		metaData.Visibility)
	return pageMetadata, metaData
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

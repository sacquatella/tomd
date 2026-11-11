package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
	log "github.com/sirupsen/logrus"
)

func GetPDF(pdfPath string, url string, customerId string, exportDir string, complements Metadata) (Page, error) {
	metadata, metaDatas := BuildFileMetadata(pdfPath, url, customerId, Metadata{}, complements)
	exportedFile := BuildFilename(metaDatas.Title, exportDir, customerId)
	assetsDir := strings.TrimSuffix(exportedFile, filepath.Ext(exportedFile)) + "-assets"
	images, err := readPDFImages(pdfPath, filepath.Base(assetsDir))
	if err != nil {
		return Page{}, err
	}
	markdown, err := extractPDF(pdfPath, images)
	if err != nil {
		return Page{}, err
	}
	if err := writePDFImages(images, assetsDir); err != nil {
		return Page{}, err
	}
	err = WriteMarkdownToFile(metadata+markdown, exportedFile)
	if err != nil {
		return Page{}, fmt.Errorf("can't write Markdown file %q: %w", exportedFile, err)
	}

	return Page{PageId: metaDatas.Doc_id, Url: metaDatas.Site_url, MdFile: exportedFile}, nil
}

// ExtractTextFromPDF extract text from a PDF.
func ExtractTextFromPDF(pdfPath string) (string, error) {
	return extractPDF(pdfPath, nil)
}

func extractPDF(pdfPath string, images map[int][]pdfImage) (markdown string, err error) {
	pageNumber := 0
	defer func() {
		if recovered := recover(); recovered != nil {
			markdown = ""
			err = fmt.Errorf("can't extract text from PDF file %q (page %d): %v", pdfPath, pageNumber, recovered)
		}
	}()

	// Ouvrir le fichier PDF
	file, err := os.Open(pdfPath)
	if err != nil {
		return "", fmt.Errorf("can't open PDF file   : %w", err)
	}
	defer file.Close()

	// Read PDF document
	filestat, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("can't stat PDF file: %w", err)
	}
	reader, err := pdf.NewReader(file, filestat.Size())
	if err != nil {
		return "", fmt.Errorf("can't read and parse PDF file : %w", err)
	}

	var textBuilder strings.Builder

	// parse pages ti get text
	for i := 1; i <= reader.NumPage(); i++ {
		pageNumber = i
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		var content pdf.Content
		if !page.V.Key("Contents").IsNull() {
			content = page.Content()
		}
		pageImages := images[i]
		if len(content.Text) == 0 && len(pageImages) == 0 {
			continue
		}
		if len(pageImages) > 0 {
			pageImages = positionPDFImages(page, pageImages)
		}
		log.Debugf("Page %d: %d text fragments", i, len(content.Text))

		textBuilder.WriteString(fmt.Sprintf("<!-- Page %d -->\n\n", i))
		textBuilder.WriteString(renderPDFContent(content.Text, pageImages))
	}

	return textBuilder.String(), nil
}

// WriteMarkdownToFile  writes markdown content to a file.
func WriteMarkdownToFile(markdown, outputPath string) error {
	return os.WriteFile(outputPath, []byte(markdown), 0644)
}

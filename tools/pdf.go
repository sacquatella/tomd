package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
	log "github.com/sirupsen/logrus"
)

type PDFOptions struct {
	Describer ImageDescriber
	Language  string
	Context   context.Context
	Warn      func(string)
}

func GetPDF(pdfPath string, url string, customerId string, exportDir string, complements Metadata) (Page, error) {
	return GetPDFWithOptions(pdfPath, url, customerId, exportDir, complements, PDFOptions{})
}

func GetPDFWithOptions(pdfPath string, url string, customerId string, exportDir string, complements Metadata, options PDFOptions) (Page, error) {
	metadata, metaDatas := BuildFileMetadata(pdfPath, url, customerId, Metadata{}, complements)
	exportedFile := BuildFilename(metaDatas.Title, exportDir, customerId)
	assetsDir := strings.TrimSuffix(exportedFile, filepath.Ext(exportedFile)) + "-assets"
	images, err := readPDFImages(pdfPath, filepath.Base(assetsDir))
	if err != nil {
		return Page{}, err
	}
	markdown, err := extractPDF(pdfPath, images, options)
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
	return extractPDF(pdfPath, nil, PDFOptions{})
}

func extractPDF(pdfPath string, images map[int][]pdfImage, options PDFOptions) (markdown string, err error) {
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Warn == nil {
		options.Warn = func(message string) { fmt.Fprintln(os.Stderr, "Warning: "+message) }
	}
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
		if err := options.Context.Err(); err != nil {
			return "", err
		}
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
			pageImages, err = describePDFImages(pageImages, content.Text, i, options)
			if err != nil {
				return "", err
			}
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

package tools

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type pdfImage struct {
	name, filename, link string
	description          string
	object               int
	data                 []byte
	x, right, y          float64
	positioned           bool
}

func describePDFImages(images []pdfImage, texts []pdf.Text, pageNumber int, options PDFOptions) ([]pdfImage, error) {
	if options.Describer == nil {
		return images, nil
	}
	contextText := []rune(renderPDFText(texts))
	if len(contextText) > 4000 {
		contextText = contextText[:4000]
	}
	for index := range images {
		if err := options.Context.Err(); err != nil {
			return nil, err
		}
		resource := &images[index]
		description, err := options.Describer.Describe(options.Context, resource.data, http.DetectContentType(resource.data), options.Language,
			fmt.Sprintf("Page %d\n%s", pageNumber, string(contextText)))
		if contextErr := options.Context.Err(); contextErr != nil {
			return nil, contextErr
		}
		if err != nil || strings.TrimSpace(description) == "" {
			options.Warn(fmt.Sprintf("page %d: AI image description unavailable for %s", pageNumber, resource.filename))
			resource.description = "AI-generated description unavailable; native content retained."
		} else {
			resource.description = "AI-generated visual description:\n\n" + strings.TrimSpace(description)
		}
	}
	return images, nil
}

func (resource pdfImage) markdown() string {
	text := "![Image](" + resource.link + ")"
	if resource.description != "" {
		text += "\n\n" + resource.description
	}
	return text
}

func readPDFImages(pdfPath, assetsPrefix string) (images map[int][]pdfImage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			images = nil
			err = fmt.Errorf("can't extract PDF images: %v", recovered)
		}
	}()
	file, err := os.Open(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("can't open PDF file: %w", err)
	}
	defer file.Close()
	images = make(map[int][]pdfImage)
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	err = api.ExtractImages(file, nil, func(resource model.Image, _ bool, _ int) error {
		if resource.Reader == nil || resource.IsImgMask || resource.Thumb {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(resource, (64<<20)+1))
		if err != nil {
			return err
		}
		if len(data) > 64<<20 {
			return fmt.Errorf("PDF image on page %d exceeds 64 MiB", resource.PageNr)
		}
		ext := strings.ToLower(resource.FileType)
		if ext != "png" && ext != "jpg" && ext != "jpeg" && ext != "webp" {
			decoded, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("decode PDF image on page %d: %w", resource.PageNr, err)
			}
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, decoded); err != nil {
				return err
			}
			data, ext = encoded.Bytes(), "png"
		}
		hash := sha256.Sum256(data)
		filename := fmt.Sprintf("image-%x.%s", hash[:16], ext)
		link := (&url.URL{Path: filepath.ToSlash(filepath.Join(assetsPrefix, filename))}).String()
		images[resource.PageNr] = append(images[resource.PageNr], pdfImage{
			name: resource.Name, object: resource.ObjNr, filename: filename, link: link, data: data,
		})
		return nil
	}, conf)
	if err != nil {
		return nil, fmt.Errorf("can't extract PDF images: %w", err)
	}
	for _, resources := range images {
		sort.Slice(resources, func(first, second int) bool { return resources[first].object < resources[second].object })
	}
	return images, nil
}

func writePDFImages(images map[int][]pdfImage, assetsDir string) error {
	written := make(map[string]bool)
	for _, resources := range images {
		for _, resource := range resources {
			if written[resource.filename] {
				continue
			}
			if len(written) == 0 {
				if err := os.MkdirAll(assetsDir, 0755); err != nil {
					return fmt.Errorf("create PDF assets directory: %w", err)
				}
			}
			if err := os.WriteFile(filepath.Join(assetsDir, resource.filename), resource.data, 0644); err != nil {
				return fmt.Errorf("write PDF image: %w", err)
			}
			written[resource.filename] = true
		}
	}
	return nil
}

type pdfTransform [6]float64

func (matrix pdfTransform) compose(local pdfTransform) pdfTransform {
	return pdfTransform{
		matrix[0]*local[0] + matrix[2]*local[1], matrix[1]*local[0] + matrix[3]*local[1],
		matrix[0]*local[2] + matrix[2]*local[3], matrix[1]*local[2] + matrix[3]*local[3],
		matrix[0]*local[4] + matrix[2]*local[5] + matrix[4], matrix[1]*local[4] + matrix[3]*local[5] + matrix[5],
	}
}

func positionPDFImages(page pdf.Page, images []pdfImage) []pdfImage {
	var positioned []pdfImage
	used := make(map[int]bool)
	var walk func(pdf.Value, pdf.Value, pdfTransform, string, int)
	walk = func(contents, resources pdf.Value, matrix pdfTransform, prefix string, depth int) {
		if depth > 32 {
			panic("PDF image form nesting exceeds 32 levels")
		}
		var stack []pdfTransform
		pdf.Interpret(contents, func(operands *pdf.Stack, operator string) {
			args := make([]pdf.Value, operands.Len())
			for index := len(args) - 1; index >= 0; index-- {
				args[index] = operands.Pop()
			}
			switch operator {
			case "q":
				stack = append(stack, matrix)
			case "Q":
				if len(stack) > 0 {
					matrix, stack = stack[len(stack)-1], stack[:len(stack)-1]
				}
			case "cm":
				if len(args) == 6 {
					var local pdfTransform
					for index := range local {
						local[index] = args[index].Float64()
					}
					matrix = matrix.compose(local)
				}
			case "Do":
				if len(args) != 1 {
					return
				}
				name := args[0].Name()
				qualifiedName := name
				if prefix != "" {
					qualifiedName = prefix + "." + name
				}
				object := resources.Key("XObject").Key(name)
				if object.Key("Subtype").Name() == "Form" {
					local := pdfTransform{1, 0, 0, 1, 0, 0}
					if values := object.Key("Matrix"); values.Len() == 6 {
						for index := range local {
							local[index] = values.Index(index).Float64()
						}
					}
					formResources := object.Key("Resources")
					if formResources.IsNull() {
						formResources = resources
					}
					walk(object, formResources, matrix.compose(local), qualifiedName, depth+1)
				} else if object.Key("Subtype").Name() == "Image" {
					for index, resource := range images {
						if resource.name != qualifiedName {
							continue
						}
						resource.x, resource.right = math.Inf(1), math.Inf(-1)
						resource.y = math.Inf(-1)
						for _, corner := range [][2]float64{{0, 0}, {0, 1}, {1, 0}, {1, 1}} {
							x := matrix[0]*corner[0] + matrix[2]*corner[1] + matrix[4]
							y := matrix[1]*corner[0] + matrix[3]*corner[1] + matrix[5]
							resource.x, resource.right = math.Min(resource.x, x), math.Max(resource.right, x)
							resource.y = math.Max(resource.y, y)
						}
						resource.positioned = true
						positioned = append(positioned, resource)
						used[index] = true
					}
				}
			}
		})
	}
	if !page.V.Key("Contents").IsNull() {
		walk(page.V.Key("Contents"), page.Resources(), pdfTransform{1, 0, 0, 1, 0, 0}, "", 0)
	}
	for index, resource := range images {
		if !used[index] {
			positioned = append(positioned, resource)
		}
	}
	return positioned
}

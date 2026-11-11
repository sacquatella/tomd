package tools

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

type pdfLine struct {
	text               string
	x, right, y        float64
	size               float64
	bold               bool
	image              bool
	weight, boldWeight int
}

var pdfNumberedHeading = regexp.MustCompile(`^(\d+(?:\.\d+)*)\.?\s+\S`)

var pdfLigatures = strings.NewReplacer("\ufb00", "ff", "\ufb01", "fi", "\ufb02", "fl", "\ufb03", "ffi", "\ufb04", "ffl")

func pdfTextLines(texts []pdf.Text) ([]pdfLine, float64) {
	glyphs := make([]pdf.Text, 0, len(texts))
	sizes := make(map[float64]int)
	for _, text := range texts {
		text.S = pdfLigatures.Replace(text.S)
		if strings.TrimSpace(text.S) == "" && text.S != " " {
			continue
		}
		text.FontSize = math.Abs(text.FontSize)
		if text.FontSize == 0 {
			text.FontSize = 12
		}
		glyphs = append(glyphs, text)
		sizes[math.Round(text.FontSize*4)/4] += utf8.RuneCountInString(strings.TrimSpace(text.S))
	}
	bodySize, weight := 12.0, 0
	for size, count := range sizes {
		if count > weight || count == weight && size > bodySize {
			bodySize, weight = size, count
		}
	}
	sort.SliceStable(glyphs, func(first, second int) bool {
		if glyphs[first].Y != glyphs[second].Y {
			return glyphs[first].Y > glyphs[second].Y
		}
		return glyphs[first].X < glyphs[second].X
	})
	var lines []pdfLine
	for start := 0; start < len(glyphs); {
		end := start + 1
		for end < len(glyphs) && math.Abs(glyphs[end].Y-glyphs[start].Y) <= math.Max(glyphs[start].FontSize, glyphs[end].FontSize)*0.5 {
			end++
		}
		row := append([]pdf.Text(nil), glyphs[start:end]...)
		sort.SliceStable(row, func(first, second int) bool { return row[first].X < row[second].X })
		line := pdfLine{x: row[0].X, y: row[0].Y}
		var content strings.Builder
		for _, glyph := range row {
			gap := glyph.X - line.right
			if content.Len() > 0 && gap > bodySize*1.5 {
				line.text = strings.TrimSpace(content.String())
				if line.text != "" {
					lines = append(lines, line)
				}
				content.Reset()
				line = pdfLine{x: glyph.X, y: glyph.Y}
			} else if content.Len() > 0 && gap > glyph.FontSize*0.15 && !strings.HasSuffix(content.String(), " ") && glyph.S != " " {
				content.WriteByte(' ')
			}
			content.WriteString(glyph.S)
			width := glyph.W
			if width == 0 {
				width = glyph.FontSize * 0.4 * float64(utf8.RuneCountInString(glyph.S))
			}
			line.right = math.Max(line.right, glyph.X+width)
			line.size = math.Max(line.size, glyph.FontSize)
			font := strings.ToLower(glyph.Font)
			weight := utf8.RuneCountInString(strings.TrimSpace(glyph.S))
			line.weight += weight
			if strings.Contains(font, "bold") || strings.Contains(font, "cmbx") || strings.Contains(font, "demi") {
				line.boldWeight += weight
			}
			line.bold = line.weight > 0 && float64(line.boldWeight)/float64(line.weight) >= 0.8
		}
		line.text = strings.TrimSpace(content.String())
		if line.text != "" {
			lines = append(lines, line)
		}
		start = end
	}
	return lines, bodySize
}

func pdfHeadingLevel(line pdfLine, bodySize float64) int {
	if line.size >= bodySize*1.6 {
		return 1
	}
	if line.size >= bodySize*1.25 {
		return 2
	}
	if len(line.text) <= 100 && line.size >= bodySize*0.95 && !strings.ContainsAny(line.text[len(line.text)-1:], ".,;:") {
		if match := pdfNumberedHeading.FindStringSubmatch(line.text); match != nil {
			return min(6, strings.Count(match[1], ".")+2)
		}
	}
	if line.size >= bodySize*1.12 && line.bold {
		return 3
	}
	if line.bold && len(line.text) <= 100 && line.size >= bodySize*0.95 && !strings.HasSuffix(line.text, ".") {
		if match := pdfNumberedHeading.FindStringSubmatch(line.text); match != nil {
			return min(6, strings.Count(match[1], ".")+2)
		}
		return 3
	}
	return 0
}

func orderPDFLines(lines []pdfLine, bodySize float64) []pdfLine {
	split, bestScore := 0.0, 2
	for index, left := range lines {
		for _, right := range lines[index+1:] {
			if math.Abs(left.y-right.y) > bodySize*0.5 || right.x-left.right < bodySize*1.5 {
				continue
			}
			candidate := (left.right + right.x) / 2
			leftCount, rightCount, crossing := 0, 0, 0
			for _, line := range lines {
				if line.image || line.size > bodySize*1.12 {
					continue
				}
				if line.right <= candidate {
					leftCount++
				} else if line.x >= candidate {
					rightCount++
				} else {
					crossing++
				}
			}
			if score := min(leftCount, rightCount) - crossing*2; score > bestScore {
				split, bestScore = candidate, score
			}
		}
	}
	if split == 0 {
		return lines
	}
	ordered := make([]pdfLine, 0, len(lines))
	var leftColumn, rightColumn []pdfLine
	flush := func() {
		ordered = append(ordered, leftColumn...)
		ordered = append(ordered, rightColumn...)
		leftColumn, rightColumn = nil, nil
	}
	for _, line := range lines {
		if line.x < split && line.right > split || line.size >= bodySize*1.6 {
			flush()
			ordered = append(ordered, line)
		} else if line.x < split {
			leftColumn = append(leftColumn, line)
		} else {
			rightColumn = append(rightColumn, line)
		}
	}
	flush()
	return ordered
}

func renderPDFText(texts []pdf.Text) string {
	return renderPDFContent(texts, nil)
}

func renderPDFContent(texts []pdf.Text, images []pdfImage) string {
	lines, bodySize := pdfTextLines(texts)
	var unplaced []string
	for _, resource := range images {
		link := resource.markdown()
		if resource.positioned {
			lines = append(lines, pdfLine{text: link, x: resource.x, right: resource.right, y: resource.y, image: true})
		} else {
			unplaced = append(unplaced, link)
		}
	}
	sort.SliceStable(lines, func(first, second int) bool {
		if lines[first].y != lines[second].y {
			return lines[first].y > lines[second].y
		}
		return lines[first].x < lines[second].x
	})
	lines = orderPDFLines(lines, bodySize)
	var markdown strings.Builder
	var paragraph strings.Builder
	flush := func() {
		if paragraph.Len() > 0 {
			markdown.WriteString(paragraph.String())
			markdown.WriteString("\n\n")
			paragraph.Reset()
		}
	}
	var previous pdfLine
	for _, line := range lines {
		if line.image {
			flush()
			markdown.WriteString(line.text + "\n\n")
			previous = line
			continue
		}
		level := pdfHeadingLevel(line, bodySize)
		if level > 0 {
			flush()
			markdown.WriteString(strings.Repeat("#", level) + " " + line.text + "\n\n")
		} else {
			if paragraph.Len() > 0 && (previous.y-line.y > bodySize*1.65 || line.y >= previous.y || line.x-previous.x > bodySize*0.8 || math.Abs(line.size-previous.size) > bodySize*0.15) {
				flush()
			}
			if paragraph.Len() > 0 {
				firstRune, _ := utf8.DecodeRuneInString(line.text)
				if strings.HasSuffix(paragraph.String(), "-") && unicode.IsLower(firstRune) {
					joined := strings.TrimSuffix(paragraph.String(), "-")
					paragraph.Reset()
					paragraph.WriteString(joined)
				} else {
					paragraph.WriteByte(' ')
				}
			}
			paragraph.WriteString(line.text)
		}
		previous = line
	}
	flush()
	for _, link := range unplaced {
		markdown.WriteString(link + "\n\n")
	}
	return markdown.String()
}

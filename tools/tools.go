// Copyright © 2024 Acquatella Stephan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tools

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"github.com/PuerkitoBio/goquery"
	"github.com/abadojack/whatlanggo"
	"github.com/apcera/termtables"
	log "github.com/sirupsen/logrus"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"
)

var Insecure bool

// CheckError display error on screen
func CheckError(err error) {
	// get caller function name
	pc, _, _, _ := runtime.Caller(1)
	if err != nil {
		fmt.Println("From ", runtime.FuncForPC(pc).Name())
		fmt.Println("An issue occur during page processing: ", err.Error())
		os.Exit(1)
	}
}

// BuildMetadata build metadata for a page as markdown header
func BuildMetadata(content *goquery.Document, url string, prefix string, complement Metadata) (string, Metadata) {

	var metaData Metadata
	// set title
	defaultTitle := strings.ReplaceAll(content.Find("title").Text(), "/", "-")
	// override title if complement.title is not empty
	if complement.Title != "" {
		// remove "/" values in title string
		metaData.Title = complement.Title
	} else {
		metaData.Title = defaultTitle
	}
	// set description base on <meta name="description" content="xxx">
	defaultDescription := content.Find("meta[name='description']").AttrOr("content", "")
	// override title if complement.title is not empty
	if complement.Description != "" {
		// remove "/" values in title string
		metaData.Description = complement.Description
	} else {
		metaData.Description = defaultDescription
	}

	// Build doc_id as TITLE in UPPERCASE WITHOUT SPACE
	// set doc_id as prefix + "_" + content.ID
	doc_id := strings.ReplaceAll(strings.ToUpper(metaData.Title), " ", "")
	// replace accentuated characters with their non-accentuated equivalent
	doc_id = strings.NewReplacer("À", "A", "Á", "A", "Â", "A", "Ã", "A", "Ä", "A",
		"Å", "A", "Æ", "AE", "Ç", "C", "È", "E", "É", "E",
		"Ê", "E", "Ë", "E", "Ì", "I", "Í", "I", "Î", "I",
		"Ï", "I", "Ð", "D", "Ñ", "N", "Ò", "O", "Ó", "O",
		"Ô", "O", "Õ", "O", "Ö", "O", "Ø", "O", "Ù", "U",
		"Ú", "U", "Û", "U", "Ü", "U", "Ý", "Y", "Þ", "TH",
		"ß", "ss", "à", "a", "á", "a", "â", "a", "ã", "a",
		"ä", "a", "å", "a", "æ", "ae", "ç", "c", "è", "e",
		"é", "e", "ê", "e", "ë", "e", "ì", "i", "í", "i",
		"î", "i", "ï", "i", "ð", "d", "ñ", "n", "ò", "o",
		"ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
		"ù", "u", "ú", "u", "û", "u", "ü", "u", "ý", "y",
		"þ", "th", "ÿ", "y").Replace(doc_id)
	// remove all special characters except alphanumeric and underscore
	doc_id = regexp.MustCompile(`[^a-zA-Z0-9_]`).ReplaceAllString(doc_id, "")
	// set doc_id as prefix + "_" + content.ID
	metaData.Doc_id = strings.ToUpper(prefix + "_" + doc_id)
	//
	// override description if complement.description is not empty
	if complement.Description != "" {
		metaData.Description = complement.Description
	} else {
		metaData.Description = defaultTitle
	}
	// add new tags if complement.tags is not empty
	if len(complement.Tags) > 0 {
		for _, tag := range complement.Tags {
			metaData.Tags = append(metaData.Tags, tag)
		}
	}
	metaData.Tags = append(metaData.Tags, "web")
	// set site_url
	metaData.Site_url = url
	// add new authors if complement.authors is not empty
	if len(complement.Authors) > 0 {
		for _, author := range complement.Authors {
			metaData.Authors = append(metaData.Authors, author)
		}
	}
	webPageAuthor := content.Find("meta[name='author']").AttrOr("content", "")
	if webPageAuthor != "" {
		metaData.Authors = append(metaData.Authors, webPageAuthor)
	}
	// date should be in ISO 8601 format without seconds
	metaData.Creation_date = content.Find("meta[name='date']").AttrOr("content", time.Now().Format("2006-01-02T15:04:05"))
	metaData.Last_update_date = content.Find("meta[name='update-date']").AttrOr("content", time.Now().Format("2006-01-02T15:04:05"))

	metaData.Visibility = "Interne"

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

	// Add metadata header to markdown with title , doc_id,description , tags, site_url, authors, creation_date, last_update

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

// BuildFileMetadata build metadata for a docs or pdf file.
func BuildFileMetadata(docpath string, url string, prefix string, meta Metadata, complement Metadata) (string, Metadata) {
	var metaData Metadata

	//metaData.Title = strings.ReplaceAll(filepath.Base(docpath), filepath.Ext(docpath), "")
	defaultTitle := strings.ReplaceAll(filepath.Base(docpath), filepath.Ext(docpath), "")

	if complement.Title != "" {
		// remove "/" values in title string
		metaData.Title = complement.Title
	} else if meta.Title != "" {
		metaData.Title = meta.Title
	} else {
		metaData.Title = defaultTitle
	}

	if complement.Description != "" {
		metaData.Description = complement.Description
	} else if meta.Description != "" {
		metaData.Description = meta.Description
	} else {
		metaData.Description = defaultTitle
	}

	if meta.Authors != nil {
		metaData.Authors = meta.Authors
	}

	// Build doc_id as TITLE in UPPERCASE WITHOUT SPACE
	// set doc_id as prefix + "_" + content.ID
	doc_id := strings.ReplaceAll(strings.ToUpper(metaData.Title), " ", "")
	metaData.Doc_id = strings.ToUpper(prefix + "_" + doc_id)

	// Add metadata header to markdown with title , doc_id,description , tags, site_url, authors, creation_date, last_update
	metaData.Tags = append(metaData.Tags, "file")
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

	// date should be in ISO 8601 format without seconds
	metaData.Creation_date = time.Now().Format("2006-01-02T15:04:05")
	metaData.Last_update_date = time.Now().Format("2006-01-02T15:04:05")

	metaData.Visibility = "Internal"

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

// GetImgList get all images from a web page and return a list of image url
func GetImgList(content *goquery.Document, ispath string, scheme string, domain string) ([]string, error) {

	var imgList []string
	content.Find("img").Each(func(i int, s *goquery.Selection) {
		imgUrl, _ := s.Attr("src")
		if ispath != "" {
			imgUrl = ispath + "/" + imgUrl
		} else {
			if !strings.HasPrefix(imgUrl, "http") {
				if strings.HasPrefix(imgUrl, "/") {
					imgUrl = scheme + "://" + domain + imgUrl
				} else {
					imgUrl = scheme + "://" + domain + "/" + imgUrl
				}
			}
		}

		imgList = append(imgList, imgUrl)
	})
	log.Info("Images list: ", imgList)
	return imgList, nil
}

// GetPage get a web page by it url and return a Page struct
func GetPage(url string, customerId string, exportDir string, complements Metadata, domain string, ia bool) (Page, error) {

	var grReader io.Reader
	var err error
	var isPath string

	// Get web page content from url or local file
	if strings.HasPrefix(url, "http") {
		// Check is option -k is set, and if yes, don't check certificate
		if Insecure {
			http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		webpageReader, err := http.Get(url)
		CheckError(err)
		if webpageReader.StatusCode != http.StatusOK {
			log.Errorf("Error fetching URL %s: %s", url, webpageReader.Status)
			return Page{PageId: "Error", Url: url, MdFile: webpageReader.Status}, nil
		}
		grReader = io.Reader(webpageReader.Body)
		defer webpageReader.Body.Close()
	} else {
		webpageReader, err := os.Open(url)
		isPath = filepath.Dir(url)
		CheckError(err)
		grReader = io.Reader(webpageReader)
		defer webpageReader.Close()
	}

	// Get web page content
	doc, err := goquery.NewDocumentFromReader(grReader)
	CheckError(err)
	content := doc.Find("body")

	if domain == "" && regexp.MustCompile(`(?i)^http`).MatchString(url) {
		domain = md.DomainFromURL(url)
	}

	//fmt.Printf("ID: %s\n", content.ID)

	converter := md.NewConverter(domain, true, nil)
	converter.Use(plugin.ConfluenceCodeBlock())
	converter.Use(plugin.ConfluenceAttachments())
	converter.Use(plugin.GitHubFlavored())
	markdown := converter.Convert(content)
	if err != nil {
		log.Fatal(err)
		return Page{}, err
	}

	// Add metadata header to markdown with title , doc_id,description , tags, site_url, authors, creation_date, last_update
	metadata, metaDatas := BuildMetadata(doc, url, customerId, complements)
	// Add metadata header to markdown
	markdown = metadata + markdown

	// get url scheme
	scheme := regexp.MustCompile(`(?i)^http`).FindString(url)

	// identify language for markdown content
	infol := whatlanggo.Detect(markdown)
	lang := infol.Lang.String()

	log.Infof("Language detected: %s at %f for %s", lang, infol.Confidence, url)

	// If imgDesc is not empty, add image description to markdown
	if ia {
		// Get all images from web page
		imgList, err := GetImgList(doc, isPath, scheme, domain)
		if err != nil {
			log.Fatal(err)
			return Page{}, err
		}
		markdown = markdown + "\n" + imageDescriptionAsMd(imgList, lang)
	}

	exportedFile := BuildFilename(metaDatas.Title, exportDir, customerId)

	// save markdown to file
	err = os.WriteFile(exportedFile, []byte(markdown), 0644)
	if err != nil {
		log.Fatal(err)
		return Page{}, err
	}
	return Page{PageId: metaDatas.Doc_id, Url: metaDatas.Site_url, MdFile: exportedFile}, nil
}

// ReadPages read pages list from json file and return a list of Metadata
func ReadPages(filename string) ([]Metadata, error) {
	// read json file with following format
	//  [{"url":"https://en.wikipedia.org/wiki/Wikipedia", "description":"Home page Wikipedia","title":"","tags":["tag1","tag2"]},]
	// return a list of Metadata
	var pages []Metadata
	// read json file
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// decode json file
	err = json.NewDecoder(file).Decode(&pages)
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// DisplayOnScreen display pages on screen as text table
func DisplayOnScreen(exportedPages []Page) {
	table := termtables.CreateTable()
	table.AddHeaders("Page ID", "Url", "Markdown files")
	for _, page := range exportedPages {
		table.AddRow(page.PageId, page.Url, page.MdFile)
	}
	fmt.Println(table.Render())
}

// BuildFilename build md filename clean from special characters
func BuildFilename(title string, dir string, id string) string {

	title = SanitizeFilename(title)

	title = strings.ToLower(title) + ".md"
	filename := dir + "/" + id + "-" + title

	return filename
}

// removeAccents remove accents from a string
func removeAccents(s string) string {
	t := transform.Chain(norm.NFD, transform.RemoveFunc(isMn), norm.NFC)
	result, _, _ := transform.String(t, s)
	return result
}

// isMn filters characters of type "Mark, nonspacing" (accents).
func isMn(r rune) bool {
	return unicode.Is(unicode.Mn, r)
}

// SanitizeFilename clean a filename by removing special characters, replacing spaces with hyphens, and removing accents
func SanitizeFilename(filename string) string {

	// Replace long dashes (EN DASH –, EM DASH —) with a simple dash
	filename = strings.ReplaceAll(filename, "–", "-")
	filename = strings.ReplaceAll(filename, "—", "-")

	// Remove forbidden characters + quotes + backticks + apostrophes
	re := regexp.MustCompile(`[<>:"/\\|?*'\x60"\x00-\x1F]`)
	filename = re.ReplaceAllString(filename, "")

	// Replace spaces with hyphens
	filename = strings.ReplaceAll(filename, " ", "-")

	// Reduces multiple dashes to a single dash
	reDash := regexp.MustCompile(`-+`)
	filename = reDash.ReplaceAllString(filename, "-")

	// Removes accents
	filename = removeAccents(filename)

	// Set to lower case
	filename = strings.ToLower(filename)

	// remove leading and trailing dashes
	filename = strings.Trim(filename, "-")

	return filename
}

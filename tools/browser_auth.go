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
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"github.com/PuerkitoBio/goquery"
	"github.com/abadojack/whatlanggo"
	log "github.com/sirupsen/logrus"
)

// BrowserAuth provides a simple browser-based authentication method
// that captures the user's session cookies after they authenticate
type BrowserAuth struct {
	SessionCookies []*http.Cookie
	UserAgent      string
}

// GetPageWithBrowserAuth opens browser for user to authenticate, then uses the session
// This is simpler than OAuth as it doesn't require client ID/secret
func GetPageWithBrowserAuth(url string) (string, error) {
	fmt.Println("\n🔐 Authentication interactive requise")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\n📋 Instructions pour récupérer les cookies:")
	fmt.Println("  1. Le navigateur va s'ouvrir sur la page SharePoint")
	fmt.Println("  2. Connectez-vous si nécessaire")
	fmt.Println("  3. Une fois sur la page, appuyez sur F12 (DevTools)")
	fmt.Println("  4. Allez dans l'onglet 'Application' (ou 'Stockage')")
	fmt.Println("  5. Dans la section 'Cookies', sélectionnez votre domaine")
	fmt.Println("  6. Copiez TOUS les cookies (Ctrl+A puis Ctrl+C)")
	fmt.Println("\n💡 Méthode alternative (via Console):")
	fmt.Println("  1. Ouvrez les DevTools (F12)")
	fmt.Println("  2. Console > Tapez: document.cookie")
	fmt.Println("  3. Copiez la valeur complète affichée")
	fmt.Println("\n⏳ Appuyez sur Entrée quand vous êtes prêt...")

	var ready string
	fmt.Scanln(&ready)

	// Open browser
	if err := openBrowser(url); err != nil {
		log.Warnf("Impossible d'ouvrir le navigateur: %v", err)
		fmt.Println("⚠️  Veuillez ouvrir manuellement:", url)
	}

	fmt.Println("\n📝 Collez vos cookies ici (puis appuyez sur Entrée):")
	fmt.Println("    (Les cookies doivent être au format: nom1=valeur1; nom2=valeur2; ...)")
	fmt.Print("\n> ")

	// Read entire line including spaces
	reader := bufio.NewReader(os.Stdin)
	cookieStr, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("erreur de lecture: %w", err)
	}

	cookieStr = strings.TrimSpace(cookieStr)

	if cookieStr == "" {
		return "", fmt.Errorf("aucun cookie fourni")
	}

	// Validate cookies format
	if !strings.Contains(cookieStr, "=") {
		return "", fmt.Errorf("format de cookies invalide (doit contenir '=')")
	}

	return cookieStr, nil
}

// GetPageContentWithCookies fetches page content using provided cookies
func GetPageContentWithCookies(url string, cookieString string) (string, error) {
	// Create cookie jar
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// Create HTTP client with cookies
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: getTLSConfig(),
		},
	}

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add cookies to request
	req.Header.Set("Cookie", cookieString)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch page, status: %d", resp.StatusCode)
	}

	// Read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}

// SaveCookiesToFile saves cookies to a file for reuse
func SaveCookiesToFile(cookies string, filename string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := filepath.Join(homeDir, ".tomd")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	cookieFile := filepath.Join(configDir, filename)
	return os.WriteFile(cookieFile, []byte(cookies), 0600)
}

// LoadCookiesFromFile loads cookies from a file
func LoadCookiesFromFile(filename string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	cookieFile := filepath.Join(homeDir, ".tomd", filename)
	data, err := os.ReadFile(cookieFile)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// getTLSConfig returns TLS configuration
func getTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: Insecure,
	}
}

// GetPageWithSimpleAuth fetches and processes a page using browser session cookies
func GetPageWithSimpleAuth(url string, customerId string, exportDir string, complements Metadata, domain string, ia bool, useBrowserAuth bool) (Page, error) {
	var htmlContent string
	var err error

	if useBrowserAuth {
		// Try to load saved cookies first
		cookieFileName := "sharepoint_cookies.txt"
		cookies, err := LoadCookiesFromFile(cookieFileName)

		if err != nil || cookies == "" {
			// No saved cookies, do interactive auth
			fmt.Println("\n🔍 Aucune session sauvegardée trouvée")
			cookies, err = GetPageWithBrowserAuth(url)
			if err != nil {
				return Page{}, fmt.Errorf("authentication failed: %w", err)
			}

			// Save cookies for next time
			if cookies != "" {
				if err := SaveCookiesToFile(cookies, cookieFileName); err != nil {
					log.Warnf("Failed to save cookies: %v", err)
				} else {
					fmt.Println("\n✅ Session sauvegardée pour les prochaines utilisations")
				}
			}
		} else {
			fmt.Println("\n♻️  Utilisation de la session sauvegardée")
		}

		// Fetch page with cookies
		htmlContent, err = GetPageContentWithCookies(url, cookies)
		if err != nil {
			// Cookies might be expired, try interactive auth again
			fmt.Println("\n⚠️  Session expirée, nouvelle authentification requise")
			cookies, err = GetPageWithBrowserAuth(url)
			if err != nil {
				return Page{}, fmt.Errorf("authentication failed: %w", err)
			}

			htmlContent, err = GetPageContentWithCookies(url, cookies)
			if err != nil {
				return Page{}, err
			}

			// Save new cookies
			SaveCookiesToFile(cookies, cookieFileName)
		}
	} else {
		return Page{}, fmt.Errorf("browser auth not enabled")
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return Page{}, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Process the document
	return processPageFromDoc(doc, url, customerId, exportDir, complements, domain, ia)
}

// processPageFromDoc processes a goquery document and converts to markdown
func processPageFromDoc(doc *goquery.Document, url string, customerId string, exportDir string, complements Metadata, domain string, ia bool) (Page, error) {
	content := doc.Find("body")

	if domain == "" {
		// Extract domain from URL
		if u, err := neturl.Parse(url); err == nil {
			domain = u.Host
		}
	}

	converter := md.NewConverter(domain, true, nil)
	converter.Use(plugin.ConfluenceCodeBlock())
	converter.Use(plugin.ConfluenceAttachments())
	converter.Use(plugin.GitHubFlavored())
	markdown := converter.Convert(content)

	// Add metadata header
	metadata, metaDatas := BuildMetadata(doc, url, customerId, complements)
	markdown = metadata + markdown

	// Get url scheme
	scheme := "https"
	if u, err := neturl.Parse(url); err == nil {
		scheme = u.Scheme
	}

	// Identify language
	infol := whatlanggo.Detect(markdown)
	lang := infol.Lang.String()
	log.Infof("Language detected: %s at %f for %s", lang, infol.Confidence, url)

	// Add image descriptions if requested
	if ia {
		imgList, err := GetImgList(doc, "", scheme, domain)
		if err != nil {
			log.Warnf("Failed to get images: %v", err)
		} else {
			markdown = markdown + "\n" + imageDescriptionAsMd(imgList, lang)
		}
	}

	exportedFile := BuildFilename(metaDatas.Title, exportDir, customerId)

	// Save markdown to file
	err := os.WriteFile(exportedFile, []byte(markdown), 0644)
	if err != nil {
		return Page{}, fmt.Errorf("failed to save file: %w", err)
	}

	return Page{PageId: metaDatas.Doc_id, Url: metaDatas.Site_url, MdFile: exportedFile}, nil
}

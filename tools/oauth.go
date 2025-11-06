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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
)

// OAuthConfig contains OAuth configuration
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	RedirectURL  string
	Scope        string
	Tenant       string // For SharePoint/Microsoft
}

// DefaultSharePointOAuthConfig returns default OAuth config for SharePoint Online
func DefaultSharePointOAuthConfig(tenant string) OAuthConfig {
	if tenant == "" {
		tenant = "common"
	}
	return OAuthConfig{
		AuthURL:     fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", tenant),
		TokenURL:    fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant),
		RedirectURL: "http://localhost:8080/callback",
		Scope:       "https://graph.microsoft.com/.default offline_access",
		Tenant:      tenant,
	}
}

// GetTokenViaOAuth opens browser for OAuth authentication and returns the token
func GetTokenViaOAuth(config OAuthConfig) (string, error) {
	// Channel to receive the token
	tokenChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Create HTTP server to handle callback
	server := &http.Server{Addr: ":8080"}

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		errorParam := r.URL.Query().Get("error")

		if errorParam != "" {
			errorDesc := r.URL.Query().Get("error_description")
			errChan <- fmt.Errorf("OAuth error: %s - %s", errorParam, errorDesc)
			fmt.Fprintf(w, "<html><body><h1>❌ Authentication Failed</h1><p>%s</p><p>You can close this window.</p></body></html>", errorDesc)
			return
		}

		if code == "" {
			errChan <- fmt.Errorf("no authorization code received")
			fmt.Fprintf(w, "<html><body><h1>❌ Error</h1><p>No authorization code received.</p><p>You can close this window.</p></body></html>")
			return
		}

		// Exchange code for token
		token, err := exchangeCodeForToken(code, config)
		if err != nil {
			errChan <- err
			fmt.Fprintf(w, "<html><body><h1>❌ Token Exchange Failed</h1><p>%s</p><p>You can close this window.</p></body></html>", err.Error())
			return
		}

		tokenChan <- token
		fmt.Fprintf(w, "<html><body><h1>✅ Authentication Successful!</h1><p>Token received successfully.</p><p>You can close this window and return to the terminal.</p></body></html>")
	})

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("Server error: %v", err)
		}
	}()

	// Build authorization URL
	authURL := buildAuthURL(config)

	// Open browser
	fmt.Println("🌐 Opening browser for authentication...")
	fmt.Printf("If the browser doesn't open automatically, please visit:\n%s\n\n", authURL)

	if err := openBrowser(authURL); err != nil {
		log.Warnf("Failed to open browser automatically: %v", err)
	}

	fmt.Println("⏳ Waiting for authentication... (Press Ctrl+C to cancel)")

	// Wait for token or error with timeout
	select {
	case token := <-tokenChan:
		// Shutdown server gracefully
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		fmt.Println("✅ Authentication successful!")
		return token, nil
	case err := <-errChan:
		// Shutdown server gracefully
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		return "", err
	case <-time.After(5 * time.Minute):
		// Timeout after 5 minutes
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		return "", fmt.Errorf("authentication timeout")
	}
}

// buildAuthURL constructs the OAuth authorization URL
func buildAuthURL(config OAuthConfig) string {
	params := url.Values{}
	params.Add("client_id", config.ClientID)
	params.Add("response_type", "code")
	params.Add("redirect_uri", config.RedirectURL)
	params.Add("scope", config.Scope)
	params.Add("response_mode", "query")

	return config.AuthURL + "?" + params.Encode()
}

// exchangeCodeForToken exchanges authorization code for access token
func exchangeCodeForToken(code string, config OAuthConfig) (string, error) {
	data := url.Values{}
	data.Set("client_id", config.ClientID)
	data.Set("client_secret", config.ClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", config.RedirectURL)
	data.Set("grant_type", "authorization_code")

	resp, err := http.PostForm(config.TokenURL, data)
	if err != nil {
		return "", fmt.Errorf("failed to exchange code for token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status: %d", resp.StatusCode)
	}

	// Parse response
	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	return result.AccessToken, nil
}

// openBrowser opens the default browser with the given URL
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

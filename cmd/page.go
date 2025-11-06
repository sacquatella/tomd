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

package cmd

import (
	"fmt"

	"github.com/sacquatella/tomd/tools"
	"github.com/spf13/cobra"
)

var Url string
var AuthUsername string
var AuthPassword string
var AuthToken string
var AuthType string
var UseOAuth bool
var OAuthClientID string
var OAuthClientSecret string
var OAuthTenant string
var UseBrowserAuth bool

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Get a web page as a markdown file",
	Long:  `Get web page by it url and generate a markdown page with metadata's'.`,
	Run:   getWebPage,
}

func init() {
	rootCmd.AddCommand(pageCmd)
	pageCmd.PersistentFlags().StringVarP(&Url, "url", "u", "", "Page URL or folder")
	pageCmd.PersistentFlags().StringVarP(&CustomerId, "cid", "c", "web", "Customer ID code ")
	pageCmd.PersistentFlags().BoolVarP(&tools.Insecure, "unsecure", "k", false, "Allow unsecure certificate")
	pageCmd.PersistentFlags().StringVar(&AuthUsername, "auth-user", "", "Username for authentication (SharePoint, Basic Auth)")
	pageCmd.PersistentFlags().StringVar(&AuthPassword, "auth-pass", "", "Password for authentication")
	pageCmd.PersistentFlags().StringVar(&AuthToken, "auth-token", "", "Bearer token for authentication")
	pageCmd.PersistentFlags().StringVar(&AuthType, "auth-type", "", "Authentication type: basic, bearer, or ntlm (default: auto)")
	pageCmd.PersistentFlags().BoolVar(&UseBrowserAuth, "browser-auth", false, "Use browser session cookies (simple, no client ID needed)")
	pageCmd.PersistentFlags().BoolVar(&UseOAuth, "oauth", false, "Use OAuth browser authentication (requires client ID/secret)")
	pageCmd.PersistentFlags().StringVar(&OAuthClientID, "oauth-client-id", "", "OAuth Client ID (required with --oauth)")
	pageCmd.PersistentFlags().StringVar(&OAuthClientSecret, "oauth-client-secret", "", "OAuth Client Secret (required with --oauth)")
	pageCmd.PersistentFlags().StringVar(&OAuthTenant, "oauth-tenant", "common", "OAuth Tenant ID (for SharePoint/Microsoft, default: common)")
}

// getWebPage get a web page by its id and generate a markdown page with its metadatas
func getWebPage(cmd *cobra.Command, args []string) {
	var pages []tools.Page
	var datas tools.Page
	var err error

	// Priority: browser-auth > oauth > basic auth > no auth
	if UseBrowserAuth {
		// Simple browser authentication using session cookies
		datas, err = tools.GetPageWithSimpleAuth(Url, CustomerId, ExportDir, tools.Metadata{}, "", ImgDesc, true)
		tools.CheckError(err)
	} else if UseOAuth {
		// OAuth authentication with client ID/secret
		if OAuthClientID == "" {
			tools.CheckError(fmt.Errorf("--oauth-client-id is required when using --oauth"))
		}
		if OAuthClientSecret == "" {
			tools.CheckError(fmt.Errorf("--oauth-client-secret is required when using --oauth"))
		}

		// Get OAuth configuration
		oauthConfig := tools.DefaultSharePointOAuthConfig(OAuthTenant)
		oauthConfig.ClientID = OAuthClientID
		oauthConfig.ClientSecret = OAuthClientSecret

		// Get token via browser authentication
		token, err := tools.GetTokenViaOAuth(oauthConfig)
		tools.CheckError(err)

		// Use the token for authentication
		auth := &tools.AuthConfig{
			Token:    token,
			AuthType: "bearer",
		}

		datas, err = tools.GetPageWithAuth(Url, CustomerId, ExportDir, tools.Metadata{}, "", ImgDesc, auth)
		tools.CheckError(err)
	} else if AuthUsername != "" || AuthPassword != "" || AuthToken != "" {
		// Basic/Bearer/NTLM authentication
		auth := &tools.AuthConfig{
			Username: AuthUsername,
			Password: AuthPassword,
			Token:    AuthToken,
			AuthType: AuthType,
		}

		datas, err = tools.GetPageWithAuth(Url, CustomerId, ExportDir, tools.Metadata{}, "", ImgDesc, auth)
		tools.CheckError(err)
	} else {
		// No authentication
		datas, err = tools.GetPage(Url, CustomerId, ExportDir, tools.Metadata{}, "", ImgDesc)
		tools.CheckError(err)
	}

	pages = append(pages, datas)
	tools.DisplayOnScreen(pages)
}

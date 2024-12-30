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
	"github.com/sacquatella/tomd/tools"
	"github.com/spf13/cobra"
)

var Url string

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
}

// getWebPage get a web page by its id and generate a markdown page with its metadatas
func getWebPage(cmd *cobra.Command, args []string) {
	var pages []tools.Page
	datas, err := tools.GetPage(Url, CustomerId, ExportDir, tools.Metadata{}, "", ImgDesc)
	tools.CheckError(err)
	pages = append(pages, datas)
	tools.DisplayOnScreen(pages)

}

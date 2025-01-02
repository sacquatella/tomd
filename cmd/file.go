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
	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var PagesFile string

var fileCmd = &cobra.Command{
	Use:   "file",
	Short: "Get a list of web pages as markdown files",
	Long:  `Get a list of web pages by their id and generate a markdown page with metadata's`,
	Run:   getWebPages,
}

func init() {
	rootCmd.AddCommand(fileCmd)
	fileCmd.PersistentFlags().StringVarP(&PagesFile, "file", "f", "", "pages list as json file")
	fileCmd.PersistentFlags().StringVarP(&CustomerId, "cid", "c", "web", "Customer ID code ")
	fileCmd.PersistentFlags().BoolVarP(&tools.Insecure, "unsecure", "k", false, "Allow unsecure certificate")
	//log = tools.InitLog(Verbose)
}

// getConfluencePages get a list of confluence pages by their id and generate a markdown page with their metadatas
func getWebPages(cmd *cobra.Command, args []string) {

	// Read json file
	pages, err := tools.ReadPages(PagesFile)
	tools.CheckError(err)

	logger.Info("Pages list read from file : ", PagesFile)

	// Display on screen
	var pagelist []tools.Page

	// loop on pages and get content
	for _, page := range pages {
		datas, err := tools.GetPage(page.Site_url, CustomerId, ExportDir, page, "", ImgDesc)
		tools.CheckError(err)
		pagelist = append(pagelist, datas)
	}
	tools.DisplayOnScreen(pagelist)
}

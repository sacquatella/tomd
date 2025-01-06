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
	"github.com/sacquatella/tomd/docx2md"
	"github.com/sacquatella/tomd/tools"
	"github.com/spf13/cobra"
)

var Docx string
var CustomerIdDocx string

var docxCmd = &cobra.Command{
	Use:   "docx",
	Short: "Get Docx text content as a markdown file",
	Long:  `Get Docx text content and generate a markdown page with metadata's'.`,
	Run:   getDocxDocument,
}

func init() {
	rootCmd.AddCommand(docxCmd)
	docxCmd.PersistentFlags().StringVarP(&Docx, "docx", "x", "", "Docx file")
	docxCmd.PersistentFlags().StringVarP(&Url, "url", "u", "", "Page URL for metadata")
	docxCmd.PersistentFlags().StringVarP(&CustomerIdDocx, "cid", "c", "docx", "Customer ID code ")
}

// getWebPage get a web page by its id and generate a markdown page with its metadatas
func getDocxDocument(cmd *cobra.Command, args []string) {
	var pages []tools.Page
	datas, err := docx2md.GetDocx(Docx, Url, CustomerIdDocx, ExportDir, tools.Metadata{})
	tools.CheckError(err)
	pages = append(pages, datas)
	tools.DisplayOnScreen(pages)

}

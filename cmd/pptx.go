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

var Pptx string
var CustomerIdPptx string

var pptxCmd = &cobra.Command{
	Use:   "pptx",
	Short: "Get pptx text content as a markdown file",
	Long:  `Get pptx text content and generate a markdown page with metadata's'.`,
	Run:   getPptxDocument,
}

func init() {
	rootCmd.AddCommand(pptxCmd)
	pptxCmd.PersistentFlags().StringVarP(&Pptx, "pptx", "s", "", "Pptx file")
	pptxCmd.PersistentFlags().StringVarP(&Url, "url", "u", "", "Page URL for metadata")
	pptxCmd.PersistentFlags().StringVarP(&CustomerIdPptx, "cid", "c", "pptx", "Customer ID code ")
}

// getPptxDocument read pptx and generate a markdown page with its metadatas
func getPptxDocument(cmd *cobra.Command, args []string) {
	var pages []tools.Page
	datas, err := docx2md.GetPptx(Pptx, Url, CustomerIdPptx, ExportDir, tools.Metadata{})
	tools.CheckError(err)
	pages = append(pages, datas)
	tools.DisplayOnScreen(pages)

}

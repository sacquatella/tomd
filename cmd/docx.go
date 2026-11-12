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

	"github.com/sacquatella/tomd/docx2md"
	"github.com/sacquatella/tomd/tools"
	"github.com/spf13/cobra"
)

var Docx string
var CustomerIdDocx string
var docxLanguage string

var docxCmd = &cobra.Command{
	Use:   "docx",
	Short: "Extract DOCX content as a Markdown file",
	Long:  `Extract DOCX paragraphs, headings, lists, tables and images with metadata. Optional image descriptions use Ollama or OpenAI.`,
	RunE:  getDocxDocument,
}

func init() {
	rootCmd.AddCommand(docxCmd)
	docxCmd.PersistentFlags().StringVarP(&Docx, "docx", "x", "", "Docx file")
	docxCmd.PersistentFlags().StringVarP(&Url, "url", "u", "", "Page URL for metadata")
	docxCmd.PersistentFlags().StringVarP(&CustomerIdDocx, "cid", "c", "docx", "Customer ID code ")
	docxCmd.Flags().StringVar(&docxLanguage, "language", "", "Language for AI descriptions (default: source document language)")
	_ = docxCmd.MarkPersistentFlagRequired("docx")
}

// getDocxDocument read docx and generate a markdown page with its metadatas
func getDocxDocument(cmd *cobra.Command, args []string) error {
	options := docx2md.DocxOptions{
		Language: docxLanguage, Context: cmd.Context(),
		Warn: func(message string) { fmt.Fprintln(cmd.ErrOrStderr(), "Warning: "+message) },
	}
	if ImgDesc {
		describer, err := tools.NewImageDescriber(tools.AIConfigFromEnv())
		if err != nil {
			return err
		}
		options.Describer = describer
	}
	datas, err := docx2md.GetDocxWithOptions(Docx, Url, CustomerIdDocx, ExportDir, tools.Metadata{}, options)
	if err != nil {
		return err
	}
	tools.DisplayOnScreen([]tools.Page{datas})
	return nil
}

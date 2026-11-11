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

var Pdf string
var CustomerIdPdf string
var pdfLanguage string

var pdfCmd = &cobra.Command{
	Use:   "pdf",
	Short: "Get PDF text content as a markdown file",
	Long:  `Get PDF text content and generate a markdown page with metadata's'.`,
	RunE:  getPdfDocument,
}

func init() {
	rootCmd.AddCommand(pdfCmd)
	pdfCmd.PersistentFlags().StringVarP(&Pdf, "pdf", "p", "", "Pdf file")
	pdfCmd.PersistentFlags().StringVarP(&Url, "url", "u", "", "Page URL for metadata")
	pdfCmd.PersistentFlags().StringVarP(&CustomerIdPdf, "cid", "c", "pdf", "Customer ID code ")
	pdfCmd.Flags().StringVar(&pdfLanguage, "language", "", "Language for AI descriptions (default: source document language)")
}

// getWebPage get a web page by its id and generate a markdown page with its metadatas
func getPdfDocument(cmd *cobra.Command, args []string) error {
	options := tools.PDFOptions{
		Language: pdfLanguage, Context: cmd.Context(),
		Warn: func(message string) { fmt.Fprintln(cmd.ErrOrStderr(), "Warning: "+message) },
	}
	if ImgDesc {
		describer, err := tools.NewImageDescriber(tools.AIConfigFromEnv())
		if err != nil {
			return err
		}
		options.Describer = describer
	}
	datas, err := tools.GetPDFWithOptions(Pdf, Url, CustomerIdPdf, ExportDir, tools.Metadata{}, options)
	if err != nil {
		return err
	}
	tools.DisplayOnScreen([]tools.Page{datas})
	return nil
}

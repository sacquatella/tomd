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

var Pptx string
var CustomerIdPptx string
var pptxExcludeNotes bool
var pptxExcludeHidden bool
var pptxLanguage string

var pptxCmd = &cobra.Command{
	Use:   "pptx",
	Short: "Extract PPTX content as RAG-ready Markdown",
	Long:  `Extract ordered slides, notes, tables, images and native diagram data. Optional image descriptions use Ollama or OpenAI.`,
	RunE:  getPptxDocument,
}

func init() {
	rootCmd.AddCommand(pptxCmd)
	pptxCmd.PersistentFlags().StringVarP(&Pptx, "pptx", "s", "", "Pptx file")
	pptxCmd.PersistentFlags().StringVarP(&Url, "url", "u", "", "Page URL for metadata")
	pptxCmd.PersistentFlags().StringVarP(&CustomerIdPptx, "cid", "c", "pptx", "Customer ID code ")
	pptxCmd.Flags().BoolVar(&pptxExcludeNotes, "no-notes", false, "Exclude presenter notes")
	pptxCmd.Flags().BoolVar(&pptxExcludeHidden, "exclude-hidden", false, "Exclude hidden slides (included by default)")
	pptxCmd.Flags().StringVar(&pptxLanguage, "language", "", "Language for AI descriptions (default: source document language)")
	_ = pptxCmd.MarkPersistentFlagRequired("pptx")
}

// getPptxDocument read pptx and generate a markdown page with its metadatas
func getPptxDocument(cmd *cobra.Command, args []string) error {
	options := docx2md.PptxOptions{
		ExcludeNotes: pptxExcludeNotes, ExcludeHidden: pptxExcludeHidden,
		Language: pptxLanguage, Context: cmd.Context(),
		Warn: func(message string) { fmt.Fprintln(cmd.ErrOrStderr(), "Warning: "+message) },
	}
	if ImgDesc {
		describer, err := tools.NewImageDescriber(tools.AIConfigFromEnv())
		if err != nil {
			return err
		}
		options.Describer = describer
	}
	datas, err := docx2md.GetPptxWithOptions(Pptx, Url, CustomerIdPptx, ExportDir, tools.Metadata{}, options)
	if err != nil {
		return err
	}
	tools.DisplayOnScreen([]tools.Page{datas})
	return nil
}

package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sacquatella/tomd/docx2md"
	"github.com/sacquatella/tomd/tools"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newDocsCommand())
}

func newDocsCommand() *cobra.Command {
	var recursive, noNotes, excludeHidden bool
	var customerID, language string
	cmd := &cobra.Command{
		Use:   "docs <directory>",
		Short: "Convert HTML, PDF, DOCX and PPTX files in a directory to Markdown",
		Long:  "Convert local documents, with one output subdirectory per source file. Existing destination subdirectories are never overwritten. Use --recursive to include nested directories.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(customerID) {
				return fmt.Errorf("--cid must contain only letters, numbers, underscores or hyphens")
			}
			source, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			source, err = filepath.EvalSymlinks(source)
			if err != nil {
				return err
			}
			info, err := os.Stat(source)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("%q is not a directory", args[0])
			}
			exportDir, err := cmd.Flags().GetString("dir")
			if err != nil {
				return err
			}
			output, err := filepath.Abs(exportDir)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(output, 0755); err != nil {
				return err
			}
			output, err = filepath.EvalSymlinks(output)
			if err != nil {
				return err
			}

			// Snapshot inputs before writing, and do not revisit a nested export tree.
			var inputs []string
			var failures []error
			err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
				if err := cmd.Context().Err(); err != nil {
					return err
				}
				if walkErr != nil {
					failures = append(failures, fmt.Errorf("%s: %w", path, walkErr))
					return nil
				}
				if entry.IsDir() {
					if path != source && (!recursive || path == output) {
						return filepath.SkipDir
					}
					return nil
				}
				if !entry.Type().IsRegular() {
					return nil
				}
				switch strings.ToLower(filepath.Ext(path)) {
				case ".html", ".htm", ".pdf", ".docx", ".pptx":
					inputs = append(inputs, path)
				}
				return nil
			})
			if err != nil {
				return err
			}

			var describer tools.ImageDescriber
			ia, err := cmd.Flags().GetBool("ia")
			if err != nil {
				return err
			}
			if ia {
				describer, err = tools.NewImageDescriber(tools.AIConfigFromEnv())
				if err != nil {
					return err
				}
			}
			var exportedPages []tools.Page
			for _, input := range inputs {
				if err := cmd.Context().Err(); err != nil {
					failures = append(failures, err)
					break
				}
				relative, err := filepath.Rel(source, input)
				if err != nil {
					return err
				}
				destination := filepath.Join(output, relative+".tomd")
				if err = os.MkdirAll(filepath.Dir(destination), 0755); err == nil {
					err = os.Mkdir(destination, 0755)
				}
				var page tools.Page
				if err == nil {
					warn := func(message string) { fmt.Fprintf(cmd.ErrOrStderr(), "%s: Warning: %s\n", relative, message) }
					switch strings.ToLower(filepath.Ext(input)) {
					case ".html", ".htm":
						page, err = tools.GetPage(input, customerID, destination, tools.Metadata{}, "", ia)
					case ".pdf":
						page, err = tools.GetPDFWithOptions(input, input, customerID, destination, tools.Metadata{}, tools.PDFOptions{
							Describer: describer, Language: language, Context: cmd.Context(), Warn: warn,
						})
					case ".docx":
						page, err = docx2md.GetDocx(input, input, customerID, destination, tools.Metadata{})
					case ".pptx":
						page, err = docx2md.GetPptxWithOptions(input, input, customerID, destination, tools.Metadata{}, docx2md.PptxOptions{
							Describer: describer, Language: language, Context: cmd.Context(), Warn: warn,
							ExcludeNotes: noNotes, ExcludeHidden: excludeHidden,
						})
					}
				}
				if err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", relative, err))
					continue
				}
				exportedPages = append(exportedPages, page)
				fmt.Fprintf(cmd.OutOrStdout(), "%s -> %s\n", relative, page.MdFile)
			}
			tools.DisplayPages(cmd.OutOrStdout(), exportedPages)
			fmt.Fprintf(cmd.OutOrStdout(), "Converted: %d; errors: %d\n", len(exportedPages), len(failures))
			return errors.Join(failures...)
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Include subdirectories")
	cmd.Flags().StringVarP(&customerID, "cid", "c", "docs", "Customer ID code")
	cmd.Flags().StringVar(&language, "language", "", "Language for PDF/PPTX AI descriptions (default: source document language)")
	cmd.Flags().BoolVar(&noNotes, "no-notes", false, "Exclude PPTX presenter notes")
	cmd.Flags().BoolVar(&excludeHidden, "exclude-hidden", false, "Exclude hidden PPTX slides")
	return cmd
}

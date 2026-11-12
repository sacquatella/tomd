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
	var s3Options tools.S3Options
	cmd := &cobra.Command{
		Use:   "docs <directory|s3://bucket/prefix>",
		Short: "Convert HTML, PDF, DOCX and PPTX files from a directory or S3 to Markdown",
		Long:  "Convert local or S3 documents, with one local output subdirectory per source file. Existing destinations are never overwritten. Use --recursive to include nested directories or S3 prefixes. S3 uses standard AWS credentials and region configuration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(customerID) {
				return fmt.Errorf("--cid must contain only letters, numbers, underscores or hyphens")
			}
			var source string
			var s3Source *tools.S3Source
			var err error
			if strings.HasPrefix(args[0], "s3:") {
				s3Source, err = tools.NewS3Source(cmd.Context(), args[0], s3Options)
				if err != nil {
					return err
				}
			} else {
				source, err = filepath.Abs(args[0])
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
			var inputs []docsInput
			var failures []error
			if s3Source != nil {
				objects, err := s3Source.List(cmd.Context(), recursive)
				if err != nil {
					return err
				}
				for _, object := range objects {
					if !supportedDocument(object.Key) {
						continue
					}
					relative, err := s3Source.RelativePath(object.Key)
					if err != nil {
						failures = append(failures, fmt.Errorf("%s: %w", object.URL, err))
						continue
					}
					inputs = append(inputs, docsInput{relative: relative, sourceURL: object.URL, key: object.Key})
				}
			} else {
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
					if supportedDocument(path) {
						relative, err := filepath.Rel(source, path)
						if err != nil {
							return err
						}
						inputs = append(inputs, docsInput{filename: path, relative: relative, sourceURL: path})
					}
					return nil
				})
				if err != nil {
					return err
				}
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
				page, err := func() (tools.Page, error) {
					if s3Source != nil {
						staging, err := os.MkdirTemp("", "tomd-s3-")
						if err != nil {
							return tools.Page{}, err
						}
						defer os.RemoveAll(staging)
						input.filename = filepath.Join(staging, filepath.Base(input.relative))
						if err := s3Source.Download(cmd.Context(), input.key, input.filename); err != nil {
							return tools.Page{}, err
						}
					}
					destination, err := createDocsDestination(output, input.relative)
					if err != nil {
						return tools.Page{}, err
					}
					warn := func(message string) { fmt.Fprintf(cmd.ErrOrStderr(), "%s: Warning: %s\n", input.sourceURL, message) }
					switch strings.ToLower(filepath.Ext(input.filename)) {
					case ".html", ".htm":
						return tools.GetPageWithSource(input.filename, input.sourceURL, customerID, destination, tools.Metadata{}, "", ia)
					case ".pdf":
						return tools.GetPDFWithOptions(input.filename, input.sourceURL, customerID, destination, tools.Metadata{}, tools.PDFOptions{
							Describer: describer, Language: language, Context: cmd.Context(), Warn: warn,
						})
					case ".docx":
						return docx2md.GetDocx(input.filename, input.sourceURL, customerID, destination, tools.Metadata{})
					case ".pptx":
						return docx2md.GetPptxWithOptions(input.filename, input.sourceURL, customerID, destination, tools.Metadata{}, docx2md.PptxOptions{
							Describer: describer, Language: language, Context: cmd.Context(), Warn: warn,
							ExcludeNotes: noNotes, ExcludeHidden: excludeHidden,
						})
					}
					return tools.Page{}, fmt.Errorf("unsupported document %q", input.sourceURL)
				}()
				if err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", input.sourceURL, err))
					continue
				}
				exportedPages = append(exportedPages, page)
				fmt.Fprintf(cmd.OutOrStdout(), "%s -> %s\n", input.relative, page.MdFile)
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
	cmd.Flags().StringVar(&s3Options.Endpoint, "s3-endpoint", "", "Custom S3-compatible service endpoint")
	cmd.Flags().StringVar(&s3Options.Region, "s3-region", "", "S3 region (default: standard AWS configuration)")
	cmd.Flags().BoolVar(&s3Options.PathStyle, "s3-path-style", false, "Use path-style S3 addressing (for example, MinIO)")
	return cmd
}

type docsInput struct {
	filename, relative, sourceURL, key string
}

func supportedDocument(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".html", ".htm", ".pdf", ".docx", ".pptx":
		return true
	}
	return false
}

func createDocsDestination(output, relative string) (string, error) {
	if !filepath.IsLocal(relative) {
		return "", fmt.Errorf("unsafe output path %q", relative)
	}
	destination := filepath.Join(output, relative+".tomd")
	current := output
	components := strings.Split(relative+".tomd", string(filepath.Separator))
	for index, component := range components {
		current = filepath.Join(current, component)
		err := os.Mkdir(current, 0755)
		if err == nil {
			continue
		}
		if !errors.Is(err, os.ErrExist) || index == len(components)-1 {
			return "", err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("output ancestor %q is not a regular directory", current)
		}
	}
	return destination, nil
}

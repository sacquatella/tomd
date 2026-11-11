package cmd

import (
	"context"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestImageDescriptionAliases(t *testing.T) {
	original := ImgDesc
	t.Cleanup(func() { ImgDesc = original })
	for _, option := range []string{"--ai", "--ia", "-i"} {
		t.Run(option, func(t *testing.T) {
			ImgDesc = false
			flags := pflag.NewFlagSet("aliases", pflag.ContinueOnError)
			for _, name := range []string{"ai", "ia"} {
				flag := rootCmd.PersistentFlags().Lookup(name)
				if flag == nil {
					t.Fatalf("missing flag --%s", name)
				}
				copy := *flag
				flags.AddFlag(&copy)
			}
			if err := flags.Parse([]string{option}); err != nil {
				t.Fatal(err)
			}
			if !ImgDesc {
				t.Fatalf("%s did not enable image descriptions", option)
			}
		})
	}
}

func TestDocxCommandReturnsErrors(t *testing.T) {
	originalDocx, originalAI := Docx, ImgDesc
	t.Cleanup(func() { Docx, ImgDesc = originalDocx, originalAI })
	Docx, ImgDesc = "missing.docx", false
	command := &cobra.Command{}
	command.SetContext(context.Background())
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err := getDocxDocument(command, nil); err == nil {
		t.Fatal("missing files must return errors rather than terminating the process")
	}
}

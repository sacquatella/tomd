// Copyright © 2024 Stephan Acquatella
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
	//	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
)

var Verbose bool
var ImgDesc bool
var CustomerId string
var ExportDir string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "tomd",
	Short: "Export web pages to markdown files",
	Long:  `Export web pages to markdown files, create metadata header and optionally use llm to describe image in markdown file`,
}

//var log *logrus.Logger

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	//Verbose = false
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := tools.InitLogger(Verbose); err != nil {
			return err
		}
		return nil
	}

	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "write debug logs in log-tomd.log file")
	rootCmd.PersistentFlags().StringVarP(&ExportDir, "dir", "d", ".", "Export page(s) folder, default is current folder")
	rootCmd.PersistentFlags().BoolVarP(&ImgDesc, "ia", "i", false, "Use IA for image description")
	//log = tools.InitLog(Verbose)
}

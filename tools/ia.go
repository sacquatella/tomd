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

package tools

import (
	"context"
	"github.com/ollama/ollama/api"
	"io"
	"net/http"
	"os"
	"strings"
)

// DescribeImg describe an image with Ollama API
func DescribeImg(img string) string {

	var err error
	var imgData []byte
	if strings.HasPrefix(img, "http") {
		resp, err := http.Get(img)
		CheckError(err)
		// get img data
		imgData, err = io.ReadAll(resp.Body)
		defer resp.Body.Close()
	} else {
		imgData, err = os.ReadFile(img)
		CheckError(err)
	}

	//imgData, err := os.ReadFile(img)
	//CheckError(err)

	client, err := api.ClientFromEnvironment()
	CheckError(err)

	req := &api.GenerateRequest{
		Model:  "llava",
		Prompt: "describe this image",
		Images: []api.ImageData{imgData},
	}

	ctx := context.Background()
	/*respFunc := func(resp api.GenerateResponse) error {
		// In streaming mode, responses are partial so we call fmt.Print (and not
		// Println) in order to avoid spurious newlines being introduced. The
		// model will insert its own newlines if it wants.
		fmt.Print(resp.Response)
		return nil
	}*/

	var llmResponse string
	respFunc := func(resp api.GenerateResponse) error {
		// In streaming mode, responses are partial so we call fmt.Print (and not
		// Println) in order to avoid spurious newlines being introduced. The
		// model will insert its own newlines if it wants.
		llmResponse += resp.Response
		return nil
	}

	err = client.Generate(ctx, req, respFunc)
	CheckError(err)

	//fmt.Println()
	return llmResponse
}

func imageDescriptionAsMd(imgList []string, domain string) string {
	// Add image description to markdown
	var markdown string
	for _, img := range imgList {
		if strings.HasSuffix(img, ".svg") || strings.HasSuffix(img, ".svg.png") {
			continue
		}
		if domain != "" {
			img = "https://" + domain + img
		}
		mddesc := DescribeImg(img)
		markdown += "[" + img + "]:" + mddesc + "\n"
	}
	return markdown
}

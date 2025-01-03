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
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"io"
	"net/http"
	"os"
	"strings"
)

// DescribeImg describe an image with Ollama API
func DescribeImg(img string, lang string) (string, error) {

	// override model if TOML_MODEL env variable is set
	viper.SetDefault("Model", "llava:7b")
	viper.SetEnvPrefix("tomd") // will be uppercased automatically
	viper.BindEnv("Model")     // set env value with TOML_MODEL

	var err error
	var imgData []byte
	var prompt string

	switch lang {
	case "French":
		//prompt = "décrire cette image"
		prompt = "Peux-tu décrire l'image suivante qui contient un schéma ? Mentionne les éléments clés, leur disposition et les relations entre eux."
	case "German":
		prompt = "beschreibe dieses Bild"
	case "Italian":
		prompt = "descrivi questa immagine"
	case "English":
		prompt = "describe this image"
	default:
		prompt = "describe this image"
	}

	if strings.HasPrefix(img, "http") {
		resp, err := http.Get(img)
		if err != nil {
			log.Infof("Error %s when getting img %s ", err, img)
			return "", err
		}
		// get img data
		imgData, err = io.ReadAll(resp.Body)
		defer resp.Body.Close()
	} else {
		imgData, err = os.ReadFile(img)
		if err != nil {
			log.Infof("Error %s when reading img %s ", err, img)
			return "", err
		}
	}
	log.Info("Use ollama to describe image : ", img)
	client, err := api.ClientFromEnvironment()
	CheckError(err)

	mymodel := viper.Get("Model")
	log.Info("Use model  : ", mymodel.(string))
	log.Info("Use promt  : ", prompt)

	req := &api.GenerateRequest{
		Model:  mymodel.(string),
		Prompt: prompt,
		Images: []api.ImageData{imgData},
	}

	ctx := context.Background()

	var llmResponse string
	respFunc := func(resp api.GenerateResponse) error {
		// In streaming mode, responses are partial so we call fmt.Print (and not
		// Println) in order to avoid spurious newlines being introduced. The
		// model will insert its own newlines if it wants.
		llmResponse += resp.Response
		return nil
	}

	err = client.Generate(ctx, req, respFunc)
	if err != nil {
		log.Infof("Error %s when when calling llm ", err)
		return "", err
	}

	return llmResponse, nil
}

// imageDescriptionAsMd add image description to markdown
func imageDescriptionAsMd(imgList []string, lang string) string {
	// Add image description to markdown
	var markdown string
	for _, img := range imgList {
		if strings.HasSuffix(img, ".svg") || strings.HasSuffix(img, ".svg.png") {
			continue
		}
		log.Info("compute Image: ", img)
		mdDesc, err := DescribeImg(img, lang)
		if err != nil {
			mdDesc = ""
		}
		markdown += "\n[" + img + "]: " + mdDesc + "\n"
	}
	return markdown
}

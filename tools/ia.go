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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/ollama/ollama/api"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type aiConfig struct {
	Provider       string
	Model          string
	OpenAIAPIKey   string
	OpenAIBaseURL  string
	OpenAIMaxToken int
}

type openAIChatCompletionsRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role    string              `json:"role"`
	Content []openAIContentPart `json:"content"`
}

type openAIContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"`
}

type openAIChatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func loadAIConfig() aiConfig {
	viper.SetEnvPrefix("tomd")
	viper.BindEnv("AIProvider", "TOMD_AI_PROVIDER")
	viper.BindEnv("Model", "TOMD_MODEL")
	viper.BindEnv("OpenAIAPIKey", "TOMD_OPENAI_API_KEY")
	viper.BindEnv("OpenAIBaseURL", "TOMD_OPENAI_BASE_URL")

	viper.SetDefault("AIProvider", "ollama")
	viper.SetDefault("OpenAIBaseURL", "https://api.openai.com/v1")

	provider := strings.ToLower(strings.TrimSpace(viper.GetString("AIProvider")))
	model := resolveModel(provider, strings.TrimSpace(viper.GetString("Model")))

	return aiConfig{
		Provider:       provider,
		Model:          model,
		OpenAIAPIKey:   strings.TrimSpace(viper.GetString("OpenAIAPIKey")),
		OpenAIBaseURL:  strings.TrimRight(strings.TrimSpace(viper.GetString("OpenAIBaseURL")), "/"),
		OpenAIMaxToken: 300,
	}
}

func resolveModel(provider string, model string) string {
	if model != "" {
		return model
	}

	if provider == "openai" {
		return "gpt-4.1-mini"
	}

	return "llava:7b"
}

func describePrompt(lang string) string {
	switch lang {
	case "French":
		return "Peux-tu décrire l'image suivante qui contient un schéma ? Mentionne les éléments clés, leur disposition et les relations entre eux."
	case "German":
		return "beschreibe dieses Bild"
	case "Italian":
		return "descrivi questa immagine"
	case "English":
		return "describe this image"
	default:
		return "describe this image"
	}
}

func readImageBytes(img string) ([]byte, error) {
	if strings.HasPrefix(img, "http://") || strings.HasPrefix(img, "https://") {
		resp, err := http.Get(img)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		return io.ReadAll(resp.Body)
	}

	return os.ReadFile(img)
}

func dataURLForLocalImage(img string) (string, error) {
	imgData, err := os.ReadFile(img)
	if err != nil {
		return "", err
	}

	contentType := http.DetectContentType(imgData)
	if contentType == "application/octet-stream" {
		switch strings.ToLower(filepath.Ext(img)) {
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		case ".svg":
			contentType = "image/svg+xml"
		default:
			contentType = "image/jpeg"
		}
	}

	return fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(imgData)), nil
}

// DescribeImg describe an image with the configured IA provider.
func DescribeImg(img string, lang string) (string, error) {
	cfg := loadAIConfig()

	switch cfg.Provider {
	case "ollama":
		return describeImgWithOllama(img, describePrompt(lang), cfg.Model)
	case "openai":
		return describeImgWithOpenAI(img, describePrompt(lang), cfg)
	default:
		return "", fmt.Errorf("unsupported AI provider %q, expected ollama or openai", cfg.Provider)
	}
}

func describeImgWithOllama(img string, prompt string, model string) (string, error) {
	imgData, err := readImageBytes(img)
	if err != nil {
		log.Infof("Error %s when reading img %s ", err, img)
		return "", err
	}

	log.Info("Use ollama to describe image : ", img)
	client, err := api.ClientFromEnvironment()
	CheckError(err)

	log.Info("Use model  : ", model)
	log.Info("Use promt  : ", prompt)

	req := &api.GenerateRequest{
		Model:  model,
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

func describeImgWithOpenAI(img string, prompt string, cfg aiConfig) (string, error) {
	if cfg.OpenAIAPIKey == "" {
		return "", fmt.Errorf("missing OpenAI API key, set TOMD_OPENAI_API_KEY")
	}

	var imageURL string
	var err error

	if strings.HasPrefix(img, "http://") || strings.HasPrefix(img, "https://") {
		imageURL = img
	} else {
		imageURL, err = dataURLForLocalImage(img)
		if err != nil {
			log.Infof("Error %s when reading img %s ", err, img)
			return "", err
		}
	}

	payload := openAIChatCompletionsRequest{
		Model: cfg.Model,
		Messages: []openAIMessage{
			{
				Role: "user",
				Content: []openAIContentPart{
					{Type: "text", Text: prompt},
					{
						Type:     "image_url",
						ImageURL: &openAIImageURL{URL: imageURL},
					},
				},
			},
		},
		MaxTokens: cfg.OpenAIMaxToken,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	log.Info("Use openai to describe image : ", img)
	log.Info("Use model  : ", cfg.Model)
	log.Info("Use promt  : ", prompt)

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		cfg.OpenAIBaseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+cfg.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var completion openAIChatCompletionsResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return "", err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		if completion.Error != nil && completion.Error.Message != "" {
			return "", fmt.Errorf("openai API error: %s", completion.Error.Message)
		}
		return "", fmt.Errorf("openai API returned status %s", resp.Status)
	}

	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("openai API returned no choices")
	}

	return strings.TrimSpace(completion.Choices[0].Message.Content), nil
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

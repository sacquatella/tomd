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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImageProvidersAndCache(t *testing.T) {
	for _, provider := range []string{"openai", "ollama"} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost {
					t.Errorf("method = %s", r.Method)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["model"] != "vision-test" {
					t.Errorf("model = %v", body["model"])
				}
				if provider == "openai" {
					if r.URL.Path != "/v1/chat/completions" {
						t.Errorf("path = %s", r.URL.Path)
					}
					if r.Header.Get("Authorization") != "Bearer secret" {
						t.Error("missing API key")
					}
					content := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
					prompt := content[0].(map[string]any)["text"].(string)
					if !strings.Contains(prompt, "French") || (!strings.Contains(prompt, "Slide context") && !strings.Contains(prompt, "Different slide")) {
						t.Errorf("missing prompt context: %s", prompt)
					}
					image := content[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
					if !strings.HasPrefix(image, "data:image/png;base64,") {
						t.Errorf("bad image URL: %s", image)
					}
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Portail → Worker"}}]}`))
				} else {
					if r.URL.Path != "/api/generate" {
						t.Errorf("path = %s", r.URL.Path)
					}
					if r.Header.Get("Authorization") != "" {
						t.Error("OpenAI key sent to Ollama")
					}
					if body["stream"] != false || len(body["images"].([]any)) != 1 {
						t.Error("invalid Ollama request")
					}
					_, _ = w.Write([]byte(`{"response":"Portail → Worker"}`))
				}
			}))
			defer server.Close()
			base := server.URL
			if provider == "openai" {
				base += "/v1"
			}
			d, err := NewImageDescriber(AIConfig{Provider: provider, Model: "vision-test", APIKey: "secret", BaseURL: base})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				text, err := d.Describe(context.Background(), []byte("png data"), "image/png", "French", "Slide context")
				if err != nil || text != "Portail → Worker" {
					t.Fatalf("description = %q, err = %v", text, err)
				}
			}
			if calls != 1 {
				t.Errorf("cache failed: %d calls", calls)
			}
			_, err = d.Describe(context.Background(), []byte("png data"), "image/png", "French", "Different slide")
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Error("cache must include source context")
			}
		})
	}
}

func TestImageProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "secret"},
		{"invalid-json", 200, "not json"},
		{"empty", 200, `{"choices":[]}`},
		{"provider-error", 200, `{"error":"secret"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			d, err := NewImageDescriber(AIConfig{Provider: "openai", Model: "vision", APIKey: "secret", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = d.Describe(context.Background(), []byte("data"), "image/jpeg", "", "")
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Error("leaked secret or provider response")
			}
		})
	}
}

func TestImageProviderRateLimitRespectsTimeout(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(429) }))
	defer server.Close()
	d, err := NewImageDescriber(AIConfig{Provider: "ollama", Model: "vision", BaseURL: server.URL, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Describe(context.Background(), []byte("data"), "image/png", "", "")
	if err == nil || calls != 1 {
		t.Fatalf("error = %v, calls = %d", err, calls)
	}
}

func TestImageProviderValidation(t *testing.T) {
	for _, config := range []AIConfig{
		{Provider: "unknown"},
		{Provider: "openai", Model: "vision", BaseURL: "https://api.openai.com/v1"},
		{Provider: "openai", APIKey: "secret", BaseURL: "https://api.openai.com/v1"},
		{Provider: "ollama", Model: "vision", BaseURL: "file:///tmp/model"},
	} {
		if _, err := NewImageDescriber(config); err == nil {
			t.Errorf("accepted invalid config: %+v", config)
		}
	}
	d, err := NewImageDescriber(AIConfig{Provider: "ollama", Model: "vision", BaseURL: "http://localhost:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Describe(context.Background(), []byte("<svg/>"), "image/svg+xml", "", ""); err == nil {
		t.Error("SVG should not be sent to a vision API")
	}
}

func TestAIConfigFromEnv(t *testing.T) {
	t.Setenv("TOMD_AI_PROVIDER", "")
	t.Setenv("TOMD_MODEL", "")
	t.Setenv("OLLAMA_HOST", "localhost:11434")
	c := AIConfigFromEnv()
	if c.Provider != "ollama" || c.Model != "llava:7b" || c.BaseURL != "http://localhost:11434" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	t.Setenv("TOMD_AI_PROVIDER", "openai")
	t.Setenv("TOMD_MODEL", "vision-model")
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("OPENAI_BASE_URL", "https://example.com/v1")
	c = AIConfigFromEnv()
	if c.Provider != "openai" || c.Model != "vision-model" || c.APIKey != "secret" || c.BaseURL != "https://example.com/v1" {
		t.Fatal("OpenAI environment not applied")
	}
}

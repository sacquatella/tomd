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
	"strings"
	"testing"
)

func TestResolveModel(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		want     string
	}{
		{name: "explicit model wins", provider: "openai", model: "gpt-5-mini", want: "gpt-5-mini"},
		{name: "openai default model", provider: "openai", model: "", want: "gpt-5-mini"},
		{name: "ollama default model", provider: "ollama", model: "", want: "llava:7b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveModel(tt.provider, tt.model)
			if got != tt.want {
				t.Fatalf("resolveModel(%q, %q) = %q, want %q", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestDataURLForLocalImage(t *testing.T) {
	got, err := dataURLForLocalImage("../samples/valid_img.jpeg")
	if err != nil {
		t.Fatalf("dataURLForLocalImage returned error: %v", err)
	}

	if !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Fatalf("unexpected data URL prefix: %s", got[:32])
	}
}

// TestDescribeImg_Bacic test the DescribeImg function (ollama shoud run localy to pass this test)
func TestDescribeImg_Bacic(t *testing.T) {
	result, _ := DescribeImg("../samples/valid_img.jpeg", "French")
	println(result)
	if result == "" {
		t.Errorf("expected description, got %s", result)
	}
}

// TestDescribeImg_List test the DescribeImg function (ollama shoud run localy to pass this test)
func TestDescribeImg_List(t *testing.T) {

	imgList := []string{"../samples/valid_img.jpeg", "../samples/valid_img.jpeg"}
	for _, img := range imgList {
		result, _ := DescribeImg(img, "French")
		println(result)
		if result == "" {
			t.Errorf("expected description, got %s", result)
		}
	}
}

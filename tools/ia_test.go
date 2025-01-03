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

import "testing"

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

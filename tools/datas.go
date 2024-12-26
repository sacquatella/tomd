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

// Metadata is a struct that contains the metadata of a document
// @Sample:
// title: MyWeb page
// doc_id: FI-TTH
// description: My super application.
// site_url: https://en.wikipedia.org/wiki/Wikipedia
// creation_date:  2024-04-09T17:52:35
// last_update_date:  2024-04-09T17:52:35
// authors:
// - me
// visibility: Internal
// tags:
// - web
// Date have ISO 8601 format: YYYY-MM-DDTHH:MM:SS

type Metadata struct {
	Title            string   `json:"title"`
	Doc_id           string   `json:"doc_id"`
	Description      string   `json:"description"`
	Site_url         string   `json:"site_url"`
	Authors          []string `json:"authors"`
	Creation_date    string   `json:"creation_date"`
	Last_update_date string   `json:"last_update_date"`
	Visibility       string   `json:"visibility"`
	Tags             []string `json:"tags"`
	PageId           string   `json:"page_id"`
}

type Page struct {
	PageId string
	MdFile string
	Title  string
	Url    string
}

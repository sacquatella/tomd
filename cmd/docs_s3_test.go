package cmd

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type docsS3Request struct {
	path, escapedPath string
	query             url.Values
}

type docsS3Server struct {
	url        string
	prefix     string
	recursive  bool
	pages      [][]string
	objects    map[string][]byte
	statuses   map[string]int
	listError  int // One-based page number; zero means no failure.
	onDownload func(http.ResponseWriter, *http.Request, string) bool
	mu         sync.Mutex
	requests   []docsS3Request
}

func newDocsS3Server(t *testing.T, prefix string, recursive bool, pages [][]string, objects map[string][]byte) *docsS3Server {
	t.Helper()
	configDir, staging := t.TempDir(), t.TempDir()
	for _, name := range []string{"config", "credentials"} {
		writeDocsFile(t, filepath.Join(configDir, name), nil)
	}
	// Never consult the developer's profiles, credential processes, or metadata services.
	for key, value := range map[string]string{
		"HOME": configDir, "TMPDIR": staging,
		"AWS_CONFIG_FILE":             filepath.Join(configDir, "config"),
		"AWS_SHARED_CREDENTIALS_FILE": filepath.Join(configDir, "credentials"),
		"AWS_ACCESS_KEY_ID":           "tomd-test-access", "AWS_SECRET_ACCESS_KEY": "tomd-test-secret",
		"AWS_SESSION_TOKEN": "tomd-test-session", "AWS_SECURITY_TOKEN": "",
		"AWS_PROFILE": "", "AWS_DEFAULT_PROFILE": "", "AWS_REGION": "", "AWS_DEFAULT_REGION": "",
		"AWS_ROLE_ARN": "", "AWS_ROLE_SESSION_NAME": "", "AWS_WEB_IDENTITY_TOKEN_FILE": "",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI": "", "AWS_CONTAINER_CREDENTIALS_FULL_URI": "",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN": "", "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE": "",
		"AWS_EC2_METADATA_DISABLED": "true", "AWS_ENDPOINT_URL": "", "AWS_ENDPOINT_URL_S3": "",
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "true", "AWS_CA_BUNDLE": "",
		"AWS_USE_FIPS_ENDPOINT": "false", "AWS_USE_DUALSTACK_ENDPOINT": "false",
		"AWS_MAX_ATTEMPTS": "3", "AWS_RETRY_MODE": "standard",
		"HTTP_PROXY": "", "HTTPS_PROXY": "", "ALL_PROXY": "", "NO_PROXY": "*",
	} {
		t.Setenv(key, value)
	}
	t.Cleanup(func() { assertDocsS3Empty(t, staging) })
	fake := &docsS3Server{prefix: prefix, recursive: recursive, pages: pages, objects: objects, statuses: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") || !strings.Contains(auth, "/us-east-1/s3/aws4_request") ||
			!strings.Contains(auth, "Signature=") || r.Header.Get("X-Amz-Date") == "" || r.Header.Get("X-Amz-Security-Token") != "tomd-test-session" {
			t.Error("request lacks expected AWS SigV4 signing and session credentials")
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected HTTP method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		query := r.URL.Query()
		fake.mu.Lock()
		fake.requests = append(fake.requests, docsS3Request{r.URL.Path, r.URL.EscapedPath(), query})
		fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		if query.Get("list-type") == "2" {
			_, delimiterPresent := query["delimiter"]
			if r.URL.Path != "/bucket" || query.Get("prefix") != fake.prefix ||
				query.Get("encoding-type") != "url" ||
				(fake.recursive && delimiterPresent) || (!fake.recursive && query.Get("delimiter") != "/") {
				t.Errorf("unexpected listing path/query: %s", r.URL)
			}
			page := 0
			if token := query.Get("continuation-token"); token != "" {
				page = -1
				for i := 1; i < len(fake.pages); i++ {
					if token == fmt.Sprintf("page %d +%%&#", i) {
						page = i
						break
					}
				}
			}
			if page < 0 || page >= len(fake.pages) {
				t.Error("unexpected continuation token or extra listing request")
				http.Error(w, "unexpected page", http.StatusBadRequest)
				return
			}
			if page+1 == fake.listError {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>Listing denied</Message></Error>`)
				return
			}
			var body bytes.Buffer
			fmt.Fprintf(&body, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><EncodingType>url</EncodingType><IsTruncated>%t</IsTruncated>`, page+1 < len(fake.pages))
			if page+1 < len(fake.pages) {
				body.WriteString("<NextContinuationToken>")
				if err := xml.EscapeText(&body, []byte(fmt.Sprintf("page %d +%%&#", page+1))); err != nil {
					t.Error(err)
				}
				body.WriteString("</NextContinuationToken>")
			}
			for _, key := range fake.pages[page] {
				body.WriteString("<Contents><Key>")
				if err := xml.EscapeText(&body, []byte(url.PathEscape(key))); err != nil {
					t.Error(err)
				}
				body.WriteString("</Key></Contents>")
			}
			body.WriteString("</ListBucketResult>")
			_, _ = w.Write(body.Bytes())
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/bucket/") {
			t.Errorf("unexpected download path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/bucket/")
		if fake.onDownload != nil && fake.onDownload(w, r, key) {
			return
		}
		if status := fake.statuses[key]; status != 0 {
			w.WriteHeader(status)
			code := "AccessDenied"
			if status == http.StatusNotFound {
				code = "NoSuchKey"
			}
			fmt.Fprintf(w, "<Error><Code>%s</Code><Message>Download denied</Message></Error>", code)
			return
		}
		data, ok := fake.objects[key]
		if !ok {
			t.Errorf("unexpected download of key %q", key)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	}))
	fake.url = server.URL
	t.Cleanup(server.Close)
	return fake
}

func (s *docsS3Server) args(source string, flags ...string) []string {
	return append([]string{source, "--s3-endpoint", s.url, "--s3-path-style", "--s3-region", "us-east-1"}, flags...)
}

func (s *docsS3Server) assertRequests(t *testing.T, lists int, downloads ...string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	gotLists, gotDownloads := 0, map[string]int{}
	for _, request := range s.requests {
		if request.query.Get("list-type") == "2" {
			wantToken := ""
			if gotLists > 0 {
				wantToken = fmt.Sprintf("page %d +%%&#", gotLists)
			}
			if request.query.Get("continuation-token") != wantToken {
				t.Error("listing did not follow continuation tokens in order")
			}
			gotLists++
		} else {
			if gotLists != lists {
				t.Error("download started before the listing snapshot completed")
			}
			gotDownloads[strings.TrimPrefix(request.path, "/bucket/")]++
		}
	}
	wantDownloads := map[string]int{}
	for _, key := range downloads {
		wantDownloads[key]++
	}
	if gotLists != lists || !reflect.DeepEqual(gotDownloads, wantDownloads) {
		t.Errorf("requests: lists=%d downloads=%v; want lists=%d downloads=%v", gotLists, gotDownloads, lists, wantDownloads)
	}
}

func assertDocsS3Empty(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Errorf("expected empty directory %s, got %v: %v", directory, entries, err)
	}
}

func TestDocsS3MixedFormatsPaginationAndIsolatedOutputs(t *testing.T) {
	fixtures, output := t.TempDir(), t.TempDir()
	markers := map[string]string{
		"Shared title.HTML": "HTML unique content", "Shared title.HTM": "HTM unique content",
		"Other name.html": "Other HTML unique content", "Shared title.PDF": "<!-- Page 1 -->",
		"nested/Shared title.DOCX": "DOCX unique content", "nested/Shared title.PPTX": "Visible slide content",
		"nested/deeper/Shared title.HTML": "Deep HTML unique content",
	}
	objects := map[string][]byte{}
	for name, marker := range markers {
		if strings.EqualFold(filepath.Ext(name), ".html") || strings.EqualFold(filepath.Ext(name), ".htm") {
			objects["reports/"+name] = docsHTML(marker)
		}
	}
	pdf, err := os.ReadFile(filepath.Join("..", "samples", "test.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	objects["reports/Shared title.PDF"] = pdf
	writeDocsZIP(t, filepath.Join(fixtures, "input.docx"), map[string]string{
		"word/document.xml": `<document><body><p><r><t>DOCX unique content</t></r></p></body></document>`,
		"docProps/core.xml": `<coreProperties><title>Shared title</title></coreProperties>`,
	})
	writeDocsZIP(t, filepath.Join(fixtures, "input.pptx"), docsPPTXParts())
	for _, ext := range []string{"docx", "pptx"} {
		data, err := os.ReadFile(filepath.Join(fixtures, "input."+ext))
		if err != nil {
			t.Fatal(err)
		}
		objects["reports/nested/Shared title."+strings.ToUpper(ext)] = data
	}
	pages := [][]string{{"reports/Shared title.PDF", "reports/nested/Shared title.PPTX", "reports/Shared title.HTML"},
		{"reports/Other name.html", "reports/nested/deeper/Shared title.HTML", "reports/Shared title.HTM", "reports/nested/Shared title.DOCX"}}
	fake := newDocsS3Server(t, "reports/", true, pages, objects)
	stdout, stderr, err := executeDocs(t, context.Background(), output, fake.args("s3://bucket/reports", "-r", "--cid", "client_1-test")...)
	if err != nil {
		t.Fatalf("conversion failed: %v; stderr: %s", err, stderr)
	}
	assertDocsSummary(t, stdout, len(markers), 0)
	var downloads []string
	for name, marker := range markers {
		downloads = append(downloads, "reports/"+name)
		destination := filepath.Join(output, filepath.FromSlash(name)+".tomd")
		markdown := readDocsMarkdown(t, destination)
		sourceURL := "s3://bucket/reports/" + strings.ReplaceAll(name, " ", "%20")
		for _, want := range []string{marker, "title: Shared title\n", "doc_id: CLIENT_1-TEST_SHAREDTITLE\n", "site_url: " + sourceURL + "\n"} {
			if !strings.Contains(markdown, want) {
				t.Errorf("%s: missing %q in Markdown", name, want)
			}
		}
		mdPath := filepath.Join(destination, "client_1-test-shared-title.md")
		if !strings.Contains(stdout, filepath.FromSlash(name)+" -> "+mdPath+"\n") {
			t.Errorf("missing output mapping for %s", name)
		}
		found := false
		for _, line := range strings.Split(stdout, "\n") {
			if strings.HasPrefix(line, "|") && strings.Contains(line, mdPath) && strings.Contains(line, sourceURL) && strings.Contains(line, "CLIENT_1-TEST_SHAREDTITLE") {
				found = true
			}
		}
		if !found {
			t.Errorf("missing table row with original S3 provenance for %s", name)
		}
	}
	fake.assertRequests(t, 2, downloads...)
}

func TestDocsS3RecursionAndSkippedKeys(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		t.Run(fmt.Sprintf("recursive=%t", recursive), func(t *testing.T) {
			output := t.TempDir()
			keys := []string{"reports/root.html", "reports/unsupported.txt", "reports/xml-invalid\x01.txt", "reports/no-extension", "reports/folder.html/", "reports/", ""}
			objects := map[string][]byte{"reports/root.html": docsHTML("Root content"), "reports/nested/child.htm": docsHTML("Nested content")}
			downloads := []string{"reports/root.html"}
			if recursive {
				keys = append(keys, "reports/nested/child.htm")
				downloads = append(downloads, "reports/nested/child.htm")
			}
			fake := newDocsS3Server(t, "reports/", recursive, [][]string{keys}, objects)
			args := fake.args("s3://bucket/reports/")
			if recursive {
				args = append(args, "-r")
			}
			stdout, stderr, err := executeDocs(t, context.Background(), output, args...)
			if err != nil {
				t.Fatalf("%v; stderr: %s", err, stderr)
			}
			assertDocsSummary(t, stdout, len(downloads), 0)
			readDocsMarkdown(t, filepath.Join(output, "root.html.tomd"))
			if recursive {
				if md := readDocsMarkdown(t, filepath.Join(output, "nested", "child.htm.tomd")); !strings.Contains(md, "Nested content") {
					t.Error("nested content missing")
				}
			} else {
				assertDocsAbsent(t, filepath.Join(output, "nested"))
			}
			for _, name := range []string{"unsupported.txt.tomd", "no-extension.tomd", "folder.html"} {
				assertDocsAbsent(t, filepath.Join(output, name))
			}
			fake.assertRequests(t, 1, downloads...)
		})
	}
}

func TestDocsS3EscapesKeysOnce(t *testing.T) {
	output := t.TempDir()
	const key = "reports/a b%20#&.HTML"
	const sourceURL = "s3://bucket/reports/a%20b%2520%23&.HTML"
	fake := newDocsS3Server(t, "reports/", false, [][]string{{key}}, map[string][]byte{key: docsHTML("Escaped key content")})
	stdout, _, err := executeDocs(t, context.Background(), output, fake.args("s3://bucket/reports")...)
	if err != nil {
		t.Fatal(err)
	}
	assertDocsSummary(t, stdout, 1, 0)
	markdown := readDocsMarkdown(t, filepath.Join(output, "a b%20#&.HTML.tomd"))
	if !strings.Contains(markdown, "site_url: "+sourceURL+"\n") || !strings.Contains(markdown, "Escaped key content") || !strings.Contains(stdout, sourceURL) {
		t.Error("original escaped S3 URL/content missing from Markdown or table")
	}
	fake.assertRequests(t, 1, key)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if got := fake.requests[1].escapedPath; got != "/bucket/reports/a%20b%2520%23%26.HTML" {
		t.Errorf("download path escaped incorrectly: %s", got)
	}
}

func TestDocsS3FailuresContinue(t *testing.T) {
	sandbox := t.TempDir()
	output := filepath.Join(sandbox, "exports")
	unsafe := []string{"reports/../escape.html", "reports//absolute.html", "/absolute.html", `reports/back\slash.html`, "reports/control\nkey.html", "reports/xml-invalid\x01.html", "other/outside.html"}
	keys := append([]string{"reports/a-missing.html", "reports/b-forbidden.html", "reports/c-invalid.docx"}, unsafe...)
	keys = append(keys, "reports/z-good.html")
	fake := newDocsS3Server(t, "reports/", true, [][]string{keys}, map[string][]byte{
		"reports/c-invalid.docx": []byte("not a ZIP document"), "reports/z-good.html": docsHTML("Survived all failures"),
	})
	fake.statuses["reports/a-missing.html"] = http.StatusNotFound
	fake.statuses["reports/b-forbidden.html"] = http.StatusForbidden
	stdout, _, err := executeDocs(t, context.Background(), output, fake.args("s3://bucket/reports", "-r")...)
	if err == nil {
		t.Fatal("expected joined per-object failures")
	}
	for _, want := range []string{"a-missing.html", "b-forbidden.html", "c-invalid.docx", "404", "403", "not a safe local path", "outside the source prefix"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing error %q: %v", want, err)
		}
	}
	for _, key := range unsafe {
		sourceURL := (&url.URL{Scheme: "s3", Host: "bucket", Path: "/" + key}).String()
		if !strings.Contains(err.Error(), sourceURL) {
			t.Errorf("error does not identify unsafe object %q", key)
		}
	}
	assertDocsSummary(t, stdout, 1, 3+len(unsafe))
	if md := readDocsMarkdown(t, filepath.Join(output, "z-good.html.tomd")); !strings.Contains(md, "Survived all failures") {
		t.Error("later valid object was not converted")
	}
	for _, name := range []string{"a-missing.html.tomd", "b-forbidden.html.tomd", "absolute.html.tomd", "escape.html.tomd"} {
		assertDocsAbsent(t, filepath.Join(output, name))
	}
	files, globErr := filepath.Glob(filepath.Join(output, "c-invalid.docx.tomd", "*.md"))
	if globErr != nil || len(files) != 0 {
		t.Errorf("invalid DOCX produced Markdown: %v, %v", files, globErr)
	}
	entries, readErr := os.ReadDir(sandbox)
	if readErr != nil || len(entries) != 1 || entries[0].Name() != "exports" {
		t.Errorf("unsafe key wrote outside the export root: %v, %v", entries, readErr)
	}
	// Counts also prove that permanent HTTP errors were not retried and unsafe keys were never fetched.
	fake.assertRequests(t, 1, "reports/a-missing.html", "reports/b-forbidden.html", "reports/c-invalid.docx", "reports/z-good.html")
}

func TestDocsS3ProtectsExistingDestinationsAndSymlinks(t *testing.T) {
	output, outside := t.TempDir(), t.TempDir()
	original := []byte("Existing Markdown must not change\n")
	existing := filepath.Join(output, "a-existing.html.tomd", "docs-shared-title.md")
	writeDocsFile(t, existing, original)
	for _, name := range []string{"b-ancestor", "c-leaf.html.tomd"} {
		if err := os.Symlink(outside, filepath.Join(output, name)); err != nil {
			t.Fatal(err)
		}
	}
	keys := []string{"reports/a-existing.html", "reports/b-ancestor/deeper/blocked.html", "reports/c-leaf.html", "reports/z-good.html"}
	objects := map[string][]byte{}
	for _, key := range keys {
		objects[key] = docsHTML("Safe conversion content")
	}
	fake := newDocsS3Server(t, "reports/", true, [][]string{keys}, objects)
	stdout, _, err := executeDocs(t, context.Background(), output, fake.args("s3://bucket/reports", "-r")...)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected destination collision errors, got %v", err)
	}
	for _, key := range keys[:3] {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("missing protected destination error for %s", key)
		}
	}
	assertDocsSummary(t, stdout, 1, 3)
	data, readErr := os.ReadFile(existing)
	if readErr != nil || !bytes.Equal(data, original) {
		t.Errorf("existing output changed: %q, %v", data, readErr)
	}
	entries, readErr := os.ReadDir(filepath.Dir(existing))
	if readErr != nil || len(entries) != 1 {
		t.Errorf("existing destination contents changed: %v, %v", entries, readErr)
	}
	for _, name := range []string{"b-ancestor", "c-leaf.html.tomd"} {
		target, linkErr := os.Readlink(filepath.Join(output, name))
		if linkErr != nil || target != outside {
			t.Errorf("protected symlink changed: %s, %v", target, linkErr)
		}
	}
	assertDocsS3Empty(t, outside)
	readDocsMarkdown(t, filepath.Join(output, "z-good.html.tomd"))
	fake.assertRequests(t, 1, keys...)
}

func TestDocsS3ListingFailureExportsNothing(t *testing.T) {
	for _, failedPage := range []int{1, 2} {
		t.Run(fmt.Sprintf("page=%d", failedPage), func(t *testing.T) {
			output := t.TempDir()
			fake := newDocsS3Server(t, "reports/", false, [][]string{{"reports/good.html"}, {"reports/later.html"}}, nil)
			fake.listError = failedPage
			stdout, _, err := executeDocs(t, context.Background(), output, fake.args("s3://bucket/reports")...)
			if err == nil || !strings.Contains(err.Error(), "list S3 objects") || !strings.Contains(err.Error(), "403") {
				t.Fatalf("expected listing failure, got %v", err)
			}
			if stdout != "" {
				t.Errorf("failed listing exported partial results: %s", stdout)
			}
			assertDocsS3Empty(t, output)
			fake.assertRequests(t, failedPage)
		})
	}
}

func TestDocsS3EmptySource(t *testing.T) {
	output := t.TempDir()
	fake := newDocsS3Server(t, "", false, [][]string{{}}, nil)
	stdout, stderr, err := executeDocs(t, context.Background(), output, fake.args("s3://bucket")...)
	if err != nil || stderr != "" {
		t.Fatalf("empty source failed: %v; stderr: %s", err, stderr)
	}
	assertDocsSummary(t, stdout, 0, 0)
	assertDocsS3Empty(t, output)
	fake.assertRequests(t, 1)
}

func TestDocsS3InterruptedDownloadCleansStagingAndContinues(t *testing.T) {
	output := t.TempDir()
	fake := newDocsS3Server(t, "reports/", false, [][]string{{"reports/a-truncated.html", "reports/z-good.html"}}, map[string][]byte{
		"reports/z-good.html": docsHTML("Survived interrupted download"),
	})
	fake.onDownload = func(w http.ResponseWriter, r *http.Request, key string) bool {
		if key != "reports/a-truncated.html" {
			return false
		}
		w.Header().Set("Content-Length", "1000000")
		_, _ = io.WriteString(w, "partial HTML")
		return true
	}
	stdout, _, err := executeDocs(t, context.Background(), output, fake.args("s3://bucket/reports")...)
	if err == nil || !strings.Contains(err.Error(), "a-truncated.html") || !strings.Contains(err.Error(), "download S3 object") {
		t.Fatalf("expected interrupted download error, got %v", err)
	}
	assertDocsSummary(t, stdout, 1, 1)
	assertDocsAbsent(t, filepath.Join(output, "a-truncated.html.tomd"))
	if md := readDocsMarkdown(t, filepath.Join(output, "z-good.html.tomd")); !strings.Contains(md, "Survived interrupted download") {
		t.Error("later valid object was not converted")
	}
	fake.assertRequests(t, 1, "reports/a-truncated.html", "reports/z-good.html")
}

func TestDocsS3DownloadCancellationCleansStaging(t *testing.T) {
	output := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fake := newDocsS3Server(t, "reports/", false, [][]string{{"reports/a-cancel.html", "reports/z-unreached.html"}}, nil)
	fake.onDownload = func(w http.ResponseWriter, r *http.Request, key string) bool {
		if key != "reports/a-cancel.html" {
			t.Errorf("unexpected download after cancellation: %s", key)
			http.NotFound(w, r)
			return true
		}
		w.Header().Set("Content-Length", "1000000")
		_, _ = io.WriteString(w, "partial HTML")
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
		return true
	}
	stdout, _, err := executeDocs(t, ctx, output, fake.args("s3://bucket/reports")...)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	// The interrupted download and the loop's cancellation check are both reported.
	assertDocsSummary(t, stdout, 0, 2)
	assertDocsS3Empty(t, output)
	fake.assertRequests(t, 1, "reports/a-cancel.html")
}

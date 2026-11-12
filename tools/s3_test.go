package tools

import (
	"context"
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
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type mockS3Client struct {
	list func(context.Context, *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error)
	get  func(context.Context, *s3.GetObjectInput) (*s3.GetObjectOutput, error)
}

func (m mockS3Client) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return m.list(ctx, in)
}

func (m mockS3Client) GetObject(ctx context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return m.get(ctx, in)
}

func s3TestConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for key, value := range map[string]string{
		"AWS_CONFIG_FILE": filepath.Join(dir, "config"), "AWS_SHARED_CREDENTIALS_FILE": filepath.Join(dir, "credentials"),
		"AWS_PROFILE": "", "AWS_DEFAULT_PROFILE": "", "AWS_REGION": "", "AWS_DEFAULT_REGION": "",
		"AWS_ACCESS_KEY_ID": "test", "AWS_SECRET_ACCESS_KEY": "test", "AWS_SESSION_TOKEN": "",
		"AWS_EC2_METADATA_DISABLED": "true",
	} {
		t.Setenv(key, value)
	}
}

func TestS3SourceParsing(t *testing.T) {
	s3TestConfig(t)
	for _, tt := range []struct{ source, prefix string }{
		{"s3://bucket", ""}, {"s3://bucket/", ""},
		{"s3://bucket/reports", "reports/"}, {"s3://bucket/reports/", "reports/"},
		{"s3://bucket/a%20b/%252F/%23/%3F/%2B", "a b/%2F/#/?/+/"},
	} {
		t.Run(tt.source, func(t *testing.T) {
			source, err := NewS3Source(context.Background(), tt.source, S3Options{Region: "us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			if source.bucket != "bucket" || source.prefix != tt.prefix {
				t.Fatalf("got bucket %q, prefix %q", source.bucket, source.prefix)
			}
		})
	}
	for _, source := range []string{
		"", "https://bucket/reports", "s3:bucket", "s3:///reports", "s3://", "S3://bucket",
		"s3://user:secret@bucket/reports", "s3://bucket:443/reports", "s3://bucket/reports?x=y",
		"s3://bucket?", "s3://bucket/#", "s3://bucket/#fragment", "s3://bucket/%zz",
		"s3://bucket/a\nb", "s3://bucket/a\tb", "s3://bucket/%0a", "s3://bucket/%0D", "s3://bucket/%00", "s3://bucket/%C2%85",
	} {
		t.Run(source, func(t *testing.T) {
			if _, err := NewS3Source(context.Background(), source, S3Options{Region: "us-east-1"}); err == nil {
				t.Fatal("expected invalid source error")
			} else if strings.Contains(err.Error(), "secret") {
				t.Fatal("error exposed credentials")
			}
		})
	}
	for _, endpoint := range []string{
		"localhost:9000", "ftp://localhost", "http://", "https://user:secret@localhost",
		"https://localhost?x=y", "https://localhost?", "https://localhost#", "https://localhost/#fragment", "https://localhost/\n",
	} {
		if _, err := NewS3Source(context.Background(), "s3://bucket", S3Options{Region: "us-east-1", Endpoint: endpoint}); err == nil {
			t.Errorf("accepted endpoint %q", endpoint)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatal("error exposed endpoint credentials")
		}
	}
	if _, err := NewS3Source(context.Background(), "s3://bucket", S3Options{}); err == nil || !strings.Contains(err.Error(), "--s3-region") {
		t.Fatalf("expected actionable missing region error, got %v", err)
	}
	t.Setenv("AWS_REGION", "eu-west-1")
	source, err := NewS3Source(context.Background(), "s3://bucket", S3Options{})
	if err != nil || source.client.(*s3.Client).Options().Region != "eu-west-1" {
		t.Fatalf("environment region not loaded: %v", err)
	}
}

func TestS3SDKEndpoint(t *testing.T) {
	s3TestConfig(t)
	const key = "reports/a b+%?#.txt"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "/eu-central-1/s3/aws4_request") {
			t.Errorf("unexpected signing region: %s", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("list-type") == "2" {
			if r.URL.Path != "/bucket" || r.URL.Query().Get("prefix") != "reports/" || r.URL.Query().Get("delimiter") != "/" {
				t.Errorf("unexpected list request: %s", r.URL)
			}
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<ListBucketResult><Contents><Key>reports/a b+%?#.txt</Key><Size>4</Size></Contents></ListBucketResult>`)
			return
		}
		if r.URL.Path != "/bucket/"+key {
			t.Errorf("key was not preserved: %s", r.URL)
		}
		w.Header().Set("Content-Length", "4")
		fmt.Fprint(w, "data")
	}))
	defer server.Close()
	t.Setenv("AWS_REGION", "us-west-2")
	source, err := NewS3Source(context.Background(), "s3://bucket/reports", S3Options{Endpoint: server.URL, Region: "eu-central-1", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	options := source.client.(*s3.Client).Options()
	if !options.UsePathStyle || aws.ToString(options.BaseEndpoint) != server.URL || options.Region != "eu-central-1" {
		t.Fatalf("unexpected SDK options: region=%s endpoint=%v pathStyle=%v", options.Region, options.BaseEndpoint, options.UsePathStyle)
	}
	objects, err := source.List(context.Background(), false)
	if err != nil || len(objects) != 1 || objects[0].Key != key {
		t.Fatalf("list: %v, %v", objects, err)
	}
	parsed, err := url.Parse(objects[0].URL)
	if err != nil || parsed.Path != "/"+key || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatalf("bad escaped object URL: %q, %v", objects[0].URL, err)
	}
	filename := filepath.Join(t.TempDir(), "object")
	if err := source.Download(context.Background(), key, filename); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != "data" {
		t.Fatalf("download: %q, %v", data, err)
	}
}

func TestS3List(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("recursive=%v/fail=%v", recursive, fail), func(t *testing.T) {
				calls := 0
				wantErr := errors.New("listing failed")
				source := &S3Source{bucket: "bucket", prefix: "reports/", client: mockS3Client{
					list: func(ctx context.Context, in *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
						calls++
						if aws.ToString(in.Bucket) != "bucket" || aws.ToString(in.Prefix) != "reports/" ||
							(recursive && in.Delimiter != nil) || (!recursive && aws.ToString(in.Delimiter) != "/") {
							t.Fatalf("unexpected list input: %+v", in)
						}
						if calls == 1 {
							return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(true), NextContinuationToken: aws.String("next"),
								Contents:       []types.Object{{Key: aws.String("reports/z%20.txt")}, {Key: aws.String("reports/folder/")}, {}},
								CommonPrefixes: []types.CommonPrefix{{Prefix: aws.String("reports/folder/")}}}, nil
						}
						if calls != 2 || aws.ToString(in.ContinuationToken) != "next" {
							t.Fatalf("unexpected pagination: %+v", in)
						}
						if fail {
							return nil, wantErr
						}
						return &s3.ListObjectsV2Output{Contents: []types.Object{
							{Key: aws.String("reports/a b.txt")}, {Key: aws.String("reports/../unsafe.txt")},
						}}, nil
					},
				}}
				objects, err := source.List(context.Background(), recursive)
				if fail {
					if !errors.Is(err, wantErr) || objects != nil {
						t.Fatalf("expected no partial results: %v, %v", objects, err)
					}
					return
				}
				want := []S3Object{
					{Key: "reports/../unsafe.txt", URL: "s3://bucket/reports/../unsafe.txt"},
					{Key: "reports/a b.txt", URL: "s3://bucket/reports/a%20b.txt"},
					{Key: "reports/z%20.txt", URL: "s3://bucket/reports/z%2520.txt"},
				}
				if err != nil || !reflect.DeepEqual(objects, want) {
					t.Fatalf("got %+v, %v; want %+v", objects, err, want)
				}
			})
		}
	}
}

func TestS3RelativePath(t *testing.T) {
	source := &S3Source{prefix: "reports/"}
	for _, rel := range []string{"file.txt", "sub/file.txt", "a b.txt", "%2e%2e/file.txt", "COM10.txt", "auxiliary.txt", "caf\u00e9.txt"} {
		got, err := source.RelativePath("reports/" + rel)
		if err != nil || got != filepath.FromSlash(rel) {
			t.Errorf("RelativePath(%q) = %q, %v", rel, got, err)
		}
	}
	for _, key := range []string{
		"reports2/file.txt", "other/file.txt", "reports/", "reports//absolute", "reports/./file", "reports/../file",
		"reports/a//b", "reports/a/../b", "reports/a/./b", "reports/a/", "reports/a\\b", "reports/C:/file",
		"reports/file\n.txt", "reports/file\x00.txt", "reports/file\u0085.txt", "reports/file\x7f.txt",
		"reports/CON", "reports/con.txt", "reports/AUX/file", "reports/NUL", "reports/prn.txt",
		"reports/COM1.txt", "reports/lpt9", "reports/LPT\u00b9.txt", "reports/COM\u00b2", "reports/CONIN$", "reports/CONOUT$.txt",
		"reports/CON .txt", "reports/file.", "reports/file ", "reports/a./file", "reports/a /file", "reports/file?.txt",
		"reports/a*b", "reports/a|b", "reports/a<b", "reports/a>b", "reports/a\"b",
	} {
		if got, err := source.RelativePath(key); err == nil {
			t.Errorf("accepted unsafe key %q as %q", key, got)
		}
	}
	source.prefix = ""
	if _, err := source.RelativePath("/absolute"); err == nil {
		t.Fatal("accepted absolute key")
	}
}

func TestS3ListEncodedKeys(t *testing.T) {
	for _, key := range []string{"reports/a%20b+%2520%23.html", "reports/%01.html", "reports/%zz.html"} {
		t.Run(key, func(t *testing.T) {
			source := &S3Source{bucket: "bucket", client: mockS3Client{
				list: func(_ context.Context, input *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
					if input.EncodingType != types.EncodingTypeUrl {
						t.Fatal("listing must request encoded keys to support XML-invalid characters")
					}
					return &s3.ListObjectsV2Output{EncodingType: types.EncodingTypeUrl, Contents: []types.Object{{Key: aws.String(key)}}}, nil
				},
			}}
			objects, err := source.List(context.Background(), true)
			want, decodeErr := url.PathUnescape(key)
			if decodeErr != nil {
				if err == nil || len(objects) != 0 {
					t.Fatalf("expected malformed encoding to fail without partial results: %v, %v", objects, err)
				}
				return
			}
			if err != nil || len(objects) != 1 || objects[0].Key != want {
				t.Fatalf("expected key decoded once with literal '+' preserved: %v, %v", objects, err)
			}
		})
	}
}

type s3TestBody struct {
	io.Reader
	closeErr error
	closed   bool
}

func (b *s3TestBody) Close() error {
	b.closed = true
	return b.closeErr
}

type s3ErrorReader struct{ err error }

func (r s3ErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestS3Download(t *testing.T) {
	wantErr := errors.New("stream failed")
	for _, tt := range []struct {
		name     string
		length   *int64
		readErr  error
		closeErr error
		getErr   error
		canceled bool
		badPath  bool
		wantErr  bool
	}{
		{name: "success", length: aws.Int64(4)},
		{name: "unknown size"},
		{name: "truncated", length: aws.Int64(5), wantErr: true},
		{name: "too large", length: aws.Int64(3), wantErr: true},
		{name: "read error", readErr: wantErr, wantErr: true},
		{name: "close error", closeErr: wantErr, wantErr: true},
		{name: "get error", getErr: wantErr, wantErr: true},
		{name: "canceled", canceled: true, wantErr: true},
		{name: "create error", badPath: true, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &s3TestBody{Reader: strings.NewReader("data"), closeErr: tt.closeErr}
			if tt.readErr != nil {
				body.Reader = io.MultiReader(strings.NewReader("da"), s3ErrorReader{tt.readErr})
			}
			source := &S3Source{bucket: "bucket", client: mockS3Client{
				get: func(ctx context.Context, in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
					if aws.ToString(in.Bucket) != "bucket" || aws.ToString(in.Key) != "key" {
						t.Fatalf("unexpected get input: %+v", in)
					}
					if tt.getErr != nil {
						return nil, tt.getErr
					}
					return &s3.GetObjectOutput{Body: body, ContentLength: tt.length}, nil
				},
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.canceled {
				cancel()
			}
			filename := filepath.Join(t.TempDir(), "download")
			if tt.badPath {
				filename = filepath.Join(filename, "missing")
			}
			err := source.Download(ctx, "key", filename)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.getErr == nil && !body.closed {
				t.Fatal("body was not closed")
			}
			if tt.wantErr {
				if _, statErr := os.Stat(filename); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("partial file remains: %v", statErr)
				}
			} else if data, err := os.ReadFile(filename); err != nil || string(data) != "data" {
				t.Fatalf("unexpected download: %q, %v", data, err)
			}
		})
	}
}

func TestS3DownloadCancellationDuringRead(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	source := &S3Source{client: mockS3Client{
		get: func(context.Context, *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{Body: reader}, nil
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	filename := filepath.Join(t.TempDir(), "download")
	done := make(chan error, 1)
	go func() { done <- source.Download(ctx, "key", filename) }()
	// A completed write proves Download has entered its streaming copy.
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("download did not stop after cancellation")
	}
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file remains: %v", err)
	}
}

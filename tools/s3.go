package tools

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Options struct {
	Endpoint, Region string
	PathStyle        bool
}

type S3Object struct {
	Key, URL string
}

type s3Client interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type S3Source struct {
	client         s3Client
	bucket, prefix string
}

func NewS3Source(ctx context.Context, sourceURL string, options S3Options) (*S3Source, error) {
	u, err := url.Parse(sourceURL)
	if err != nil || u.Scheme != "s3" || !strings.HasPrefix(sourceURL, "s3://") || u.Host == "" ||
		u.User != nil || strings.ContainsAny(u.Host, ":%\\") || u.RawQuery != "" || u.ForceQuery ||
		strings.Contains(sourceURL, "#") || strings.ContainsFunc(sourceURL, unicode.IsControl) {
		return nil, fmt.Errorf("invalid S3 source: expected s3://bucket[/prefix] without credentials, port, query or fragment")
	}
	prefix := strings.TrimPrefix(u.Path, "/")
	if strings.ContainsFunc(prefix, unicode.IsControl) {
		return nil, fmt.Errorf("invalid S3 source: prefix contains control characters")
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if options.Endpoint != "" {
		endpoint, err := url.Parse(options.Endpoint)
		if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" ||
			endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(options.Endpoint, "#") ||
			strings.ContainsFunc(options.Endpoint, unicode.IsControl) {
			return nil, fmt.Errorf("invalid S3 endpoint: expected an http/https URL without credentials, query or fragment")
		}
	}
	var loadOptions []func(*config.LoadOptions) error
	if options.Region != "" {
		loadOptions = append(loadOptions, config.WithRegion(options.Region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, fmt.Errorf("S3 region is required: configure an AWS region or use --s3-region")
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = options.PathStyle
		if options.Endpoint != "" {
			o.BaseEndpoint = aws.String(options.Endpoint)
		}
	})
	return &S3Source{client: client, bucket: u.Host, prefix: prefix}, nil
}

func (s *S3Source) List(ctx context.Context, recursive bool) ([]S3Object, error) {
	input := &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(s.prefix), EncodingType: types.EncodingTypeUrl}
	if !recursive {
		input.Delimiter = aws.String("/")
	}
	pages := s3.NewListObjectsV2Paginator(s.client, input)
	var objects []S3Object
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list S3 objects: %w", err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if page.EncodingType == types.EncodingTypeUrl {
				key, err = url.PathUnescape(key)
				if err != nil {
					return nil, fmt.Errorf("invalid URL-encoded S3 key: %w", err)
				}
			}
			if key == "" || strings.HasSuffix(key, "/") {
				continue
			}
			u := url.URL{Scheme: "s3", Host: s.bucket, Path: "/" + key}
			objects = append(objects, S3Object{Key: key, URL: u.String()})
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	return objects, nil
}

func (s *S3Source) RelativePath(key string) (string, error) {
	if !strings.HasPrefix(key, s.prefix) {
		return "", fmt.Errorf("S3 key %q is outside the source prefix", key)
	}
	rel := strings.TrimPrefix(key, s.prefix)
	unsafe := func() (string, error) { return "", fmt.Errorf("S3 key %q is not a safe local path", key) }
	if strings.ContainsAny(rel, "\\:<>\"|?*") || strings.ContainsFunc(rel, unicode.IsControl) {
		return unsafe()
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return unsafe()
		}
		base, _, _ := strings.Cut(part, ".")
		base = strings.ToUpper(strings.TrimRight(base, " "))
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return unsafe()
		}
		if (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
			strings.Contains("|1|2|3|4|5|6|7|8|9|\u00b9|\u00b2|\u00b3|", "|"+base[3:]+"|") {
			return unsafe()
		}
	}
	local, err := filepath.Localize(rel)
	if err != nil || !filepath.IsLocal(local) {
		return unsafe()
	}
	contained, err := filepath.Rel(".", filepath.Join(".", local))
	if err != nil || !filepath.IsLocal(contained) {
		return unsafe()
	}
	return local, nil
}

func (s *S3Source) Download(ctx context.Context, key, filename string) (err error) {
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("get S3 object: %w", err)
	}
	if object.Body == nil {
		return fmt.Errorf("get S3 object: missing response body")
	}
	var once sync.Once
	var closeErr error
	closeBody := func() { once.Do(func() { closeErr = object.Body.Close() }) }
	stop := context.AfterFunc(ctx, closeBody)
	var file *os.File
	defer func() {
		stop()
		closeBody()
		if err == nil {
			err = closeErr
		}
		if file != nil {
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				_ = os.Remove(filename)
			}
		}
	}()
	file, err = os.Create(filename)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	n, err := io.Copy(file, s3ContextReader{ctx, object.Body})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("download S3 object: %w", err)
	}
	if object.ContentLength != nil && n != *object.ContentLength {
		return fmt.Errorf("download S3 object: size mismatch: got %d bytes, expected %d", n, *object.ContentLength)
	}
	return nil
}

type s3ContextReader struct {
	ctx context.Context
	io.Reader
}

func (r s3ContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

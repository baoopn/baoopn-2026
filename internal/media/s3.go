package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// S3Config configures the S3-compatible bucket (Cloudflare R2 in production,
// RustFS in development). The bucket is used only when Enabled reports true.
type S3Config struct {
	Endpoint        string // https://<account>.r2.cloudflarestorage.com
	Region          string // "auto" for R2
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle addresses the bucket as <endpoint>/<bucket> instead of
	// <bucket>.<endpoint>; needed for RustFS on localhost, not for R2.
	PathStyle bool
	// PublicURL is the bucket's public origin (https://media.example.com).
	// Empty: the API streams objects itself at /media/<filename>.
	PublicURL string
	// HTTPClient overrides the SDK's HTTP client (tests).
	HTTPClient *http.Client
}

// Enabled reports whether the bucket, endpoint and both keys are set.
func (c S3Config) Enabled() bool {
	return c.Endpoint != "" && c.Bucket != "" && c.AccessKeyID != "" && c.SecretAccessKey != ""
}

const immutableCache = "public, max-age=31536000, immutable"

// s3Backend stores objects in one bucket under their media filename.
type s3Backend struct {
	client *s3.Client
	bucket string
}

func newS3Backend(c S3Config) *s3Backend {
	region := c.Region
	if region == "" {
		region = "auto"
	}
	opts := s3.Options{
		Region:       region,
		BaseEndpoint: aws.String(strings.TrimRight(c.Endpoint, "/")),
		UsePathStyle: c.PathStyle,
		Credentials:  credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""),
		// R2 and RustFS reject or ignore the SDK's default trailing CRC
		// checksums (aws-chunked uploads); only send them when an operation
		// requires one.
		RequestChecksumCalculation:                aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation:                aws.ResponseChecksumValidationWhenRequired,
		DisableLogOutputChecksumValidationSkipped: true,
	}
	if c.HTTPClient != nil {
		opts.HTTPClient = c.HTTPClient
	}
	return &s3Backend{client: s3.New(opts), bucket: c.Bucket}
}

// putDisposition returns the Content-Disposition stored with an object. An SVG
// opened straight from the bucket's public domain (a sibling of the site)
// is downloaded, never rendered, so its scripts cannot run there.
func putDisposition(ctype string) *string {
	switch ctype {
	case "image/svg+xml":
		return aws.String("attachment")
	case "application/pdf":
		return aws.String("inline")
	}
	return nil
}

// put uploads the local file at path as name.
func (b *s3Backend) put(ctx context.Context, name, ctype, path string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:             aws.String(b.bucket),
		Key:                aws.String(name),
		Body:               f, // seekable: signed without aws-chunked encoding
		ContentLength:      aws.Int64(size),
		ContentType:        aws.String(ctype),
		CacheControl:       aws.String(immutableCache),
		ContentDisposition: putDisposition(ctype),
	})
	if err != nil {
		return fmt.Errorf("media: put object: %w", err)
	}
	return nil
}

// exists reports whether the object is in the bucket.
func (b *s3Backend) exists(ctx context.Context, name string) (bool, error) {
	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(b.bucket), Key: aws.String(name)})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, err
}

func (b *s3Backend) remove(ctx context.Context, name string) error {
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b.bucket), Key: aws.String(name)})
	return err
}

// object is a fetched object (or, for HEAD, only its metadata).
type object struct {
	Body          io.ReadCloser // nil for HEAD
	ContentLength int64
	ContentRange  string
	ETag          string
	LastModified  time.Time
}

// get fetches name; rng and inm are optional Range and If-None-Match values.
func (b *s3Backend) get(ctx context.Context, name, rng, inm string, head bool) (object, error) {
	var o object
	if head {
		out, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(b.bucket), Key: aws.String(name)})
		if err != nil {
			return o, err
		}
		o.ContentLength = aws.ToInt64(out.ContentLength)
		o.ETag = aws.ToString(out.ETag)
		o.LastModified = aws.ToTime(out.LastModified)
		return o, nil
	}
	in := &s3.GetObjectInput{Bucket: aws.String(b.bucket), Key: aws.String(name)}
	if rng != "" {
		in.Range = aws.String(rng)
	}
	if inm != "" {
		in.IfNoneMatch = aws.String(inm)
	}
	out, err := b.client.GetObject(ctx, in)
	if err != nil {
		return o, err
	}
	o.Body = out.Body
	o.ContentLength = aws.ToInt64(out.ContentLength)
	o.ContentRange = aws.ToString(out.ContentRange)
	o.ETag = aws.ToString(out.ETag)
	o.LastModified = aws.ToTime(out.LastModified)
	return o, nil
}

// isNotFound reports a missing object (404 on HEAD has no error code body).
func isNotFound(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "NotFound", "NoSuchKey":
			return true
		}
	}
	return httpStatus(err) == http.StatusNotFound
}

// httpStatus returns the HTTP status of a failed S3 call, or 0.
func httpStatus(err error) int {
	var re interface{ HTTPStatusCode() int }
	if errors.As(err, &re) {
		return re.HTTPStatusCode()
	}
	return 0
}

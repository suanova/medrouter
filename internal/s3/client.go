// Package s3 is the narrow S3-compatible object-store client MedRouter uses to
// browse dataset contents. It wraps aws-sdk-go-v2 and exposes only the three
// operations the dataset file browser needs: list a "directory" one level at a
// time, stat an object, and sign a short-lived download URL.
//
// Errors are classified into the stable reasons the API exposes; messages never
// carry response bodies, bucket names or credentials.
package s3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
)

// Stable reasons returned to the API layer. The frontend translates them.
const (
	ReasonAuth        = "S3_AUTH"
	ReasonNotFound    = "NOT_FOUND"
	ReasonUnavailable = "S3_UNAVAILABLE"
	ReasonTimeout     = "TIMEOUT"
)

// Error is a classified S3 failure. Error() is safe to log: it never contains
// a response body, an object key or credentials.
type Error struct {
	Reason    string
	Status    int
	Retryable bool
	Code      string
	Op        string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("s3 %s: %s (%s)", e.Op, e.Reason, e.Code)
	}
	return fmt.Sprintf("s3 %s: %s", e.Op, e.Reason)
}

// Object is one stored object (or a stat result).
type Object struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

// ListPage is one page of a delimiter-based listing.
type ListPage struct {
	// Prefixes are the common prefixes (sub-directories), as full keys ending
	// in "/".
	Prefixes []string
	Objects  []Object
	// NextToken is empty when there are no more pages.
	NextToken string
	Truncated bool
}

// Options configures a Client. Endpoint is optional: leave it empty to use the
// standard AWS S3 endpoint for Region (for example on real AWS), or set it to
// an absolute http(s) URL for Ceph RGW and other S3-compatible stores.
type Options struct {
	Endpoint        string
	Region          string
	PathStyle       bool
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	HTTPClient      *http.Client
}

// Client talks to one S3-compatible endpoint.
type Client struct {
	core    *awss3.Client
	presign *awss3.PresignClient
}

// New validates the options and builds a client. Empty credentials mean
// anonymous access, which only works for public buckets.
func New(opts Options) (*Client, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(opts.Endpoint), "/")
	if endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("s3 endpoint must be an absolute http(s) URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("s3 endpoint must use http or https")
		}
	}

	region := strings.TrimSpace(opts.Region)
	if region == "" {
		region = "us-east-1"
	}

	accessKey := strings.TrimSpace(opts.AccessKeyID)
	secretKey := strings.TrimSpace(opts.SecretAccessKey)
	var provider aws.CredentialsProvider = aws.AnonymousCredentials{}
	if accessKey != "" || secretKey != "" {
		if accessKey == "" || secretKey == "" {
			return nil, fmt.Errorf("s3 credentials need both an access key id and a secret access key")
		}
		provider = credentials.NewStaticCredentialsProvider(accessKey, secretKey, strings.TrimSpace(opts.SessionToken))
	}

	cfg := aws.Config{
		Region:      region,
		Credentials: provider,
	}
	if opts.HTTPClient != nil {
		cfg.HTTPClient = opts.HTTPClient
	}
	core := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = opts.PathStyle
	})
	return &Client{core: core, presign: awss3.NewPresignClient(core)}, nil
}

// List returns one level of bucket contents under prefix. A non-empty
// delimiter groups keys into common prefixes instead of enumerating every key.
func (c *Client) List(ctx context.Context, bucket, prefix, delimiter, token string, maxKeys int32) (*ListPage, error) {
	input := &awss3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int32(maxKeys),
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	if delimiter != "" {
		input.Delimiter = aws.String(delimiter)
	}
	if token != "" {
		input.ContinuationToken = aws.String(token)
	}

	out, err := c.core.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, classify(ctx, "list", err)
	}

	page := &ListPage{
		Truncated: aws.ToBool(out.IsTruncated),
		NextToken: aws.ToString(out.NextContinuationToken),
	}
	for _, p := range out.CommonPrefixes {
		if key := aws.ToString(p.Prefix); key != "" {
			page.Prefixes = append(page.Prefixes, key)
		}
	}
	for _, o := range out.Contents {
		key := aws.ToString(o.Key)
		if key == "" {
			continue
		}
		page.Objects = append(page.Objects, Object{
			Key:          key,
			Size:         aws.ToInt64(o.Size),
			ETag:         strings.Trim(aws.ToString(o.ETag), `"`),
			LastModified: aws.ToTime(o.LastModified),
		})
	}
	return page, nil
}

// Head returns object metadata and reports the same NOT_FOUND reason as List
// when the key does not exist.
func (c *Client) Head(ctx context.Context, bucket, key string) (Object, error) {
	out, err := c.core.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return Object{}, classify(ctx, "head", err)
	}
	return Object{
		Key:          key,
		Size:         aws.ToInt64(out.ContentLength),
		ETag:         strings.Trim(aws.ToString(out.ETag), `"`),
		LastModified: aws.ToTime(out.LastModified),
	}, nil
}

// PresignGet returns a time-limited GET URL for a single object.
func (c *Client) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, func(o *awss3.PresignOptions) {
		o.Expires = ttl
	})
	if err != nil {
		return "", classify(ctx, "presign", err)
	}
	return req.URL, nil
}

// classify maps SDK and transport failures onto stable reasons. It deliberately
// discards service messages and response bodies.
func classify(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Error{Reason: ReasonTimeout, Retryable: true, Op: op}
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return &Error{Reason: ReasonTimeout, Retryable: true, Op: op}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &Error{Reason: ReasonTimeout, Retryable: true, Op: op}
	}

	code := ""
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code = apiErr.ErrorCode()
	}

	status := 0
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		status = respErr.HTTPStatusCode()
	}

	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return &Error{Reason: ReasonAuth, Status: status, Retryable: false, Code: code, Op: op}
	case status == http.StatusNotFound || code == "NoSuchKey" || code == "NoSuchBucket" || code == "NotFound":
		return &Error{Reason: ReasonNotFound, Status: http.StatusNotFound, Retryable: false, Code: code, Op: op}
	default:
		return &Error{Reason: ReasonUnavailable, Status: status, Retryable: true, Code: code, Op: op}
	}
}

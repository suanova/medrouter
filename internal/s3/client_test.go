package s3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Options{
		Endpoint:        endpoint,
		Region:          "us-east-1",
		PathStyle:       true,
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		HTTPClient:      &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestListParsesPageAndUsesPathStyle(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>datasets</Name>
  <Prefix>demo/2024/</Prefix>
  <KeyCount>1</KeyCount>
  <MaxKeys>200</MaxKeys>
  <IsTruncated>true</IsTruncated>
  <Contents>
    <Key>demo/2024/a.csv</Key>
    <LastModified>2026-11-01T00:00:00.000Z</LastModified>
    <ETag>&quot;abc123&quot;</ETag>
    <Size>42</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <CommonPrefixes><Prefix>demo/2024/sub/</Prefix></CommonPrefixes>
  <NextContinuationToken>tok-2</NextContinuationToken>
</ListBucketResult>`)
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	page, err := client.List(context.Background(), "datasets", "demo/2024/", "/", "tok-1", 200)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if gotPath != "/datasets" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery.Get("list-type") != "2" || gotQuery.Get("prefix") != "demo/2024/" ||
		gotQuery.Get("delimiter") != "/" || gotQuery.Get("max-keys") != "200" ||
		gotQuery.Get("continuation-token") != "tok-1" {
		t.Fatalf("query = %v", gotQuery)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=test-key/") {
		t.Fatalf("request was not signed: %q", gotAuth)
	}

	if !page.Truncated || page.NextToken != "tok-2" {
		t.Fatalf("page = %#v", page)
	}
	if len(page.Prefixes) != 1 || page.Prefixes[0] != "demo/2024/sub/" {
		t.Fatalf("prefixes = %#v", page.Prefixes)
	}
	if len(page.Objects) != 1 {
		t.Fatalf("objects = %#v", page.Objects)
	}
	object := page.Objects[0]
	if object.Key != "demo/2024/a.csv" || object.Size != 42 || object.ETag != "abc123" {
		t.Fatalf("object = %#v", object)
	}
	if !object.LastModified.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("last modified = %s", object.LastModified)
	}
}

func TestListClassifiesErrors(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   string
	}{
		{http.StatusForbidden, "AccessDenied", ReasonAuth},
		{http.StatusNotFound, "NoSuchBucket", ReasonNotFound},
		{http.StatusInternalServerError, "InternalError", ReasonUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>nope</Message></Error>", tc.code)
			}))
			defer server.Close()

			client := testClient(t, server.URL)
			_, err := client.List(context.Background(), "datasets", "demo/", "/", "", 200)
			var s3Err *Error
			if !errors.As(err, &s3Err) || s3Err.Reason != tc.want {
				t.Fatalf("err = %#v", err)
			}
			if s3Err.Error() == "" || strings.Contains(s3Err.Error(), "nope") {
				t.Fatalf("error leaks the response body: %q", s3Err.Error())
			}
		})
	}
}

func TestHeadNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, "<Error><Code>NoSuchKey</Code></Error>")
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	_, err := client.Head(context.Background(), "datasets", "demo/2024/missing.csv")
	var s3Err *Error
	if !errors.As(err, &s3Err) || s3Err.Reason != ReasonNotFound {
		t.Fatalf("err = %#v", err)
	}
}

func TestPresignGet(t *testing.T) {
	client := testClient(t, "http://127.0.0.1:9000")
	raw, err := client.PresignGet(context.Background(), "datasets", "demo/2024/a.csv", 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Path != "/datasets/demo/2024/a.csv" {
		t.Fatalf("path = %q", parsed.Path)
	}
	query := parsed.Query()
	for key, want := range map[string]string{
		"X-Amz-Algorithm": "AWS4-HMAC-SHA256",
		"X-Amz-Expires":   "600",
	} {
		if got := query.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if signature := query.Get("X-Amz-Signature"); len(signature) != 64 {
		t.Fatalf("signature = %q", signature)
	}
}

func TestPresignGetPathStyleCustomEndpoint(t *testing.T) {
	// UCloud US3 / UFile style: custom endpoint, region code from the host,
	// path-style addressing.
	client, err := New(Options{
		Endpoint:        "https://s3-cn-sh2.example.com",
		Region:          "cn-sh2",
		PathStyle:       true,
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := client.PresignGet(context.Background(), "my-bucket", "pubchem/a.csv", 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Host != "s3-cn-sh2.example.com" || parsed.Path != "/my-bucket/pubchem/a.csv" {
		t.Fatalf("url = %s", raw)
	}
	credential := parsed.Query().Get("X-Amz-Credential")
	if !strings.Contains(credential, "/cn-sh2/s3/aws4_request") {
		t.Fatalf("credential = %q", credential)
	}
}

func TestTimeoutIsClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := testClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.List(ctx, "datasets", "demo/", "/", "", 200)
	var s3Err *Error
	if !errors.As(err, &s3Err) || s3Err.Reason != ReasonTimeout || !s3Err.Retryable {
		t.Fatalf("err = %#v", err)
	}
}

func TestNewValidatesOptions(t *testing.T) {
	cases := []Options{
		{Endpoint: "ftp://example.com"},
		{Endpoint: "http://example.com", AccessKeyID: "only-key"},
		{Endpoint: "http://example.com", SecretAccessKey: "only-secret"},
	}
	for _, opts := range cases {
		if _, err := New(opts); err == nil {
			t.Fatalf("New(%#v) should fail", opts)
		}
	}
}

func TestNewAllowsDefaultAWSEndpoint(t *testing.T) {
	// An empty endpoint means the standard AWS S3 endpoint for the region.
	client, err := New(Options{
		Region:          "ap-east-1",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := client.PresignGet(context.Background(), "my-bucket", "prefix/file.csv", time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Virtual-hosted addressing is the default for real AWS.
	if parsed.Host != "my-bucket.s3.ap-east-1.amazonaws.com" {
		t.Fatalf("host = %q", parsed.Host)
	}
	if parsed.Path != "/prefix/file.csv" {
		t.Fatalf("path = %q", parsed.Path)
	}
}

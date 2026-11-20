package service

import (
	"context"
	"errors"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/s3"
)

// Reasons the file endpoints return alongside available=false. The values are
// stable API codes; the frontend owns the localized text.
const (
	FilesDisabled           = "S3_DISABLED"
	FilesUnsupportedStorage = "UNSUPPORTED_STORAGE"
	FilesURIMissing         = "S3_URI_MISSING"
	FilesInvalidPath        = "INVALID_PATH"
	FilesNotFound           = "NOT_FOUND"
	FilesAuth               = s3.ReasonAuth
	FilesUnavailable        = s3.ReasonUnavailable
	FilesTimeout            = s3.ReasonTimeout

	// Preview and download limits.
	FilesPreviewDisabled    = "PREVIEW_DISABLED"
	FilesPreviewUnsupported = "PREVIEW_UNSUPPORTED_FORMAT"
	FilesPreviewTooLarge    = "PREVIEW_TOO_LARGE"
	FilesTooLarge           = "FILE_TOO_LARGE"
	FilesQuotaExceeded      = "DOWNLOAD_QUOTA_EXCEEDED"
)

const (
	filesDefaultLimit = 200
	filesMaxLimit     = 1000

	defaultPreviewTextWindow = 256 << 10
	maxPreviewTextWindow     = 1 << 20
	defaultPreviewImageMax   = 10 << 20
	defaultPreviewPDFMax     = 32 << 20
)

// FilesStatus is the availability envelope of the file listing.
type FilesStatus struct {
	Available   bool      `json:"available"`
	Reason      string    `json:"reason,omitempty"`
	Message     string    `json:"message,omitempty"`
	Retryable   bool      `json:"retryable"`
	GeneratedAt time.Time `json:"generated_at"`
}

// FileEntry is one directory or file under the current prefix. Path is
// relative to the dataset version root and never contains the bucket or the
// full S3 key.
type FileEntry struct {
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	Type         string     `json:"type"`
	Size         *int64     `json:"size,omitempty"`
	LastModified *time.Time `json:"last_modified,omitempty"`
}

// FileListing is the payload of GET /api/datasets/:name/files.
type FileListing struct {
	FilesStatus
	Ref        string      `json:"ref"`
	Version    string      `json:"version"`
	Storage    string      `json:"storage"`
	Prefix     string      `json:"prefix"`
	Parent     string      `json:"parent,omitempty"`
	Entries    []FileEntry `json:"entries"`
	Truncated  bool        `json:"truncated"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// FileDownload is the payload of GET /api/datasets/:name/files/download.
// URL is a short-lived presigned GET; it is only set when Available is true
// and Mode is "presign". With Mode "proxy" the browser must fetch the
// /files/content endpoint instead, and bytes stream through MedRouter.
type FileDownload struct {
	Available bool       `json:"available"`
	Reason    string     `json:"reason,omitempty"`
	Message   string     `json:"message,omitempty"`
	Retryable bool       `json:"retryable"`
	Mode      string     `json:"mode,omitempty"`
	Size      int64      `json:"size"`
	URL       string     `json:"url,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// FilePreview is the payload of GET /api/datasets/:name/files/preview. Content
// carries at most WindowBytes of text; binary kinds instead point the browser
// at the same-origin /files/content endpoint.
type FilePreview struct {
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
	Message     string `json:"message,omitempty"`
	Retryable   bool   `json:"retryable"`
	Kind        string `json:"kind"`
	Previewable bool   `json:"previewable"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type,omitempty"`
	Content     string `json:"content,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	Truncated   bool   `json:"truncated"`
	WindowBytes int64  `json:"window_bytes,omitempty"`
	// ContentURL is filled in by the controller, which knows the API prefix.
	ContentURL string `json:"content_url,omitempty"`
}

// PreviewContent is one ranged read used to stream a file.
type PreviewContent struct {
	Body        io.ReadCloser
	Size        int64
	Offset      int64
	Length      int64
	Partial     bool
	ContentType string
	ETag        string
	// Attachment marks a proxy-mode download rather than a preview.
	Attachment bool
}

// ObjectStore is the S3 subset the file browser depends on. Keeping it an
// interface lets handler tests run without an object store.
type ObjectStore interface {
	List(ctx context.Context, bucket, prefix, delimiter, token string, maxKeys int32) (*s3.ListPage, error)
	Head(ctx context.Context, bucket, key string) (s3.Object, error)
	GetRange(ctx context.Context, bucket, key string, offset, length int64) (*s3.RangeObject, error)
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

// VersionStorage is the subset of a dataset/model version the file browser
// needs. S3URI stays server-side.
type VersionStorage struct {
	Ref      string
	Version  string
	Strategy string
	S3URI    string
}

// StorageStatus reports whether the catalog could resolve a version. It is the
// generic form of the dataset/model catalog availability envelope.
type StorageStatus struct {
	Available bool
	Reason    string
	Message   string
	Retryable bool
}

// StorageResolver maps a dataset or model version to its storage block.
type StorageResolver interface {
	ResolveVersionStorage(ctx context.Context, name, versionRef string) (VersionStorage, bool, StorageStatus)
}

// FilesService resolves a version to its S3 location and serves one directory
// level at a time. A nil store reports S3_DISABLED rather than failing the
// request. It is shared by the dataset and model libraries.
type FilesService struct {
	resolver   StorageResolver
	store      ObjectStore
	presignTTL time.Duration
	cacheTTL   time.Duration
	headTTL    time.Duration
	timeout    time.Duration
	streamTTL  time.Duration
	now        func() time.Time

	previewEnabled  bool
	previewWindow   int64
	previewImageMax int64
	previewPDFMax   int64
	downloadMode    string
	downloadMax     int64
	largeThreshold  int64
	largeTTL        time.Duration
	limiter         *Limiter
	quota           *common.ByteQuota

	mu        sync.Mutex
	cache     map[string]filesCacheEntry
	headMu    sync.Mutex
	headCache map[string]headCacheEntry
	group     singleflight.Group
}

type filesCacheEntry struct {
	page      *s3.ListPage
	expiresAt time.Time
}

type headCacheEntry struct {
	object    s3.Object
	expiresAt time.Time
}

// Limiter bounds concurrent streaming responses within one process. Model and
// dataset services must share one instance: two copies would double the cap.
type Limiter struct {
	slots chan struct{}
}

// NewLimiter builds a limiter with n slots. A non-positive n keeps a small
// default so a misconfiguration cannot disable back-pressure entirely.
func NewLimiter(n int) *Limiter {
	if n <= 0 {
		n = 16
	}
	return &Limiter{slots: make(chan struct{}, n)}
}

// Acquire blocks until a slot is free or ctx is done. The returned release must
// be called once, and it is safe to call without a successful acquire.
func (l *Limiter) Acquire(ctx context.Context) (release func(), err error) {
	if l == nil {
		return func() {}, nil
	}
	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}

// FilesOption customizes a FilesService. Options exist so model and dataset
// services can share one limiter and one quota.
type FilesOption func(*FilesService)

// WithLimiter shares a concurrency limiter across services.
func WithLimiter(limiter *Limiter) FilesOption {
	return func(s *FilesService) { s.limiter = limiter }
}

// WithQuota shares a byte quota across services.
func WithQuota(quota *common.ByteQuota) FilesOption {
	return func(s *FilesService) { s.quota = quota }
}

func NewFilesService(cfg *config.Config, resolver StorageResolver, store ObjectStore, opts ...FilesOption) *FilesService {
	presignTTL := cfg.S3PresignTTL
	if presignTTL <= 0 {
		presignTTL = 10 * time.Minute
	}
	cacheTTL := cfg.S3ListCacheTTL
	if cacheTTL < 0 {
		cacheTTL = 0
	}
	timeout := cfg.S3Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	window := cfg.S3PreviewTextWindow
	if window <= 0 {
		window = defaultPreviewTextWindow
	}
	if window > maxPreviewTextWindow {
		window = maxPreviewTextWindow
	}
	imageMax := cfg.S3PreviewImageMax
	if imageMax <= 0 {
		imageMax = defaultPreviewImageMax
	}
	pdfMax := cfg.S3PreviewPDFMax
	if pdfMax <= 0 {
		pdfMax = defaultPreviewPDFMax
	}
	mode := cfg.S3DownloadMode
	if mode != "proxy" {
		mode = "presign"
	}
	service := &FilesService{
		resolver:        resolver,
		store:           store,
		presignTTL:      presignTTL,
		cacheTTL:        cacheTTL,
		headTTL:         cfg.S3HeadCacheTTL,
		timeout:         timeout,
		streamTTL:       cfg.S3StreamTimeout,
		now:             time.Now,
		previewEnabled:  cfg.S3PreviewEnabled,
		previewWindow:   window,
		previewImageMax: imageMax,
		previewPDFMax:   pdfMax,
		downloadMode:    mode,
		downloadMax:     cfg.S3DownloadMax,
		largeThreshold:  cfg.S3DownloadLargeThreshold,
		largeTTL:        cfg.S3DownloadLargeTTL,
		quota:           common.NewByteQuota(cfg.S3DownloadDailyQuota, 24*time.Hour),
		cache:           map[string]filesCacheEntry{},
		headCache:       map[string]headCacheEntry{},
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

// StreamTimeout is the write deadline a streaming handler should apply.
func (s *FilesService) StreamTimeout() time.Duration {
	if s.streamTTL > 0 {
		return s.streamTTL
	}
	return 5 * time.Minute
}

// AcquireSlot takes one preview slot. The returned release must always be
// called.
func (s *FilesService) AcquireSlot(ctx context.Context) (func(), error) {
	return s.limiter.Acquire(ctx)
}

// List returns one directory level of a dataset or model version. found is
// false only when the version does not exist; an unavailable catalog or S3
// problem is reported through the embedded status instead.
func (s *FilesService) List(ctx context.Context, name, versionRef, rawPrefix, cursor string, limit int) (*FileListing, bool) {
	now := s.now().UTC()

	version, ok, status := s.resolver.ResolveVersionStorage(ctx, name, versionRef)
	if !status.Available {
		return &FileListing{
			FilesStatus: FilesStatus{
				Available:   false,
				Reason:      status.Reason,
				Message:     status.Message,
				Retryable:   status.Retryable,
				GeneratedAt: now,
			},
			Entries: []FileEntry{},
		}, true
	}
	if !ok {
		return nil, false
	}

	files := &FileListing{
		FilesStatus: FilesStatus{Available: true, GeneratedAt: now},
		Ref:         version.Ref,
		Version:     version.Version,
		Storage:     version.Strategy,
		Entries:     []FileEntry{},
	}

	if version.Strategy != "S3" {
		return files.unavailable(FilesUnsupportedStorage, false), true
	}
	if s.store == nil {
		return files.unavailable(FilesDisabled, false), true
	}

	bucket, rootPrefix, err := ParseS3URI(version.S3URI)
	if err != nil {
		return files.unavailable(FilesURIMissing, false), true
	}
	prefix, err := NormalizePrefix(rawPrefix)
	if err != nil {
		return files.unavailable(FilesInvalidPath, false), true
	}

	limit = clampLimit(limit)
	fullPrefix := rootPrefix + "/" + prefix
	page, err := s.listPage(ctx, bucket, fullPrefix, cursor, int32(limit))
	if err != nil {
		reason, retryable := s3Reason(err)
		return files.unavailable(reason, retryable), true
	}

	files.Prefix = prefix
	files.Parent = parentPrefix(prefix)
	files.Entries = projectEntries(page, rootPrefix, prefix)
	files.Truncated = page.Truncated
	if page.Truncated {
		files.NextCursor = page.NextToken
	}
	return files, true
}

// objectLocation is a validated object inside one version's root prefix.
type objectLocation struct {
	Version VersionStorage
	Bucket  string
	Key     string
	Path    string
}

// resolveFile maps a version and a relative path to an S3 object. A non-empty
// reason means the request cannot be served; found is false only when the
// version itself does not exist. message carries the catalog's own explanation
// when the failure came from resolving the version.
func (s *FilesService) resolveFile(ctx context.Context, name, versionRef, rawPath string) (loc objectLocation, reason, message string, retryable bool, found bool) {
	version, ok, status := s.resolver.ResolveVersionStorage(ctx, name, versionRef)
	if !status.Available {
		return objectLocation{Version: version}, status.Reason, status.Message, status.Retryable, true
	}
	if !ok {
		return objectLocation{}, "", "", false, false
	}
	loc = objectLocation{Version: version}
	if version.Strategy != "S3" {
		return loc, FilesUnsupportedStorage, "", false, true
	}
	if s.store == nil {
		return loc, FilesDisabled, "", false, true
	}
	bucket, rootPrefix, err := ParseS3URI(version.S3URI)
	if err != nil {
		return loc, FilesURIMissing, "", false, true
	}
	objectPath, err := NormalizeFilePath(rawPath)
	if err != nil {
		return loc, FilesInvalidPath, "", false, true
	}
	loc.Bucket, loc.Path, loc.Key = bucket, objectPath, rootPrefix+"/"+objectPath
	return loc, "", "", false, true
}

// head stats an object, serving repeat lookups from a short-lived cache so a
// preview does not add an extra round trip per click.
func (s *FilesService) head(ctx context.Context, bucket, key string) (s3.Object, error) {
	if s.headTTL <= 0 {
		return s.store.Head(ctx, bucket, key)
	}
	cacheKey := bucket + "\x00" + key
	s.headMu.Lock()
	if entry, ok := s.headCache[cacheKey]; ok && s.now().Before(entry.expiresAt) {
		s.headMu.Unlock()
		return entry.object, nil
	}
	s.headMu.Unlock()

	result, err, _ := s.group.Do("head\x00"+cacheKey, func() (any, error) {
		callCtx, cancel := context.WithTimeout(ctx, s.timeout)
		defer cancel()
		object, err := s.store.Head(callCtx, bucket, key)
		if err != nil {
			return nil, err
		}
		s.headMu.Lock()
		if len(s.headCache) > 5000 {
			s.headCache = map[string]headCacheEntry{}
		}
		s.headCache[cacheKey] = headCacheEntry{object: object, expiresAt: s.now().Add(s.headTTL)}
		s.headMu.Unlock()
		return object, nil
	})
	if err != nil {
		return s3.Object{}, err
	}
	object, _ := result.(s3.Object)
	return object, nil
}

// Download validates a relative path inside the version root and either signs
// a short-lived GET URL or, in proxy mode, tells the caller to stream
// /files/content. found is false only when the version is missing. actor keys
// the download quota.
func (s *FilesService) Download(ctx context.Context, actor, name, versionRef, rawPath string) (*FileDownload, bool) {
	loc, reason, message, retryable, found := s.resolveFile(ctx, name, versionRef, rawPath)
	if !found {
		return nil, false
	}
	if reason != "" {
		denied := unavailableDownload(retryable, reason)
		if message != "" {
			denied.Message = message
		}
		return denied, true
	}

	object, err := s.head(ctx, loc.Bucket, loc.Key)
	if err != nil {
		reason, retryable := s3Reason(err)
		return unavailableDownload(retryable, reason), true
	}

	if s.downloadMax > 0 && object.Size > s.downloadMax {
		return unavailableDownload(false, FilesTooLarge), true
	}
	if s.downloadMode == "proxy" {
		// Proxy mode accounts the bytes when the stream starts, not here: this
		// call is only a preflight so the UI can show the size.
		return &FileDownload{Available: true, Mode: "proxy", Size: object.Size}, true
	}
	if ok, _ := s.quota.Reserve(actor, object.Size); !ok {
		denied := unavailableDownload(false, FilesQuotaExceeded)
		denied.Retryable = true
		return denied, true
	}

	ttl := s.presignTTL
	if s.largeThreshold > 0 && s.largeTTL > 0 && object.Size > s.largeThreshold {
		ttl = s.largeTTL
	}
	url, err := s.store.PresignGet(ctx, loc.Bucket, loc.Key, ttl)
	if err != nil {
		reason, retryable := s3Reason(err)
		return unavailableDownload(retryable, reason), true
	}
	expires := s.now().Add(ttl).UTC()
	return &FileDownload{Available: true, Mode: "presign", Size: object.Size, URL: url, ExpiresAt: &expires}, true
}

// Preview describes one file and, for text, returns its first window. Sizes
// that cannot be previewed are reported through previewable/reason with
// available still true: the file exists, the browser just cannot render it.
func (s *FilesService) Preview(ctx context.Context, name, versionRef, rawPath string) (*FilePreview, bool) {
	loc, reason, message, retryable, found := s.resolveFile(ctx, name, versionRef, rawPath)
	if !found {
		return nil, false
	}
	preview := &FilePreview{Available: true, Kind: string(kindUnsupported)}
	if reason != "" {
		denied := preview.unavailable(reason, retryable)
		if message != "" {
			denied.Message = message
		}
		return denied, true
	}
	if !s.previewEnabled {
		return preview.unavailable(FilesPreviewDisabled, false), true
	}

	object, err := s.head(ctx, loc.Bucket, loc.Key)
	if err != nil {
		reason, retryable := s3Reason(err)
		return preview.unavailable(reason, retryable), true
	}
	preview.Size = object.Size
	kind, contentType := classifyFile(loc.Path)
	preview.Kind, preview.ContentType = string(kind), contentType

	switch kind {
	case kindImage:
		if object.Size > s.previewImageMax {
			return preview.notPreviewable(FilesPreviewTooLarge), true
		}
		preview.Previewable = true
		return preview, true
	case kindPDF:
		if object.Size > s.previewPDFMax {
			return preview.notPreviewable(FilesPreviewTooLarge), true
		}
		preview.Previewable = true
		return preview, true
	case kindText:
		return s.textPreview(ctx, loc, object.Size, preview), true
	default:
		return preview.notPreviewable(FilesPreviewUnsupported), true
	}
}

// textPreview reads at most one window from the head of the object. The cost
// is constant regardless of how large the object is.
func (s *FilesService) textPreview(ctx context.Context, loc objectLocation, size int64, preview *FilePreview) *FilePreview {
	if size == 0 {
		preview.Previewable, preview.Encoding = true, "utf-8"
		return preview
	}
	window := s.previewWindow
	if window > size {
		window = size
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	object, err := s.store.GetRange(callCtx, loc.Bucket, loc.Key, 0, window)
	if err != nil {
		reason, retryable := s3Reason(err)
		return preview.unavailable(reason, retryable)
	}
	defer func() { _ = object.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(object.Body, window))
	if err != nil {
		return preview.unavailable(s3.ReasonUnavailable, true)
	}
	if !utf8.Valid(data) {
		preview.Kind = string(kindUnsupported)
		return preview.notPreviewable(FilesPreviewUnsupported)
	}
	preview.Previewable = true
	preview.Content = string(data)
	preview.Encoding = "utf-8"
	preview.WindowBytes = int64(len(data))
	preview.Truncated = size > int64(len(data))
	return preview
}

// Content opens a ranged read for the same-origin content endpoint. With
// asDownload it serves any file type (proxy-mode downloads); otherwise it only
// serves previewable images and PDFs. A non-empty reason means the request
// cannot be streamed; found is false only when the version is missing.
// offset/length implement one HTTP Range so a PDF viewer can seek.
func (s *FilesService) Content(ctx context.Context, actor, name, versionRef, rawPath string, offset, length int64, asDownload bool) (*PreviewContent, string, bool) {
	loc, reason, _, _, found := s.resolveFile(ctx, name, versionRef, rawPath)
	if !found {
		return nil, "", false
	}
	if reason != "" {
		return nil, reason, true
	}
	if !asDownload && !s.previewEnabled {
		return nil, FilesPreviewDisabled, true
	}

	kind, contentType := classifyFile(loc.Path)
	if !asDownload && kind != kindImage && kind != kindPDF {
		return nil, FilesPreviewUnsupported, true
	}

	object, err := s.head(ctx, loc.Bucket, loc.Key)
	if err != nil {
		reason, _ := s3Reason(err)
		return nil, reason, true
	}
	if asDownload {
		if s.downloadMax > 0 && object.Size > s.downloadMax {
			return nil, FilesTooLarge, true
		}
		if ok, _ := s.quota.Reserve(actor, object.Size); !ok {
			return nil, FilesQuotaExceeded, true
		}
	} else {
		if kind == kindImage && object.Size > s.previewImageMax {
			return nil, FilesPreviewTooLarge, true
		}
		if kind == kindPDF && object.Size > s.previewPDFMax {
			return nil, FilesPreviewTooLarge, true
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if object.Size == 0 {
		return &PreviewContent{
			Body:        io.NopCloser(strings.NewReader("")),
			Size:        0,
			ContentType: contentType,
			Attachment:  asDownload,
		}, "", true
	}
	if offset < 0 || offset >= object.Size {
		return nil, FilesInvalidPath, true
	}
	if length <= 0 || offset+length > object.Size {
		length = object.Size - offset
	}

	streamCtx, cancel := context.WithTimeout(ctx, s.StreamTimeout())
	ranged, err := s.store.GetRange(streamCtx, loc.Bucket, loc.Key, offset, length)
	if err != nil {
		cancel()
		reason, _ := s3Reason(err)
		return nil, reason, true
	}
	return &PreviewContent{
		Body:        &cancelReadCloser{ReadCloser: ranged.Body, cancel: cancel},
		Size:        object.Size,
		Offset:      offset,
		Length:      length,
		Partial:     offset > 0 || length < object.Size,
		ContentType: contentType,
		ETag:        object.ETag,
		Attachment:  asDownload,
	}, "", true
}

// cancelReadCloser cancels the streaming context when the body is closed, so a
// client that disconnects early also aborts the object-store read.
type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func (s *FilesService) listPage(ctx context.Context, bucket, fullPrefix, cursor string, maxKeys int32) (*s3.ListPage, error) {
	key := strings.Join([]string{bucket, fullPrefix, cursor, strconv.Itoa(int(maxKeys))}, "\x00")

	if s.cacheTTL > 0 {
		s.mu.Lock()
		if entry, ok := s.cache[key]; ok && s.now().Before(entry.expiresAt) {
			s.mu.Unlock()
			return entry.page, nil
		}
		s.mu.Unlock()
	}

	result, err, _ := s.group.Do(key, func() (any, error) {
		callCtx, cancel := context.WithTimeout(ctx, s.timeout)
		defer cancel()
		page, err := s.store.List(callCtx, bucket, fullPrefix, "/", cursor, maxKeys)
		if err != nil {
			return nil, err
		}
		if s.cacheTTL > 0 {
			s.mu.Lock()
			s.cache[key] = filesCacheEntry{page: page, expiresAt: s.now().Add(s.cacheTTL)}
			s.mu.Unlock()
		}
		return page, nil
	})
	if err != nil {
		return nil, err
	}
	page, _ := result.(*s3.ListPage)
	if page == nil {
		page = &s3.ListPage{}
	}
	return page, nil
}

// ParseS3URI splits s3://bucket/prefix into a bucket and a normalized prefix.
// A prefix is required: browsing a whole bucket root is out of scope and is
// treated as an undeclared location.
func ParseS3URI(raw string) (bucket, prefix string, err error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 5 || !strings.EqualFold(raw[:5], "s3://") {
		return "", "", errors.New("not an s3:// URI")
	}
	rest := raw[5:]
	bucket, rawPrefix, _ := strings.Cut(rest, "/")
	if bucket == "" || strings.ContainsAny(bucket, " \t\r\n\\") {
		return "", "", errors.New("invalid S3 bucket")
	}
	if strings.HasPrefix(rawPrefix, "/") || strings.Contains(rawPrefix, "//") {
		return "", "", errors.New("invalid S3 prefix")
	}
	normalized, err := normalizeRelative(rawPrefix, true)
	if err != nil {
		return "", "", err
	}
	if normalized == "" {
		return "", "", errors.New("the S3 URI needs a non-empty prefix")
	}
	return bucket, normalized, nil
}

// NormalizePrefix validates a user-supplied directory relative to the version
// root and returns it with a trailing slash (empty for the root).
func NormalizePrefix(raw string) (string, error) {
	normalized, err := normalizeRelative(raw, true)
	if err != nil {
		return "", err
	}
	if normalized == "" {
		return "", nil
	}
	return normalized + "/", nil
}

// NormalizeFilePath validates a user-supplied file path relative to the version
// root. Directory paths (trailing slash) are rejected.
func NormalizeFilePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasSuffix(trimmed, "/") {
		return "", errors.New("a download path must identify a file")
	}
	normalized, err := normalizeRelative(trimmed, false)
	if err != nil {
		return "", err
	}
	if normalized == "" {
		return "", errors.New("a download path is required")
	}
	return normalized, nil
}

// normalizeRelative rejects absolute paths, backslashes, control characters
// and any "." / ".." segment, then collapses repeated slashes.
func normalizeRelative(raw string, allowEmpty bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("path is required")
	}
	if strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") {
		return "", errors.New("path must be relative")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("path contains control characters")
		}
	}
	segments := strings.Split(raw, "/")
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch segment {
		case "":
			continue
		case ".", "..":
			return "", errors.New("path must not contain . or .. segments")
		}
		out = append(out, segment)
	}
	if len(out) == 0 {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("path is required")
	}
	return strings.Join(out, "/"), nil
}

// projectEntries turns an S3 page into directory/file entries. Path is
// relative to the version root, not to the directory being listed, so callers
// can use one entry's Path directly as the `path` of a preview or download
// request. The bucket name and the version root prefix never leave this
// function.
func projectEntries(page *s3.ListPage, rootPrefix, prefix string) []FileEntry {
	if page == nil {
		return []FileEntry{}
	}
	entries := make([]FileEntry, 0, len(page.Prefixes)+len(page.Objects))
	// Keys arrive relative to the version root; parentPrefix(relative) then
	// tells us whether an entry belongs to the directory we are listing.
	root := rootPrefix + "/"

	for _, prefixKey := range page.Prefixes {
		relative, ok := relativeKey(prefixKey, root)
		if !ok || relative == "" || !strings.HasSuffix(relative, "/") {
			continue
		}
		if parentPrefix(relative) != prefix {
			continue
		}
		trimmed := strings.TrimSuffix(relative, "/")
		if trimmed == "" {
			continue
		}
		entries = append(entries, FileEntry{
			Name: path.Base(trimmed),
			Path: relative,
			Type: "dir",
		})
	}

	for _, object := range page.Objects {
		relative, ok := relativeKey(object.Key, root)
		if !ok || relative == "" || strings.HasSuffix(relative, "/") {
			// The delimiter itself and "directory placeholder" objects are not
			// entries of this directory.
			continue
		}
		if parentPrefix(relative) != prefix {
			continue
		}
		size := object.Size
		modified := object.LastModified
		entries = append(entries, FileEntry{
			Name:         path.Base(relative),
			Path:         relative,
			Type:         "file",
			Size:         &size,
			LastModified: &modified,
		})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "dir"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries
}

func relativeKey(key, base string) (string, bool) {
	if !strings.HasPrefix(key, base) {
		return "", false
	}
	return strings.TrimPrefix(key, base), true
}

func parentPrefix(prefix string) string {
	trimmed := strings.TrimSuffix(prefix, "/")
	if trimmed == "" {
		return ""
	}
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		return trimmed[:index+1]
	}
	return ""
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return filesDefaultLimit
	}
	if limit > filesMaxLimit {
		return filesMaxLimit
	}
	return limit
}

// fileKind is how the preview endpoint classifies an object. The decision is
// made from the extension only: ListObjectsV2 does not return a content type,
// and objects uploaded to an S3-compatible store usually have none set.
type fileKind string

const (
	kindText        fileKind = "text"
	kindImage       fileKind = "image"
	kindPDF         fileKind = "pdf"
	kindUnsupported fileKind = "unsupported"
)

// textExtensions are previewed as UTF-8 text. SVG is deliberately here and not
// in imageExtensions: rendering user-uploaded SVG inline would run its scripts
// on the site origin.
var textExtensions = map[string]struct{}{
	".json": {}, ".jsonl": {}, ".json5": {}, ".ndjson": {}, ".txt": {}, ".md": {},
	".markdown": {}, ".csv": {}, ".tsv": {}, ".yaml": {}, ".yml": {}, ".toml": {},
	".ini": {}, ".cfg": {}, ".conf": {}, ".log": {}, ".py": {}, ".go": {}, ".sh": {},
	".js": {}, ".mjs": {}, ".ts": {}, ".tsx": {}, ".jsx": {}, ".java": {}, ".c": {},
	".cpp": {}, ".h": {}, ".hpp": {}, ".rs": {}, ".sql": {}, ".proto": {}, ".xml": {},
	".html": {}, ".htm": {}, ".css": {}, ".svg": {}, ".mod": {}, ".sum": {},
	".patch": {}, ".diff": {}, ".srt": {}, ".vtt": {}, ".env": {}, ".gitignore": {},
}

// imageExtensions map to the only content types the content endpoint will emit.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// textBasenames cover extension-less files that are conventionally text.
var textBasenames = map[string]struct{}{
	"readme": {}, "license": {}, "notice": {}, "changelog": {}, "makefile": {},
	"dockerfile": {}, ".gitignore": {}, ".dockerignore": {}, ".env": {}, "authors": {},
}

// classifyFile returns the preview kind and the content type to serve.
func classifyFile(name string) (fileKind, string) {
	base := strings.ToLower(path.Base(name))
	extension := path.Ext(base)
	if contentType, ok := imageExtensions[extension]; ok {
		return kindImage, contentType
	}
	if extension == ".pdf" {
		return kindPDF, "application/pdf"
	}
	if _, ok := textExtensions[extension]; ok {
		return kindText, "text/plain; charset=utf-8"
	}
	if extension == "" {
		if _, ok := textBasenames[base]; ok {
			return kindText, "text/plain; charset=utf-8"
		}
	}
	return kindUnsupported, ""
}

// unavailable marks a file that cannot be read at all.
func (f *FilePreview) unavailable(reason string, retryable bool) *FilePreview {
	f.Available = false
	f.Previewable = false
	f.Reason = reason
	f.Message = filesMessage(reason)
	f.Retryable = retryable
	f.Content = ""
	f.ContentURL = ""
	return f
}

// notPreviewable marks a readable file that the browser cannot render. The
// object exists, so available stays true and only previewable/reason are set.
func (f *FilePreview) notPreviewable(reason string) *FilePreview {
	f.Previewable = false
	f.Reason = reason
	f.Message = filesMessage(reason)
	f.Retryable = false
	f.Content = ""
	f.ContentURL = ""
	return f
}

func (f *FileListing) unavailable(reason string, retryable bool) *FileListing {
	f.Available = false
	f.Reason = reason
	f.Message = filesMessage(reason)
	f.Retryable = retryable
	f.Entries = []FileEntry{}
	f.Truncated = false
	f.NextCursor = ""
	return f
}

func unavailableDownload(retryable bool, reason string) *FileDownload {
	return &FileDownload{
		Available: false,
		Reason:    reason,
		Message:   filesMessage(reason),
		Retryable: retryable,
	}
}

func filesMessage(reason string) string {
	switch reason {
	case FilesDisabled:
		return "File browsing is disabled for this deployment."
	case FilesUnsupportedStorage:
		return "This version is not stored in S3, so it cannot be browsed online."
	case FilesURIMissing:
		return "This version does not declare a usable S3 location."
	case FilesInvalidPath:
		return "The requested path is not valid."
	case FilesNotFound:
		return "The object does not exist."
	case FilesPreviewDisabled:
		return "File preview is disabled for this deployment."
	case FilesPreviewUnsupported:
		return "This file format cannot be previewed in the browser; download it instead."
	case FilesPreviewTooLarge:
		return "This file is too large to preview; download it instead."
	case FilesTooLarge:
		return "This file exceeds the download size limit."
	case FilesQuotaExceeded:
		return "The daily download quota has been reached."
	case s3.ReasonAuth:
		return "The object store rejected the platform credentials."
	case s3.ReasonTimeout:
		return "The object store did not respond in time."
	case s3.ReasonUnavailable:
		return "The object store is temporarily unavailable."
	default:
		return "The file contents could not be loaded."
	}
}

func s3Reason(err error) (reason string, retryable bool) {
	var s3Err *s3.Error
	if errors.As(err, &s3Err) {
		return s3Err.Reason, s3Err.Retryable
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return s3.ReasonTimeout, true
	}
	return s3.ReasonUnavailable, true
}

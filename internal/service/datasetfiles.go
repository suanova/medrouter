package service

import (
	"context"
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

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
)

const (
	filesDefaultLimit = 200
	filesMaxLimit     = 1000
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
// URL is a short-lived presigned GET; it is only set when Available is true.
type FileDownload struct {
	Available bool       `json:"available"`
	Reason    string     `json:"reason,omitempty"`
	Message   string     `json:"message,omitempty"`
	Retryable bool       `json:"retryable"`
	URL       string     `json:"url,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// ObjectStore is the S3 subset the file browser depends on. Keeping it an
// interface lets handler tests run without an object store.
type ObjectStore interface {
	List(ctx context.Context, bucket, prefix, delimiter, token string, maxKeys int32) (*s3.ListPage, error)
	Head(ctx context.Context, bucket, key string) (s3.Object, error)
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
	timeout    time.Duration
	now        func() time.Time

	mu    sync.Mutex
	cache map[string]filesCacheEntry
	group singleflight.Group
}

type filesCacheEntry struct {
	page      *s3.ListPage
	expiresAt time.Time
}

func NewFilesService(cfg *config.Config, resolver StorageResolver, store ObjectStore) *FilesService {
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
	return &FilesService{
		resolver:   resolver,
		store:      store,
		presignTTL: presignTTL,
		cacheTTL:   cacheTTL,
		timeout:    timeout,
		now:        time.Now,
		cache:      map[string]filesCacheEntry{},
	}
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

// Download validates a relative path inside the version root and returns a
// presigned GET URL. found is false only when the version is missing.
func (s *FilesService) Download(ctx context.Context, name, versionRef, rawPath string) (*FileDownload, bool) {
	version, ok, status := s.resolver.ResolveVersionStorage(ctx, name, versionRef)
	if !status.Available {
		return &FileDownload{
			Available: false,
			Reason:    status.Reason,
			Message:   status.Message,
			Retryable: status.Retryable,
		}, true
	}
	if !ok {
		return nil, false
	}

	if version.Strategy != "S3" {
		return unavailableDownload(true, FilesUnsupportedStorage), true
	}
	if s.store == nil {
		return unavailableDownload(true, FilesDisabled), true
	}
	bucket, rootPrefix, err := ParseS3URI(version.S3URI)
	if err != nil {
		return unavailableDownload(true, FilesURIMissing), true
	}
	objectPath, err := NormalizeFilePath(rawPath)
	if err != nil {
		return unavailableDownload(true, FilesInvalidPath), true
	}
	key := rootPrefix + "/" + objectPath

	if _, err := s.store.Head(ctx, bucket, key); err != nil {
		reason, retryable := s3Reason(err)
		return unavailableDownload(retryable, reason), true
	}

	url, err := s.store.PresignGet(ctx, bucket, key, s.presignTTL)
	if err != nil {
		reason, retryable := s3Reason(err)
		return unavailableDownload(retryable, reason), true
	}
	expires := s.now().Add(s.presignTTL).UTC()
	return &FileDownload{Available: true, URL: url, ExpiresAt: &expires}, true
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

// projectEntries turns an S3 page into relative directory/file entries. The
// bucket name and the version root prefix never leave this function.
func projectEntries(page *s3.ListPage, rootPrefix, prefix string) []FileEntry {
	if page == nil {
		return []FileEntry{}
	}
	entries := make([]FileEntry, 0, len(page.Prefixes)+len(page.Objects))
	base := rootPrefix + "/" + prefix

	for _, prefixKey := range page.Prefixes {
		relative, ok := relativeKey(prefixKey, base)
		if !ok || relative == "" {
			continue
		}
		trimmed := strings.TrimSuffix(relative, "/")
		if trimmed == "" || strings.Contains(trimmed, "/") {
			continue
		}
		entries = append(entries, FileEntry{
			Name: path.Base(trimmed),
			Path: relative,
			Type: "dir",
		})
	}

	for _, object := range page.Objects {
		relative, ok := relativeKey(object.Key, base)
		if !ok || relative == "" || strings.HasSuffix(relative, "/") || strings.Contains(relative, "/") {
			// The delimiter itself, "directory placeholder" objects and keys
			// from deeper levels are not entries of this directory.
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
		return "Dataset file browsing is disabled for this deployment."
	case FilesUnsupportedStorage:
		return "This version is not stored in S3, so it cannot be browsed online."
	case FilesURIMissing:
		return "This version does not declare a usable S3 location."
	case FilesInvalidPath:
		return "The requested path is not valid."
	case FilesNotFound:
		return "The object does not exist."
	case s3.ReasonAuth:
		return "The object store rejected the platform credentials."
	case s3.ReasonTimeout:
		return "The object store did not respond in time."
	case s3.ReasonUnavailable:
		return "The object store is temporarily unavailable."
	default:
		return "The dataset contents could not be loaded."
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

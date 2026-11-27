package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/suanova/medrouter/internal/config"
)

const (
	// dshUploadsDirName is the subdirectory of the shared dsh volume MedRouter
	// writes to. The dsh pod mounts the same volume at /data, so the directory
	// is visible there as <DSH_UPLOAD_MOUNT>.
	dshUploadsDirName = "uploads"
)

// dshUploadTypes is the extension allowlist. The client Content-Type is never
// trusted; the extension only decides whether the file may be stored.
var dshUploadTypes = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
	".gif":  true,
	".bmp":  true,
	".heic": true,
	".heif": true,
	".pdf":  true,
	".doc":  true,
	".docx": true,
	".xls":  true,
	".xlsx": true,
	".ppt":  true,
	".pptx": true,
	".csv":  true,
	".txt":  true,
	".md":   true,
	".json": true,
	".yaml": true,
	".yml":  true,
	".xml":  true,
	".html": true,
	".zip":  true,
}

// DshUploadConfig describes where attachments are written and what the agent
// sees. Dir is the gateway-side directory; Mount is the equivalent path inside
// the dsh container.
type DshUploadConfig struct {
	Dir      string
	Mount    string
	MaxBytes int64
	MaxFiles int
}

// LoadDshUploadConfig reads the shared-volume layout. Both env values are
// mandatory: one without the other would point the agent at a file it cannot
// open.
func LoadDshUploadConfig(cfg *config.Config) (DshUploadConfig, error) {
	if cfg == nil || !cfg.DshUploadsEnabled() {
		return DshUploadConfig{}, errors.New("DSH uploads are not configured")
	}
	return DshUploadConfig{
		Dir:      filepath.Join(cfg.DshUploadRoot, dshUploadsDirName),
		Mount:    path.Join(cfg.DshUploadMount),
		MaxBytes: cfg.DshUploadMaxBytes,
		MaxFiles: cfg.DshUploadMaxFiles,
	}, nil
}

// DshUploadAccept returns the accepted extensions for the file picker.
func DshUploadAccept() []string {
	accept := make([]string, 0, len(dshUploadTypes))
	for extension := range dshUploadTypes {
		accept = append(accept, extension)
	}
	sort.Strings(accept)
	return accept
}

// SaveDshAttachment stores one upload under <Dir>/<userId> and returns the
// manifest entry the agent will read. The stored name gets a random prefix so
// two users (or two files with the same name) never collide.
func SaveDshAttachment(upload DshUploadConfig, userID int64, header *multipart.FileHeader) (DshAttachment, error) {
	if userID <= 0 {
		return DshAttachment{}, errors.New("invalid user")
	}
	if header.Size > upload.MaxBytes {
		return DshAttachment{}, fmt.Errorf("files can be at most %d MB", upload.MaxBytes>>20)
	}
	name, err := sanitizeDshUploadName(header.Filename)
	if err != nil {
		return DshAttachment{}, err
	}
	extension := strings.ToLower(filepath.Ext(name))
	if !dshUploadTypes[extension] {
		return DshAttachment{}, fmt.Errorf("unsupported file type: %s", extension)
	}
	userDir := dshUserUploadDir(upload.Dir, userID)
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		return DshAttachment{}, fmt.Errorf("could not prepare the upload directory")
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return DshAttachment{}, fmt.Errorf("could not allocate an upload id")
	}
	storedName := hex.EncodeToString(random) + "-" + name
	storedPath := filepath.Join(userDir, storedName)

	source, err := header.Open()
	if err != nil {
		return DshAttachment{}, fmt.Errorf("could not read the uploaded file")
	}
	defer source.Close()
	target, err := os.OpenFile(storedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return DshAttachment{}, fmt.Errorf("could not store the uploaded file")
	}
	written, copyErr := io.Copy(target, io.LimitReader(source, upload.MaxBytes+1))
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(storedPath)
		return DshAttachment{}, fmt.Errorf("could not store the uploaded file")
	}
	// The multipart header's Size is client-controlled; enforce the real count.
	if written > upload.MaxBytes {
		_ = os.Remove(storedPath)
		return DshAttachment{}, fmt.Errorf("files can be at most %d MB", upload.MaxBytes>>20)
	}

	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = mime.TypeByExtension(extension)
	}
	return DshAttachment{
		Name:     name,
		Path:     path.Join(upload.Mount, strconv.FormatInt(userID, 10), storedName),
		MimeType: mimeType,
		Size:     written,
	}, nil
}

// ValidateDshAttachments accepts only files this user uploaded. Paths are the
// ones returned by SaveDshAttachment, so the prefix check plus the local stat
// rejects another user's directory, path traversal and /etc/passwd alike.
func ValidateDshAttachments(upload DshUploadConfig, userID int64, attachments []DshAttachment) error {
	if len(attachments) == 0 {
		return nil
	}
	if userID <= 0 {
		return errors.New("invalid user")
	}
	if len(attachments) > upload.MaxFiles {
		return fmt.Errorf("at most %d files can be attached", upload.MaxFiles)
	}
	userPrefix := path.Join(upload.Mount, strconv.FormatInt(userID, 10)) + "/"
	for _, attachment := range attachments {
		raw := strings.TrimSpace(attachment.Path)
		if raw == "" {
			return errors.New("each attachment needs a path")
		}
		// Clean collapses ".." and duplicate separators before the prefix check.
		cleaned := path.Clean(raw)
		if !strings.HasPrefix(cleaned, userPrefix) {
			return errors.New("attachment paths must come from this user's uploads")
		}
		local := filepath.Join(upload.Dir, strings.TrimPrefix(cleaned, upload.Mount))
		stat, statErr := os.Lstat(local)
		if statErr != nil || !stat.Mode().IsRegular() {
			return errors.New("attachment file is missing")
		}
	}
	return nil
}

func sanitizeDshUploadName(raw string) (string, error) {
	// Windows clients send backslashes as separators; normalize before basename.
	name := filepath.Base(strings.ReplaceAll(raw, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	if name == "" {
		return "", errors.New("the uploaded file needs a name")
	}
	if len(name) > 180 {
		// Keep the extension and cut the stem on a rune boundary; slicing a
		// Chinese filename by bytes would leave half a character.
		extension := filepath.Ext(name)
		budget := 180 - len(extension)
		if budget < 1 {
			budget = 1
		}
		stem := strings.TrimSuffix(name, extension)
		var builder strings.Builder
		used := 0
		for _, char := range stem {
			size := utf8.RuneLen(char)
			if used+size > budget {
				break
			}
			builder.WriteRune(char)
			used += size
		}
		name = builder.String() + extension
	}
	return name, nil
}

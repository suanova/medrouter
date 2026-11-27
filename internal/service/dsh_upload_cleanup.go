package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/suanova/medrouter/internal/config"
)

const (
	// dshUploadCleanupInterval is the scan period. Attachments are only read
	// while a turn runs, so an hourly sweep is frequent enough and does not
	// compete with uploads for disk.
	dshUploadCleanupInterval = time.Hour
)

// DshUploadRetention is the cleanup policy. A zero field disables that rule.
type DshUploadRetention struct {
	Age      time.Duration
	MaxFiles int
}

// StartDshUploadCleanup sweeps the upload directory hourly until ctx ends.
// The sweep is idempotent, so several MedRouter replicas running it at once
// only repeat work; nothing is deleted twice in a harmful way.
func StartDshUploadCleanup(ctx context.Context, cfg *config.Config) {
	upload, err := LoadDshUploadConfig(cfg)
	if err != nil {
		return
	}
	retention := DshUploadRetention{Age: cfg.DshUploadRetention, MaxFiles: cfg.DshUploadMaxFilesPerUser}
	if retention.Age <= 0 && retention.MaxFiles <= 0 {
		return
	}

	sweep := func() {
		if _, statErr := os.Stat(upload.Dir); statErr != nil {
			return
		}
		removed, err := CleanupDshUploads(upload.Dir, retention, time.Now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "dsh uploads: cleanup failed: %v\n", err)
			return
		}
		if removed > 0 {
			fmt.Fprintf(os.Stderr, "dsh uploads: removed %d expired files\n", removed)
		}
	}
	go func() {
		sweep()
		ticker := time.NewTicker(dshUploadCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweep()
			}
		}
	}()
}

// CleanupDshUploads removes expired files and caps each user directory. It
// returns the number of files deleted. Only regular files are touched, and
// only directories named after a numeric user id are visited.
func CleanupDshUploads(root string, retention DshUploadRetention, now time.Time) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		userID, parseErr := strconv.ParseInt(entry.Name(), 10, 64)
		if parseErr != nil || userID <= 0 {
			continue
		}
		count, err := cleanupDshUserDir(filepath.Join(root, entry.Name()), retention, now)
		removed += count
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func cleanupDshUserDir(dir string, retention DshUploadRetention, now time.Time) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	type fileInfo struct {
		name    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, fileInfo{name: entry.Name(), modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	removed := 0
	for index, file := range files {
		expired := retention.Age > 0 && now.Sub(file.modTime) > retention.Age
		overCap := retention.MaxFiles > 0 && index >= retention.MaxFiles
		if !expired && !overCap {
			continue
		}
		if err := os.Remove(filepath.Join(dir, file.name)); err == nil {
			removed++
		}
	}
	return removed, nil
}

// dshUserUploadDir is the directory convention shared with SaveDshAttachment
// and the cleanup sweep: one numeric directory per user.
func dshUserUploadDir(root string, userID int64) string {
	return filepath.Join(root, strconv.FormatInt(userID, 10))
}

package common

import (
	"sync"
	"time"
)

// ByteQuota is an in-memory byte budget per key over a rolling window. It is
// used to bound how much a single user may pull from the object store per
// day. Like RateLimiter it is meant for a single-process deployment; a restart
// resets the counters, and scaling out needs a shared backend.
//
// A limit of zero or less disables the quota.
type ByteQuota struct {
	mu      sync.Mutex
	entries map[string]*quotaEntry
	limit   int64
	window  time.Duration
	now     func() time.Time
}

type quotaEntry struct {
	used    int64
	resetAt time.Time
}

// NewByteQuota builds a quota. limit is the number of bytes allowed per key
// per window; window is the budget period.
func NewByteQuota(limit int64, window time.Duration) *ByteQuota {
	if window <= 0 {
		window = 24 * time.Hour
	}
	return &ByteQuota{
		entries: map[string]*quotaEntry{},
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

// Unlimited reports whether the quota is disabled.
func (q *ByteQuota) Unlimited() bool { return q == nil || q.limit <= 0 }

// Reserve accounts n bytes to key. It returns ok=false with the time until the
// window resets when the budget would be exceeded. A denied request does not
// consume anything.
func (q *ByteQuota) Reserve(key string, n int64) (ok bool, retryAfter time.Duration) {
	if q.Unlimited() {
		return true, 0
	}
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()

	// Opportunistic cleanup keeps the map bounded without a goroutine.
	if len(q.entries) > 10_000 {
		for k, e := range q.entries {
			if now.After(e.resetAt) {
				delete(q.entries, k)
			}
		}
	}

	entry, ok := q.entries[key]
	if !ok || now.After(entry.resetAt) {
		entry = &quotaEntry{resetAt: now.Add(q.window)}
		q.entries[key] = entry
	}
	if n > 0 && entry.used+n > q.limit {
		return false, time.Until(entry.resetAt)
	}
	if n > 0 {
		entry.used += n
	}
	return true, 0
}

// Used reports the bytes already accounted to key in the current window.
func (q *ByteQuota) Used(key string) int64 {
	if q.Unlimited() {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	entry, ok := q.entries[key]
	if !ok || q.now().After(entry.resetAt) {
		return 0
	}
	return entry.used
}

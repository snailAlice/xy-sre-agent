package alertmanager

import (
	"sync"
	"time"
)

type Deduper struct {
	mu   sync.Mutex
	ttl  time.Duration
	seen map[string]time.Time
}

func NewDeduper(ttl time.Duration) *Deduper {
	return &Deduper{ttl: ttl, seen: make(map[string]time.Time)}
}

func (d *Deduper) ShouldNotify(key string) bool {
	if key == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	d.cleanupLocked(now)
	last, ok := d.seen[key]
	if ok && now.Sub(last) < d.ttl {
		return false
	}
	d.seen[key] = now
	return true
}

func (d *Deduper) cleanupLocked(now time.Time) {
	for key, seenAt := range d.seen {
		if now.Sub(seenAt) > d.ttl*2 {
			delete(d.seen, key)
		}
	}
}

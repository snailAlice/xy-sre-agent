package memory

import (
	"context"
	"sync"
	"time"
)

type Message struct {
	Role      string
	Content   string
	CreatedAt time.Time
}

type Store interface {
	Append(ctx context.Context, sessionID string, msg Message) error
	List(ctx context.Context, sessionID string) ([]Message, error)
}

type InMemoryStore struct {
	mu         sync.RWMutex
	ttl        time.Duration
	maxHistory int
	sessions   map[string][]Message
}

func NewInMemoryStore(ttl time.Duration, maxHistory int) *InMemoryStore {
	return &InMemoryStore{
		ttl:        ttl,
		maxHistory: maxHistory,
		sessions:   make(map[string][]Message),
	}
}

func (s *InMemoryStore) Append(_ context.Context, sessionID string, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg.CreatedAt = time.Now()
	s.cleanupLocked(msg.CreatedAt)
	history := append(s.sessions[sessionID], msg)
	if len(history) > s.maxHistory {
		history = history[len(history)-s.maxHistory:]
	}
	s.sessions[sessionID] = history
	return nil
}

func (s *InMemoryStore) List(_ context.Context, sessionID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var out []Message
	for _, msg := range s.sessions[sessionID] {
		if now.Sub(msg.CreatedAt) <= s.ttl {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (s *InMemoryStore) cleanupLocked(now time.Time) {
	for sessionID, history := range s.sessions {
		kept := history[:0]
		for _, msg := range history {
			if now.Sub(msg.CreatedAt) <= s.ttl {
				kept = append(kept, msg)
			}
		}
		if len(kept) == 0 {
			delete(s.sessions, sessionID)
			continue
		}
		s.sessions[sessionID] = kept
	}
}

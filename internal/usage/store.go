// Package usage persists per-profile tool call counts across daemon
// restarts, so lazy-loading profiles can register their most-frequently-used
// tools directly again after a restart instead of falling back to plain
// catalog order every time.
package usage

import (
	"encoding/json"
	"os"
	"sync"
)

// Store tracks call counts per (profile, exposed tool name), persisted as a
// single JSON file. All methods are safe for concurrent use.
type Store struct {
	path string

	mu     sync.Mutex
	counts map[string]map[string]int64 // profile -> exposedName -> count
}

// Load reads path if it exists (an absent file is not an error - it just
// starts empty) and returns a Store that persists back to it on every Record.
func Load(path string) (*Store, error) {
	s := &Store{path: path, counts: make(map[string]map[string]int64)}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.counts); err != nil {
		return nil, err
	}
	return s, nil
}

// Record increments the call count for (profile, exposedName) and persists
// the updated totals immediately - call volume here is interactive (an IDE
// making tool calls), not hot-loop traffic, so a synchronous write keeps
// counts crash-safe without needing a debounce/flush path.
func (s *Store) Record(profile, exposedName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.counts[profile] == nil {
		s.counts[profile] = make(map[string]int64)
	}
	s.counts[profile][exposedName]++

	// Best-effort: a failed write here shouldn't break the calling tool
	// dispatch, so errors are swallowed rather than surfaced to callers.
	_ = s.save()
}

// Counts returns a snapshot of the call counts recorded for profile (empty
// map if none yet).
func (s *Store) Counts(profile string) map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string]int64, len(s.counts[profile]))
	for k, v := range s.counts[profile] {
		out[k] = v
	}
	return out
}

// save writes the current counts to s.path via write-temp-then-rename, so a
// crash mid-write can't corrupt the previous good copy. Caller must hold s.mu.
func (s *Store) save() error {
	b, err := json.Marshal(s.counts)
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

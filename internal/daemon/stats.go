package daemon

import (
	"sync"
	"time"
)

// CallRecord is one entry in the daemon's recent-calls ring buffer.
type CallRecord struct {
	Timestamp   time.Time `json:"timestamp"`
	Profile     string    `json:"profile"`
	ExposedTool string    `json:"exposed_tool"`
	RulesFired  []string  `json:"rules_fired,omitempty"`
	OK          bool      `json:"ok"`
	Error       string    `json:"error,omitempty"`
}

// statsRing is a fixed-capacity ring buffer of recent call records.
type statsRing struct {
	mu      sync.Mutex
	entries []CallRecord
	next    int
	full    bool
}

func newStatsRing(capacity int) *statsRing {
	return &statsRing{entries: make([]CallRecord, capacity)}
}

// record implements gateway.Recorder.
func (s *statsRing) record(clientProfile, exposedName string, rulesFired []string, isError bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := CallRecord{
		Timestamp:   time.Now(),
		Profile:     clientProfile,
		ExposedTool: exposedName,
		RulesFired:  rulesFired,
		OK:          !isError && err == nil,
	}
	if err != nil {
		rec.Error = err.Error()
	}

	s.entries[s.next] = rec
	s.next = (s.next + 1) % len(s.entries)
	if s.next == 0 {
		s.full = true
	}
}

// recent returns up to limit records, most recent first. limit <= 0 means "all".
func (s *statsRing) recent(limit int) []CallRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	var all []CallRecord
	if s.full {
		all = append(all, s.entries[s.next:]...)
		all = append(all, s.entries[:s.next]...)
	} else {
		all = append(all, s.entries[:s.next]...)
	}

	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}

	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return all
}

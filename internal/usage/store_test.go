package usage

import (
	"path/filepath"
	"testing"
)

func TestRecordAndCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s.Record("vscode", "kite__get_holdings")
	s.Record("vscode", "kite__get_holdings")
	s.Record("vscode", "kite__place_order")
	s.Record("cursor", "colab__open_colab_browser_connection")

	got := s.Counts("vscode")
	if got["kite__get_holdings"] != 2 {
		t.Errorf("kite__get_holdings count = %d, want 2", got["kite__get_holdings"])
	}
	if got["kite__place_order"] != 1 {
		t.Errorf("kite__place_order count = %d, want 1", got["kite__place_order"])
	}
	if len(s.Counts("cursor")) != 1 {
		t.Errorf("cursor counts should be independent of vscode's")
	}
	if len(s.Counts("nonexistent")) != 0 {
		t.Errorf("unknown profile should return an empty map, not nil-panic")
	}
}

func TestLoadPersistsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")

	s1, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s1.Record("vscode", "kite__get_holdings")
	s1.Record("vscode", "kite__get_holdings")
	s1.Record("vscode", "kite__place_order")

	s2, err := Load(path)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	got := s2.Counts("vscode")
	if got["kite__get_holdings"] != 2 || got["kite__place_order"] != 1 {
		t.Errorf("counts did not survive a reload: %+v", got)
	}
}

func TestLoadMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load of missing file should not error: %v", err)
	}
	if len(s.Counts("vscode")) != 0 {
		t.Errorf("expected empty counts for a fresh store")
	}
}

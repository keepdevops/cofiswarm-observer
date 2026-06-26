package alertstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keepdevops/cofiswarm-observer/internal/bustail"
)

func TestRoundTripSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "alerts.json") // dir created by persist
	s, err := New(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(bustail.Alert{Message: "kv gate denied", At: "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(bustail.Alert{Message: "model evicted", At: "t2"}); err != nil {
		t.Fatal(err)
	}

	// Reopen: a fresh Store must load both alerts from disk.
	s2, err := New(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Existing()
	if len(got) != 2 || got[0].Message != "kv gate denied" || got[1].Message != "model evicted" {
		t.Fatalf("loaded alerts = %+v, want the two persisted", got)
	}
}

func TestRetentionCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	s, err := New(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"a", "b", "c", "d", "e"} {
		if err := s.Append(bustail.Alert{Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.Existing()
	if len(got) != 3 || got[0].Message != "c" || got[2].Message != "e" {
		t.Fatalf("retention = %+v, want last 3 [c d e]", got)
	}
}

func TestClearPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	s, _ := New(path, 0)
	_ = s.Append(bustail.Alert{Message: "x"})
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if reopened, _ := New(path, 0); len(reopened.Existing()) != 0 {
		t.Fatalf("alerts survived Clear: %+v", reopened.Existing())
	}
}

func TestMissingFileIsFreshStart(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "nope.json"), 0)
	if err != nil {
		t.Fatalf("missing file should be a fresh start, got %v", err)
	}
	if len(s.Existing()) != 0 {
		t.Fatal("fresh store should be empty")
	}
}

func TestCorruptFileIsLoudError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path, 0); err == nil {
		t.Fatal("corrupt alert file should fail loudly, not silently start empty")
	}
}

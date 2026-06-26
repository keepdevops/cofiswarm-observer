package bustail

import "testing"

type fakeStore struct {
	appended []Alert
	cleared  int
}

func (f *fakeStore) Append(a Alert) error { f.appended = append(f.appended, a); return nil }
func (f *fakeStore) Clear() error         { f.cleared++; return nil }

// An attached store must receive every alert and the clear.
func TestAlertsPersistThroughStore(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	fs := &fakeStore{}
	tl.SetStore(fs)

	tl.handle(`{"topic":"swarm.observer.alert","payload":{"message":"kv gate denied"}}`)
	if len(fs.appended) != 1 || fs.appended[0].Message != "kv gate denied" {
		t.Fatalf("store.Append not invoked on alert: %+v", fs.appended)
	}

	if n := tl.ClearAlerts(); n != 1 || fs.cleared != 1 {
		t.Fatalf("ClearAlerts n=%d store.cleared=%d, want 1/1", n, fs.cleared)
	}
}

// SeedAlerts loads persisted history into the live ring at startup.
func TestSeedAlertsPopulatesRing(t *testing.T) {
	tl := New("http://unused")
	tl.SeedAlerts([]Alert{{Message: "old1"}, {Message: "old2"}})
	if _, a := tl.Snapshot(); len(a) != 2 || a[0].Message != "old1" {
		t.Fatalf("seeded ring = %+v, want [old1 old2]", a)
	}
}

// SeedAlerts must respect the 100-entry ring cap, keeping the most recent.
func TestSeedAlertsCapsToRing(t *testing.T) {
	tl := New("http://unused")
	seed := make([]Alert, 150)
	for i := range seed {
		seed[i] = Alert{Message: string(rune('A' + i%26))}
	}
	tl.SeedAlerts(seed)
	_, a := tl.Snapshot()
	if len(a) != 100 {
		t.Fatalf("ring after seeding 150 = %d, want 100 (most recent)", len(a))
	}
}

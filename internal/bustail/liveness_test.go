package bustail

import (
	"testing"
	"time"
)

// An announce/presence records lastSeen so the reaper can later detect silence.
func TestAnnounceRecordsLastSeen(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	tl.handle(`{"topic":"swarm.observer.announce","payload":{"component_id":"kvpool","info":{"name":"kvpool"}}}`)
	if tl.lastSeen["kvpool"].IsZero() {
		t.Fatal("lastSeen not recorded for announced component")
	}
}

// reap expires only components unseen past the TTL, leaving fresh ones online.
func TestReapExpiresStaleNotFresh(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	now := time.Unix(1_700_000_000, 0)
	for _, cid := range []string{"fresh", "stale"} {
		tl.roster[cid] = Presence{ComponentID: cid, Status: "online"}
	}
	tl.lastSeen["fresh"] = now.Add(-time.Second) // seen just now
	tl.lastSeen["stale"] = now.Add(-2 * tl.ttl)  // long gone

	expired := tl.reap(now)
	if len(expired) != 1 || expired[0] != "stale" {
		t.Fatalf("reap = %v, want [stale]", expired)
	}
	if _, ok := tl.roster["fresh"]; !ok {
		t.Fatal("fresh component wrongly reaped")
	}
	if _, ok := tl.roster["stale"]; ok {
		t.Fatal("stale component not reaped from roster")
	}
	if _, ok := tl.lastSeen["stale"]; ok {
		t.Fatal("stale lastSeen not cleared")
	}
}

// Liveness is skipped (RunLiveness returns immediately) when the TTL is non-positive.
func TestLivenessDisabledWhenTTLZero(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	tl.SetLiveness(0, 15*time.Second)
	done := make(chan struct{})
	go func() { tl.RunLiveness(nil); close(done) }() // returns at once: no ctx needed
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunLiveness did not return with liveness disabled")
	}
	if len(published) != 0 {
		t.Fatalf("disabled liveness still published: %+v", published)
	}
}

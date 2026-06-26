package bustail

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// bridgeStub captures presence republished to /v1/publish and 202s like the real bridge.
type publishedEvent struct {
	Topic   string         `json:"topic"`
	Payload map[string]any `json:"payload"`
}

func newBridgeStub(t *testing.T, sink *[]publishedEvent) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev publishedEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			t.Errorf("bridge stub: bad publish body: %v", err)
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		*sink = append(*sink, ev)
		w.WriteHeader(http.StatusAccepted)
	}))
}

// announce (Pattern A) must surface the component as online and republish presence.
func TestTranslateAnnounceToPresence(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	tl.handle(`{"topic":"swarm.observer.announce","payload":{"component_id":"kvpool","status":"online","info":{"name":"kvpool","engine":"kv"}}}`)

	online, _ := tl.Snapshot()
	if len(online) != 1 || online[0].ComponentID != "kvpool" || online[0].Status != "online" {
		t.Fatalf("roster = %+v, want kvpool online", online)
	}
	if online[0].Model != "kvpool" {
		t.Fatalf("model = %q, want kvpool (carried from announce info.name)", online[0].Model)
	}
	if len(published) != 1 || published[0].Topic != presenceTopic {
		t.Fatalf("republished = %+v, want one %s event", published, presenceTopic)
	}
	if published[0].Payload["status"] != "online" {
		t.Fatalf("republished status = %v, want online", published[0].Payload["status"])
	}
}

func TestClearAlertsEmptiesAlertList(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	tl.handle(`{"topic":"swarm.observer.alert","payload":{"message":"mode flat execute unavailable"}}`)
	tl.handle(`{"topic":"swarm.observer.alert","payload":{"message":"dispatch timeout"}}`)
	if _, alerts := tl.Snapshot(); len(alerts) != 2 {
		t.Fatalf("alerts before clear = %d, want 2", len(alerts))
	}
	if n := tl.ClearAlerts(); n != 2 {
		t.Fatalf("ClearAlerts returned %d, want 2", n)
	}
	if _, alerts := tl.Snapshot(); len(alerts) != 0 {
		t.Fatalf("alerts after clear = %d, want 0", len(alerts))
	}
}

// goodbye must flip the component offline (removed from the roster) and republish offline.
func TestTranslateGoodbyeToOffline(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	tl.handle(`{"topic":"swarm.observer.announce","payload":{"component_id":"launcher","info":{"name":"launcher"}}}`)
	tl.handle(`{"topic":"swarm.observer.goodbye","payload":{"component_id":"launcher","reason":"shutdown"}}`)

	online, _ := tl.Snapshot()
	if len(online) != 0 {
		t.Fatalf("roster = %+v, want empty after goodbye", online)
	}
	if len(published) != 2 || published[1].Payload["status"] != "offline" {
		t.Fatalf("republished = %+v, want second event offline", published)
	}
}

// A direct presence event must NOT be re-translated (no republish), so there is no loop.
func TestPresenceIsNotRepublished(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	tl.handle(`{"topic":"swarm.observer.presence","payload":{"component_id":"dispatch","status":"online","info":{"name":"dispatch"}}}`)

	online, _ := tl.Snapshot()
	if len(online) != 1 || online[0].ComponentID != "dispatch" {
		t.Fatalf("roster = %+v, want dispatch online", online)
	}
	if len(published) != 0 {
		t.Fatalf("republished = %+v, want none (presence must not loop)", published)
	}
}

// An announce missing component_id is ignored, not translated.
func TestAnnounceWithoutComponentIDIgnored(t *testing.T) {
	var published []publishedEvent
	srv := newBridgeStub(t, &published)
	defer srv.Close()

	tl := New(srv.URL)
	tl.handle(`{"topic":"swarm.observer.announce","payload":{"info":{"name":"ghost"}}}`)

	online, _ := tl.Snapshot()
	if len(online) != 0 || len(published) != 0 {
		t.Fatalf("ignored announce leaked: roster=%+v published=%+v", online, published)
	}
}

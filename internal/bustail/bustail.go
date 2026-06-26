// Package bustail makes cofiswarm-observer a reactive consumer of the observer bus.
//
// It connects to cofiswarm-zmq-bridge's SSE stream (/v1/stream) — so it needs no NATS
// client, only HTTP — and keeps a live view of bus state: which components are online
// (from swarm.observer.presence) and recent alerts (from swarm.observer.alert). Events
// are also logged, fitting the observer's telemetry-sink role. Reconnects with backoff.
//
// It is also the bus's announce/goodbye -> presence translator: Pattern A components
// (launcher, slot-manager, kvpool) only publish swarm.observer.announce/goodbye, never
// presence, so without this they are invisible to every observer. bustail normalizes those
// into swarm.observer.presence — applied locally and republished to the bus via the bridge.
package bustail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const presenceTopic = "swarm.observer.presence"

type Presence struct {
	ComponentID string `json:"component_id"`
	Status      string `json:"status"`
	Model       string `json:"model,omitempty"`
}

type Alert struct {
	Message string `json:"message"`
	At      string `json:"at"`
}

// Tailer subscribes to the bridge SSE stream and tracks online components + recent alerts.
type Tailer struct {
	url        string
	bridge     string // bridge base URL, used to republish translated presence to /v1/publish
	client     *http.Client
	pub        *http.Client
	mu         sync.RWMutex
	roster     map[string]Presence
	alerts     []Alert
	lastSeen   map[string]time.Time // component_id -> last time seen online, for TTL reaping
	ttl        time.Duration        // expire a component unseen this long (<=0 disables liveness)
	helloEvery time.Duration        // how often to broadcast hello (<=0 disables liveness)
}

// New builds a tailer for the bridge base URL (e.g. http://host:5555). It consumes the
// /v1/stream SSE feed and republishes translated presence to /v1/publish. Liveness defaults
// to a 45s TTL with a 15s hello interval; override with SetLiveness before RunLiveness.
func New(bridgeBase string) *Tailer {
	base := strings.TrimRight(bridgeBase, "/")
	return &Tailer{
		url:        base + "/v1/stream",
		bridge:     base,
		client:     &http.Client{},                         // no timeout: SSE is long-lived
		pub:        &http.Client{Timeout: 5 * time.Second}, // short timeout: publish is fire-and-forget
		roster:     map[string]Presence{},
		alerts:     []Alert{},
		lastSeen:   map[string]time.Time{},
		ttl:        45 * time.Second,
		helloEvery: 15 * time.Second,
	}
}

// Run consumes the stream until ctx is cancelled, reconnecting with capped backoff.
func (t *Tailer) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := t.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("bustail: stream error: %v (retry in %s)", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (t *Tailer) consume(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	log.Printf("bustail: connected to %s", t.url)

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			t.handle(strings.TrimSpace(line[len("data:"):]))
		}
	}
	return sc.Err()
}

// handle parses one SSE {topic, payload} envelope and routes it through dispatch.
func (t *Tailer) handle(data string) {
	var ev struct {
		Topic   string         `json:"topic"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		log.Printf("bustail: bad event: %v", err)
		return
	}
	t.dispatch(ev.Topic, ev.Payload)
}

// dispatch routes one decoded bus event by topic. It is the shared core for both ingest
// sources — the SSE tail (handle) and the ZMQ egress subscription (RunZmq).
func (t *Tailer) dispatch(topic string, payload map[string]any) {
	log.Printf("bustail: %s %v", topic, payload)
	switch {
	case strings.HasSuffix(topic, ".announce"):
		t.translateAnnounce(payload)
	case strings.HasSuffix(topic, ".goodbye"):
		t.translateGoodbye(payload)
	case strings.HasSuffix(topic, ".presence"):
		t.applyPresence(payload)
	case strings.HasSuffix(topic, ".alert"):
		t.applyAlert(payload)
	}
}

// translateAnnounce converts a Pattern A component's announce into an "online" presence
// event. Those components (launcher, slot-manager, kvpool) publish swarm.observer.announce
// but never presence, so without this they never appear in any observer's roster. The
// normalized presence is applied locally and republished to the bus for peers and the UI.
func (t *Tailer) translateAnnounce(p map[string]any) {
	cid, _ := p["component_id"].(string)
	if cid == "" {
		log.Printf("bustail: announce without component_id, ignored")
		return
	}
	presence := map[string]any{"component_id": cid, "status": "online", "info": p["info"]}
	t.applyPresence(presence)
	t.publishPresence(presence)
}

// translateGoodbye converts a graceful goodbye into an "offline" presence event, so a
// component that exits cleanly is removed from the roster instead of lingering online.
func (t *Tailer) translateGoodbye(p map[string]any) {
	cid, _ := p["component_id"].(string)
	if cid == "" {
		log.Printf("bustail: goodbye without component_id, ignored")
		return
	}
	presence := map[string]any{"component_id": cid, "status": "offline"}
	t.applyPresence(presence)
	t.publishPresence(presence)
}

// publishPresence republishes a normalized presence event to the bus so peers and the UI
// see it. The local roster was already updated by the caller.
func (t *Tailer) publishPresence(payload map[string]any) {
	t.publish(presenceTopic, payload)
}

// publish posts a {topic, payload} envelope to the bridge's HTTP publish endpoint. Failures
// are logged (never silent) but don't block tailing. Receiving our own message back is a
// no-op: handle() only translates .announce/.goodbye, never .presence/.hello (no loop).
func (t *Tailer) publish(topic string, payload map[string]any) {
	// ZMQ-only ingest (no bridge HTTP base): read-side works, but presence/hello can't be
	// republished. Skip loudly rather than POST to a bogus URL.
	if t.bridge == "" {
		log.Printf("bustail: no bridge URL; skipping republish of %s", topic)
		return
	}
	body, err := json.Marshal(map[string]any{"topic": topic, "payload": payload})
	if err != nil {
		log.Printf("bustail: marshal %s: %v", topic, err)
		return
	}
	resp, err := t.pub.Post(t.bridge+"/v1/publish", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("bustail: publish %s: %v", topic, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		log.Printf("bustail: publish %s: bridge returned %s", topic, resp.Status)
	}
}

func (t *Tailer) applyPresence(p map[string]any) {
	cid, _ := p["component_id"].(string)
	if cid == "" {
		return
	}
	status, _ := p["status"].(string)
	model := ""
	if info, ok := p["info"].(map[string]any); ok {
		model, _ = info["name"].(string)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if status == "offline" {
		delete(t.roster, cid)
		delete(t.lastSeen, cid)
	} else {
		t.roster[cid] = Presence{ComponentID: cid, Status: status, Model: model}
		t.lastSeen[cid] = time.Now()
	}
}

func (t *Tailer) applyAlert(p map[string]any) {
	msg, _ := p["message"].(string)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alerts = append(t.alerts, Alert{Message: msg, At: time.Now().UTC().Format(time.RFC3339)})
	if len(t.alerts) > 100 {
		t.alerts = t.alerts[len(t.alerts)-100:]
	}
}

// Snapshot returns the current online components and recent alerts.
func (t *Tailer) Snapshot() ([]Presence, []Alert) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	online := make([]Presence, 0, len(t.roster))
	for _, p := range t.roster {
		online = append(online, p)
	}
	alerts := append([]Alert(nil), t.alerts...)
	return online, alerts
}

// ClearAlerts drops all recorded alerts and returns how many were cleared. Alerts have no TTL
// (applyAlert only appends), so a resolved one-shot alert otherwise lingers until 100 newer
// alerts push it off or the process restarts — this lets the dashboard dismiss stale entries.
func (t *Tailer) ClearAlerts() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.alerts)
	t.alerts = nil
	return n
}

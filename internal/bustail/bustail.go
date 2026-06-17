// Package bustail makes cofiswarm-observer a reactive consumer of the observer bus.
//
// It connects to cofiswarm-zmq-bridge's SSE stream (/v1/stream) — so it needs no NATS
// client, only HTTP — and keeps a live view of bus state: which components are online
// (from swarm.observer.presence) and recent alerts (from swarm.observer.alert). Events
// are also logged, fitting the observer's telemetry-sink role. Reconnects with backoff.
package bustail

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

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
	url    string
	client *http.Client
	mu     sync.RWMutex
	roster map[string]Presence
	alerts []Alert
}

// New builds a tailer for the given SSE stream URL (e.g. http://host:5555/v1/stream).
func New(streamURL string) *Tailer {
	return &Tailer{
		url:    streamURL,
		client: &http.Client{}, // no timeout: SSE is long-lived
		roster: map[string]Presence{},
		alerts: []Alert{},
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

func (t *Tailer) handle(data string) {
	var ev struct {
		Topic   string         `json:"topic"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		log.Printf("bustail: bad event: %v", err)
		return
	}
	log.Printf("bustail: %s %v", ev.Topic, ev.Payload)
	switch {
	case strings.HasSuffix(ev.Topic, ".presence"):
		t.applyPresence(ev.Payload)
	case strings.HasSuffix(ev.Topic, ".alert"):
		t.applyAlert(ev.Payload)
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
	} else {
		t.roster[cid] = Presence{ComponentID: cid, Status: status, Model: model}
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

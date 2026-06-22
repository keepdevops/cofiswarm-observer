package bustail

import (
	"context"
	"log"
	"time"
)

const helloTopic = "swarm.observer.hello"

// SetLiveness configures crash detection. ttl is how long a component may go unseen before
// it is reaped; helloEvery is how often the observer broadcasts hello to prompt a re-announce.
// A non-positive ttl or interval disables liveness. Call before RunLiveness.
func (t *Tailer) SetLiveness(ttl, helloEvery time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ttl = ttl
	t.helloEvery = helloEvery
}

// RunLiveness drives crash detection until ctx is cancelled. A clean exit publishes goodbye
// (translated to offline), but a crash leaves no goodbye — so the observer periodically
// broadcasts swarm.observer.hello, to which live components re-announce, and reaps any
// component not seen within the TTL. Both reporting patterns already re-announce on hello,
// so a silent component is a dead one. No-op when liveness is disabled.
func (t *Tailer) RunLiveness(ctx context.Context) {
	t.mu.RLock()
	ttl, every := t.ttl, t.helloEvery
	t.mu.RUnlock()
	if ttl <= 0 || every <= 0 {
		log.Printf("bustail: liveness disabled (ttl=%s interval=%s)", ttl, every)
		return
	}
	log.Printf("bustail: liveness on (ttl=%s, hello every %s)", ttl, every)

	ticker := time.NewTicker(every)
	defer ticker.Stop()
	t.publishHello() // prompt an immediate roster refresh on startup (also covers late joins)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			t.publishHello()
			for _, cid := range t.reap(now) {
				log.Printf("bustail: %s unseen for >%s -> reaping (offline)", cid, ttl)
				t.publishPresence(map[string]any{"component_id": cid, "status": "offline"})
			}
		}
	}
}

// reap removes every component not seen within the TTL as of now, returning their ids so the
// caller can announce them offline. Deletion happens under lock; the network publish does not.
func (t *Tailer) reap(now time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var expired []string
	for cid, seen := range t.lastSeen {
		if now.Sub(seen) > t.ttl {
			expired = append(expired, cid)
			delete(t.roster, cid)
			delete(t.lastSeen, cid)
		}
	}
	return expired
}

// publishHello asks live components to re-announce. Pattern A subscribes to the hello NATS
// subject; Pattern B streams it over SSE. The payload is informational only.
func (t *Tailer) publishHello() {
	t.publish(helloTopic, map[string]any{"from": "observer"})
}

package bustail

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-zeromq/zmq4"
)

// End-to-end over a real ZMQ socket: a stand-in egress PUB (the bridge) publishes a
// presence frame and the observer's RunZmq ingest surfaces it in the roster.
func TestRunZmqIngestsPresence(t *testing.T) {
	const addr = "tcp://127.0.0.1:55731"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pub := zmq4.NewPub(ctx)
	if err := pub.Listen(addr); err != nil {
		t.Skipf("cannot bind egress stub on %s: %v", addr, err)
	}
	defer pub.Close()

	tl := New("") // ZMQ-only: no bridge, read-side only
	go tl.RunZmq(ctx, addr, "swarm.")

	payload, _ := json.Marshal(map[string]any{
		"component_id": "dispatch",
		"status":       "online",
		"info":         map[string]any{"name": "dispatch"},
	})
	frame := zmq4.NewMsgFrom([]byte("swarm.observer.presence"), payload)

	// PUB->SUB slow joiner: resend until the subscription propagates and the roster fills.
	deadline := time.After(3 * time.Second)
	for {
		if err := pub.Send(frame); err != nil {
			t.Fatalf("send: %v", err)
		}
		online, _ := tl.Snapshot()
		if len(online) == 1 && online[0].ComponentID == "dispatch" && online[0].Status == "online" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout: roster never filled (got %+v)", online)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

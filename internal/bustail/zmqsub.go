package bustail

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/go-zeromq/zmq4"
)

// RunZmq subscribes to the zmq-bridge egress PUB wire (e.g. tcp://127.0.0.1:5557) and feeds
// every matching frame into the same dispatch path as the SSE tail. filter is the SUB
// prefix ("" = all, normally "swarm."). It runs until ctx is cancelled or Recv fails;
// callers wanting reconnect use RunZmqForever. Multipart frames are [topic, json-payload].
func (t *Tailer) RunZmq(ctx context.Context, addr, filter string) error {
	sub := zmq4.NewSub(ctx)
	if err := sub.Dial(addr); err != nil {
		return err
	}
	defer sub.Close()
	if err := sub.SetOption(zmq4.OptionSubscribe, filter); err != nil {
		return err
	}
	log.Printf("bustail: zmq subscribed to %s (filter %q)", addr, filter)
	for ctx.Err() == nil {
		msg, err := sub.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if len(msg.Frames) < 2 {
			log.Printf("bustail: zmq dropping malformed message (%d frames)", len(msg.Frames))
			continue
		}
		topic := string(msg.Frames[0])
		var payload map[string]any
		if err := json.Unmarshal(msg.Frames[1], &payload); err != nil {
			log.Printf("bustail: zmq %s: bad json: %v", topic, err)
			continue
		}
		t.dispatch(topic, payload)
	}
	return nil
}

// RunZmqForever runs RunZmq until ctx is cancelled, reconnecting with capped backoff so a
// bridge restart doesn't permanently sever the observer (mirrors Run for the SSE tail).
func (t *Tailer) RunZmqForever(ctx context.Context, addr, filter string) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := t.RunZmq(ctx, addr, filter)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("bustail: zmq error: %v (retry in %s)", err, backoff)
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

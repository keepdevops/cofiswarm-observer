package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/keepdevops/cofiswarm-observer/internal/alertstore"
	"github.com/keepdevops/cofiswarm-observer/internal/bustail"
	"github.com/keepdevops/cofiswarm-observer/internal/httpapi"
)

func main() {
	addr := flag.String("listen", ":8016", "listen address")
	flag.Parse()
	pd, ld := httpapi.DefaultDirs()

	// Optional: tail the bus for the live view at /v1/observed (default-off). The read path
	// is selected by env:
	//   COFISWARM_ZMQ_EGRESS_ADDR=tcp://127.0.0.1:5557  subscribe to the bridge's ZMQ egress
	//   COFISWARM_BRIDGE_URL=http://127.0.0.1:5555       tail the bridge's SSE stream
	// The bridge URL also carries presence/hello republished over /v1/publish; with only the
	// ZMQ address set, ingest still works read-only (republish is skipped).
	base := os.Getenv("COFISWARM_BRIDGE_URL")
	zmqAddr := os.Getenv("COFISWARM_ZMQ_EGRESS_ADDR")
	var tail *bustail.Tailer
	if base != "" || zmqAddr != "" {
		tail = bustail.New(base)
		// Crash detection: broadcast hello and reap components that go silent. Tune with
		// COFISWARM_PRESENCE_TTL / COFISWARM_HELLO_INTERVAL; set TTL to 0 to disable.
		tail.SetLiveness(
			durationEnv("COFISWARM_PRESENCE_TTL", 45*time.Second),
			durationEnv("COFISWARM_HELLO_INTERVAL", 15*time.Second),
		)
		// Persist alerts across restarts when a state root is configured (the deploy
		// sets COFISWARM_VAR_LIB). Off in bare dev; override the path with
		// COFISWARM_OBSERVER_ALERTS (or "off" to disable). The live roster is not
		// persisted — it self-heals from announce/hello on startup.
		if path := alertsPath(); path != "" {
			store, err := alertstore.New(path, intEnv("COFISWARM_OBSERVER_ALERTS_MAX", alertstore.DefaultMax))
			if err != nil {
				log.Printf("observer: alert persistence at %s disabled: %v", path, err)
			} else {
				existing := store.Existing()
				tail.SeedAlerts(existing)
				tail.SetStore(store)
				log.Printf("observer: persisting alerts to %s (%d loaded)", path, len(existing))
			}
		}
		ctx := context.Background()
		if zmqAddr != "" {
			filter := os.Getenv("COFISWARM_ZMQ_FILTER")
			if filter == "" {
				filter = "swarm."
			}
			go tail.RunZmqForever(ctx, zmqAddr, filter)
			log.Printf("observer subscribing to bus egress %s (republish via %q)", zmqAddr, base)
		} else {
			go tail.Run(ctx)
			log.Printf("observer tailing bus via %s", base)
		}
		go tail.RunLiveness(ctx)
	}

	log.Printf("observer listening on %s plugins=%s logs=%s", *addr, pd, ld)
	log.Fatal(http.ListenAndServe(*addr, httpapi.New(pd, ld, tail).Handler()))
}

// alertsPath returns where to persist alerts: COFISWARM_OBSERVER_ALERTS wins ("off"
// disables); otherwise derive from COFISWARM_VAR_LIB. Empty means no persistence.
func alertsPath() string {
	if p := os.Getenv("COFISWARM_OBSERVER_ALERTS"); p != "" {
		if p == "off" {
			return ""
		}
		return p
	}
	lib := os.Getenv("COFISWARM_VAR_LIB")
	if lib == "" {
		return ""
	}
	return filepath.Join(lib, "cofiswarm", "observer", "alerts.json")
}

// intEnv reads an int from env, falling back to def (malformed values are logged, not silent).
func intEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("observer: bad %s=%q (%v); using %d", key, v, err, def)
		return def
	}
	return n
}

// durationEnv reads a Go duration from env, falling back to def. A malformed value is logged
// loudly and the default is used (never silently ignored).
func durationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("observer: bad %s=%q (%v); using %s", key, v, err, def)
		return def
	}
	return d
}

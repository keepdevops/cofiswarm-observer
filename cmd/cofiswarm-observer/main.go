package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/keepdevops/cofiswarm-observer/internal/bustail"
	"github.com/keepdevops/cofiswarm-observer/internal/httpapi"
)

func main() {
	addr := flag.String("listen", ":8016", "listen address")
	flag.Parse()
	pd, ld := httpapi.DefaultDirs()

	// Optional: tail the bus via the zmq-bridge SSE stream (default-off).
	// COFISWARM_BRIDGE_URL=http://127.0.0.1:5555 enables the live bus view at /v1/observed.
	var tail *bustail.Tailer
	if base := os.Getenv("COFISWARM_BRIDGE_URL"); base != "" {
		tail = bustail.New(strings.TrimRight(base, "/") + "/v1/stream")
		go tail.Run(context.Background())
		log.Printf("observer tailing bus via %s", base)
	}

	log.Printf("observer listening on %s plugins=%s logs=%s", *addr, pd, ld)
	log.Fatal(http.ListenAndServe(*addr, httpapi.New(pd, ld, tail).Handler()))
}

package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/keepdevops/cofiswarm-observer/internal/httpapi"
)

func main() {
	addr := flag.String("listen", ":8016", "listen address")
	flag.Parse()
	pd, ld := httpapi.DefaultDirs()
	log.Printf("observer listening on %s plugins=%s logs=%s", *addr, pd, ld)
	log.Fatal(http.ListenAndServe(*addr, httpapi.New(pd, ld).Handler()))
}

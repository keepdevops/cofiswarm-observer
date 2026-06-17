package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/keepdevops/cofiswarm-observer/internal/bustail"
	"github.com/keepdevops/cofiswarm-observer/internal/metrics"
)

type Server struct {
	pluginsDir string
	logsDir    string
	tail       *bustail.Tailer // optional: live bus view (nil when disabled)
}

func New(pluginsDir, logsDir string, tail *bustail.Tailer) *Server {
	return &Server{pluginsDir: pluginsDir, logsDir: logsDir, tail: tail}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/plugins", func(w http.ResponseWriter, _ *http.Request) {
		entries, _ := filepath.Glob(filepath.Join(s.pluginsDir, "*.yaml"))
		_ = json.NewEncoder(w).Encode(map[string]any{"plugins": entries, "dir": s.pluginsDir})
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(metrics.RenderPrometheus()))
	})
	// Live bus view, fed by the bridge SSE stream (empty when the tail is disabled).
	mux.HandleFunc("/v1/observed", func(w http.ResponseWriter, _ *http.Request) {
		var online []bustail.Presence
		var alerts []bustail.Alert
		if s.tail != nil {
			online, alerts = s.tail.Snapshot()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": s.tail != nil, "online": online, "alerts": alerts,
		})
	})
	return mux
}

func DefaultDirs() (plugins, logs string) {
	lib := os.Getenv("COFISWARM_VAR_LIB")
	if lib == "" {
		lib = "/var/lib"
	}
	logRoot := os.Getenv("COFISWARM_VAR_LOG")
	if logRoot == "" {
		logRoot = "/var/log/cofiswarm"
	}
	return filepath.Join(lib, "cofiswarm", "observer", "plugins"),
		filepath.Join(logRoot, "agent_logs")
}

package httpapi

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/keepdevops/cofiswarm-observer/internal/bustail"
	"github.com/keepdevops/cofiswarm-observer/internal/dockerstat"
	"github.com/keepdevops/cofiswarm-observer/internal/metrics"
	"github.com/keepdevops/cofiswarm-observer/internal/sysstat"
)

//go:embed index.html
var indexHTML []byte

type Server struct {
	pluginsDir string
	logsDir    string
	tail       *bustail.Tailer // optional: live bus view (nil when disabled)
	stat       *sysstat.Sampler
	docker     *dockerstat.Client
}

func New(pluginsDir, logsDir string, tail *bustail.Tailer) *Server {
	// Docker widget scopes to cofiswarm-* containers; fails soft if the socket isn't mounted.
	return &Server{pluginsDir: pluginsDir, logsDir: logsDir, tail: tail,
		stat: sysstat.New(), docker: dockerstat.New("cofiswarm")}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Dashboard: a tiny embedded page that polls /v1/observed and renders the roster + alerts.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
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
	// Observer's own CPU/memory ("self"), for the dashboard CPU/Memory widget.
	mux.HandleFunc("/v1/stats", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"self": s.stat.Read()})
	})
	// Per-container CPU/memory via the Docker socket (available=false if not mounted).
	mux.HandleFunc("/v1/docker", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(s.docker.Read())
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
	// Dismiss all recorded alerts (alerts have no TTL). POST so a stray GET can't clear them.
	mux.HandleFunc("/v1/alerts/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		cleared := 0
		if s.tail != nil {
			cleared = s.tail.ClearAlerts()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"cleared": cleared})
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

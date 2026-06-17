package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderPrometheus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"port":8086,"names":["architect"],"backend":"llama","ok":true,"usage":0.42,"slots_busy":1,"slots_total":2,"endpoint_id":"coder7b"}]`))
	}))
	defer srv.Close()
	t.Setenv("COFISWARM_SLOT_MANAGER_URL", srv.URL)

	out := RenderPrometheus()
	for _, want := range []string{
		"cofiswarm_kv_pressure_usage",
		`endpoint_id="coder7b"`,
		"0.42",
		"cofiswarm_endpoint_up",
		"cofiswarm_slots_busy",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

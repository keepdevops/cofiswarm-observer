package metrics

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type entry struct {
	Port       int      `json:"port"`
	Names      []string `json:"names"`
	Backend    string   `json:"backend"`
	OK         bool     `json:"ok"`
	Usage      *float64 `json:"usage"`
	SlotsBusy  int      `json:"slots_busy"`
	SlotsTotal int      `json:"slots_total"`
	EndpointID string   `json:"endpoint_id"`
}

func slotManagerURL() string {
	u := os.Getenv("COFISWARM_SLOT_MANAGER_URL")
	if u == "" {
		u = "http://127.0.0.1:8013"
	}
	return strings.TrimRight(u, "/")
}

// RenderPrometheus fetches /api/pressure and emits Prometheus text exposition.
func RenderPrometheus() string {
	var b strings.Builder
	b.WriteString("# HELP cofiswarm_kv_pressure_usage KV fill ratio per inference endpoint (0–1).\n")
	b.WriteString("# TYPE cofiswarm_kv_pressure_usage gauge\n")
	b.WriteString("# HELP cofiswarm_endpoint_up Endpoint responded to pressure probe.\n")
	b.WriteString("# TYPE cofiswarm_endpoint_up gauge\n")
	b.WriteString("# HELP cofiswarm_slots_busy Busy slot count per endpoint.\n")
	b.WriteString("# TYPE cofiswarm_slots_busy gauge\n")
	b.WriteString("# HELP cofiswarm_slots_total Total slot count per endpoint.\n")
	b.WriteString("# TYPE cofiswarm_slots_total gauge\n")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(slotManagerURL() + "/api/pressure")
	if err != nil {
		fmt.Fprintf(&b, "# cofiswarm-observer scrape error: %v\n", err)
		return b.String()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(&b, "# cofiswarm-observer pressure status %d\n", resp.StatusCode)
		return b.String()
	}
	var rows []entry
	if err := json.Unmarshal(body, &rows); err != nil {
		fmt.Fprintf(&b, "# cofiswarm-observer pressure parse error: %v\n", err)
		return b.String()
	}
	for _, e := range rows {
		ep := e.EndpointID
		if ep == "" {
			ep = fmt.Sprintf("port%d", e.Port)
		}
		labels := fmt.Sprintf(`port="%d",backend="%s",endpoint_id="%s"`, e.Port, e.Backend, ep)
		up := 0.0
		if e.OK {
			up = 1
		}
		fmt.Fprintf(&b, "cofiswarm_endpoint_up{%s} %g\n", labels, up)
		usage := 0.0
		if e.Usage != nil {
			usage = *e.Usage
		}
		fmt.Fprintf(&b, "cofiswarm_kv_pressure_usage{%s} %g\n", labels, usage)
		fmt.Fprintf(&b, "cofiswarm_slots_busy{%s} %d\n", labels, e.SlotsBusy)
		fmt.Fprintf(&b, "cofiswarm_slots_total{%s} %d\n", labels, e.SlotsTotal)
	}
	return b.String()
}

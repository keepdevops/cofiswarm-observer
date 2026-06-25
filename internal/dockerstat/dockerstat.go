// Package dockerstat reads per-container CPU/memory from the Docker Engine API over the
// mounted unix socket (/var/run/docker.sock). It powers the dashboard's Docker widget.
// Everything fails soft: if the socket is absent (not mounted) it reports Available=false
// rather than erroring, so the observer runs fine without it.
package dockerstat

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Container is one container's live usage.
type Container struct {
	Name     string  `json:"name"`
	State    string  `json:"state"`
	CPUPct   float64 `json:"cpu_pct"`
	MemUsed  uint64  `json:"mem_used"`
	MemTotal uint64  `json:"mem_total"`
	MemPct   float64 `json:"mem_pct"`
}

// Snapshot is what /v1/docker returns.
type Snapshot struct {
	Available  bool        `json:"available"`
	Containers []Container `json:"containers"`
	Error      string      `json:"error,omitempty"` // surfaced when the socket read fails
}

// Client lazily refreshes a cached snapshot (so a 3s frontend poll doesn't hammer Docker).
type Client struct {
	http   *http.Client
	filter string // only containers whose name contains this (e.g. "cofiswarm"); "" = all

	mu     sync.Mutex
	cached Snapshot
	at     time.Time
	ttl    time.Duration
}

// New builds a client for the socket (COFISWARM_DOCKER_SOCK, default /var/run/docker.sock).
// filter limits to containers whose name contains it.
func New(filter string) *Client {
	sock := os.Getenv("COFISWARM_DOCKER_SOCK")
	if sock == "" {
		sock = "/var/run/docker.sock"
	}
	return &Client{
		filter: filter,
		ttl:    3 * time.Second,
		http: &http.Client{
			Timeout: 6 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", sock)
				},
			},
		},
	}
}

// Read returns the cached snapshot, refreshing if older than ttl.
func (c *Client) Read() Snapshot {
	c.mu.Lock()
	fresh := time.Since(c.at) < c.ttl && (c.cached.Available || !c.at.IsZero())
	if fresh {
		s := c.cached
		c.mu.Unlock()
		return s
	}
	c.mu.Unlock()

	s := c.fetch()
	c.mu.Lock()
	c.cached, c.at = s, time.Now()
	c.mu.Unlock()
	return s
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) fetch() Snapshot {
	var list []struct {
		Names []string `json:"Names"`
		State string   `json:"State"`
	}
	if err := c.get("/v1.43/containers/json", &list); err != nil {
		return Snapshot{Available: false, Error: err.Error()} // socket absent/unreachable — fail soft
	}

	type result struct{ Container }
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := []Container{}
	for _, it := range list {
		name := cleanName(it.Names)
		if c.filter != "" && !strings.Contains(name, c.filter) {
			continue
		}
		wg.Add(1)
		go func(name, state string) {
			defer wg.Done()
			cpu, used, total := c.stats(name)
			var pct float64
			if total > 0 {
				pct = round1(float64(used) / float64(total) * 100)
			}
			mu.Lock()
			out = append(out, Container{Name: name, State: state, CPUPct: round1(cpu), MemUsed: used, MemTotal: total, MemPct: pct})
			mu.Unlock()
		}(name, it.State)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Snapshot{Available: true, Containers: out}
}

// stats does a one-shot stats read (Docker includes precpu, so CPU% is valid in a single call).
func (c *Client) stats(name string) (cpu float64, memUsed, memTotal uint64) {
	var s struct {
		CPU struct {
			Usage  struct{ Total uint64 `json:"total_usage"` } `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
			Online uint64 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPU struct {
			Usage  struct{ Total uint64 `json:"total_usage"` } `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		Mem struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := c.get("/v1.43/containers/"+name+"/stats?stream=false", &s); err != nil {
		return 0, 0, 0
	}
	cpuDelta := float64(s.CPU.Usage.Total) - float64(s.PreCPU.Usage.Total)
	sysDelta := float64(s.CPU.System) - float64(s.PreCPU.System)
	cpus := float64(s.CPU.Online)
	if cpus == 0 {
		cpus = 1
	}
	if sysDelta > 0 && cpuDelta > 0 {
		cpu = cpuDelta / sysDelta * cpus * 100
	}
	memUsed = s.Mem.Usage
	// subtract reclaimable page cache for a "real" usage figure (cgroup v2 / v1 keys)
	if v, ok := s.Mem.Stats["inactive_file"]; ok && v <= memUsed {
		memUsed -= v
	} else if v, ok := s.Mem.Stats["cache"]; ok && v <= memUsed {
		memUsed -= v
	}
	return cpu, memUsed, s.Mem.Limit
}

func cleanName(names []string) string {
	if len(names) == 0 {
		return "?"
	}
	return strings.TrimPrefix(names[0], "/")
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// Package sysstat reports the observer process's own CPU and memory usage — the "self"
// stats the dashboard's CPU/Memory widget shows. Linux /proc + cgroup based (the observer
// runs in a linux container); all reads fail soft to zero so a missing /proc never panics.
package sysstat

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const clkTck = 100.0 // Linux CLK_TCK (jiffies/sec) — the standard value on the container.

// Stat is one self-usage snapshot (bytes for memory).
type Stat struct {
	CPUPct   float64 `json:"cpu_pct"`
	MemUsed  uint64  `json:"mem_used"`
	MemTotal uint64  `json:"mem_total"`
	MemPct   float64 `json:"mem_pct"`
}

// Sampler computes CPU% across calls; it needs two reads for a non-zero delta.
type Sampler struct {
	mu       sync.Mutex
	prevCPU  float64 // seconds of cpu time at last read
	prevWall time.Time
}

func New() *Sampler { return &Sampler{} }

// Read returns current self stats. CPU% is the delta since the previous Read (0 on the
// first call), normalized by NumCPU so a fully-busy core reads 100%/NumCPU of the whole.
func (s *Sampler) Read() Stat {
	cpuSec := readSelfCPUSeconds()
	now := time.Now()

	s.mu.Lock()
	var pct float64
	if !s.prevWall.IsZero() {
		dw := now.Sub(s.prevWall).Seconds()
		if dc := cpuSec - s.prevCPU; dw > 0 && dc >= 0 {
			pct = dc / dw / float64(runtime.NumCPU()) * 100
		}
	}
	s.prevCPU, s.prevWall = cpuSec, now
	s.mu.Unlock()

	used, total := readSelfRSS(), readMemLimit()
	var mpct float64
	if total > 0 {
		mpct = float64(used) / float64(total) * 100
	}
	return Stat{CPUPct: round1(pct), MemUsed: used, MemTotal: total, MemPct: round1(mpct)}
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// readSelfCPUSeconds sums utime+stime from /proc/self/stat (fields 14,15 after the comm).
func readSelfCPUSeconds() float64 {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')') // comm can contain spaces; split after it
	if i < 0 {
		return 0
	}
	f := strings.Fields(s[i+1:]) // f[0]=state(field 3); utime=field14=f[11], stime=f[12]
	if len(f) < 13 {
		return 0
	}
	utime, _ := strconv.ParseFloat(f[11], 64)
	stime, _ := strconv.ParseFloat(f[12], 64)
	return (utime + stime) / clkTck
}

func readSelfRSS() uint64 { return kbField("/proc/self/status", "VmRSS:") }

// readMemLimit prefers the container's cgroup limit, falling back to host MemTotal.
func readMemLimit() uint64 {
	for _, p := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		if b, err := os.ReadFile(p); err == nil {
			v := strings.TrimSpace(string(b))
			if v == "max" {
				continue
			}
			if n, err := strconv.ParseUint(v, 10, 64); err == nil && n > 0 && n < (1<<62) {
				return n
			}
		}
	}
	return kbField("/proc/meminfo", "MemTotal:")
}

// kbField reads a "Label: <kB> kB" line from a /proc file and returns bytes.
func kbField(path, label string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), label) {
			if fields := strings.Fields(sc.Text()); len(fields) >= 2 {
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

//go:build linux

// Package collector loads the eBPF programs, attaches tracepoints to the
// write/read syscalls, and aggregates captured HTTP events into per-endpoint
// statistics that the output package can read at any time.
package collector

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"sync"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang bpf ../../bpf/http_trace.c -- -I/usr/include/bpf -I/usr/include

// Collector reads HTTP events from the eBPF ring buffer and aggregates them
// into per-endpoint statistics, keyed by "comm method path".
type Collector struct {
	objs  bpfObjects
	links []link.Link
	stats map[string]*EndpointStats
	mu    sync.RWMutex
}

// New loads the eBPF programs, attaches tracepoints, and starts reading
// events in the background. Call Close when done.
func New() (*Collector, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("remove memlock: %w", err)
	}

	objs := bpfObjects{}
	if err := loadBpfObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("load bpf objects: %w", err)
	}

	c := &Collector{
		objs:  objs,
		stats: make(map[string]*EndpointStats),
	}

	// sys_enter_write — detects outgoing HTTP requests (Go, Python, curl, etc.)
	tpWrite, err := link.Tracepoint("syscalls", "sys_enter_write", objs.TraceWrite, nil)
	if err != nil {
		return nil, fmt.Errorf("attach write: %w", err)
	}
	c.links = append(c.links, tpWrite)

	// sys_enter_read — saves fd so the exit handler can look up the matching request
	tpReadEnter, err := link.Tracepoint("syscalls", "sys_enter_read", objs.TraceReadEnter, nil)
	if err != nil {
		return nil, fmt.Errorf("attach read enter: %w", err)
	}
	c.links = append(c.links, tpReadEnter)

	// sys_exit_read — computes latency and emits the completed event to the ring buffer
	tpReadExit, err := link.Tracepoint("syscalls", "sys_exit_read", objs.TraceReadExit, nil)
	if err != nil {
		return nil, fmt.Errorf("attach read exit: %w", err)
	}
	c.links = append(c.links, tpReadExit)

	go c.readEvents()

	return c, nil
}

// readEvents loops until the ring buffer is closed, decoding each raw record
// into an HttpEvent and forwarding it to processEvent.
func (c *Collector) readEvents() {
	rd, err := ringbuf.NewReader(c.objs.HttpEvents)
	if err != nil {
		log.Printf("collector: ringbuf reader: %v", err)
		return
	}
	defer rd.Close()

	for {
		record, err := rd.Read()
		if err != nil {
			return // ring buffer closed — collector is shutting down
		}

		var event HttpEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			continue
		}

		c.processEvent(&event)
	}
}

// processEvent accumulates one HTTP event into the stats map.
func (c *Collector) processEvent(e *HttpEvent) {
	method := nullStr(e.Method[:])
	comm := nullStr(e.Comm[:])
	path := nullStr(e.Path[:])

	// Strip query string so "/api/orders?page=1" and "/api/orders" share the same key.
	for i, ch := range path {
		if ch == '?' {
			path = path[:i]
			break
		}
	}

	// Key format matches what output/table.go expects: "comm method path".
	key := fmt.Sprintf("%-12s %s %s", comm, method, path)

	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.stats[key]
	if !ok {
		s = &EndpointStats{}
		c.stats[key] = s
	}

	s.Count++
	s.Latencies = append(s.Latencies, e.DurationNs)

	if e.StatusCode >= 500 || e.StatusCode == 0 {
		s.ErrCount++
	}
}

// Stats returns a function that snapshots the current stats map each time it
// is called. The returned map is safe to read from any goroutine.
func (c *Collector) Stats() func() map[string]*EndpointStats {
	return func() map[string]*EndpointStats {
		c.mu.RLock()
		defer c.mu.RUnlock()

		snapshot := make(map[string]*EndpointStats, len(c.stats))
		for k, v := range c.stats {
			snapshot[k] = v
		}
		return snapshot
	}
}

// Close detaches all tracepoints and unloads the eBPF objects from the kernel.
func (c *Collector) Close() {
	for _, l := range c.links {
		l.Close()
	}
	c.objs.Close()
}

// nullStr converts a null-terminated C string in a byte slice to a Go string.
func nullStr(b []byte) string {
	n := bytes.IndexByte(b, 0)
	if n == -1 {
		return string(b)
	}
	return string(b[:n])
}

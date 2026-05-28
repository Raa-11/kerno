//go:build !linux

// Stub implementations so the project compiles on non-Linux platforms.
// The real collector lives in http.go and requires Linux + eBPF.
package collector

import "fmt"

type Collector struct{}

func New() (*Collector, error) {
	return nil, fmt.Errorf("kerno only runs on Linux")
}

func (c *Collector) Stats() func() map[string]*EndpointStats {
	return func() map[string]*EndpointStats { return nil }
}

func (c *Collector) Close() {}

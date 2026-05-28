// Package aggregator provides stateless helpers for computing latency statistics.
package aggregator

import (
	"fmt"
	"sort"
)

// Percentile returns the pct-th percentile value from a slice of nanosecond
// durations. For example, Percentile(latencies, 99) returns the P99 latency.
func Percentile(latencies []uint64, pct float64) uint64 {
	if len(latencies) == 0 {
		return 0
	}

	sorted := make([]uint64, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	idx := int(float64(len(sorted)-1) * pct / 100)
	return sorted[idx]
}

// NsToMs converts nanoseconds to milliseconds (integer, for sorting/comparison).
func NsToMs(ns uint64) uint64 {
	return ns / 1_000_000
}

// NsToMsF converts nanoseconds to milliseconds as a float64.
func NsToMsF(ns uint64) float64 {
	return float64(ns) / 1_000_000
}

// FmtLatency formats a nanosecond duration as milliseconds with enough decimal
// places to show sub-ms precision without unicode characters.
func FmtLatency(ns uint64) string {
	if ns == 0 {
		return "0ms"
	}
	ms := float64(ns) / 1_000_000
	if ms < 1.0 {
		return fmt.Sprintf("%.2fms", ms)
	}
	if ms < 100.0 {
		return fmt.Sprintf("%.1fms", ms)
	}
	return fmt.Sprintf("%.0fms", ms)
}

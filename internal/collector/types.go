package collector

// HttpEvent is the Go mirror of struct http_event in bpf/http_trace.c.
// Fields must match the C layout exactly so binary.Read can decode ring buffer records.
type HttpEvent struct {
	Pid         uint32
	Sport       uint16
	Dport       uint16
	TimestampNs uint64
	DurationNs  uint64
	StatusCode  uint16
	Method      [8]byte
	Path        [128]byte
	Comm        [16]byte
}

// EndpointStats holds running statistics for one endpoint key.
type EndpointStats struct {
	Count     uint64   // total requests observed
	ErrCount  uint64   // requests with status >= 500 or no status code
	Latencies []uint64 // per-request duration in nanoseconds
}

# kerno

Live HTTP traffic monitor for Linux, powered by eBPF.

Attaches to kernel syscalls to capture HTTP requests from any process — no instrumentation needed.

```
SERVICE         ENDPOINTS   REQ/S   P99 max    ERR% max
────────────────────────────────────────────────────────
coredns         1           4/s     0.30ms     0%
k3s-server      3           3/s     0.53ms     0%
```

## Requirements

- Linux kernel 4.15+
- `CAP_SYS_ADMIN` or root

## Install

```bash
curl -L https://github.com/Raa-11/kerno/releases/latest/download/kerno_linux_amd64.tar.gz | tar xz
sudo install kerno /usr/local/bin/
```

## Run

```bash
sudo kerno
```

**Navigation:**
- `enter` — drill into a service's endpoints
- `s` — cycle sort (REQ/S → P50 → P99 → ERR%)
- `/` — filter by name
- `space` — pause/resume
- `esc` — go back
- `q` — quit

## Build from source

Requires `clang` and Linux BPF headers.

```bash
go generate ./...
go build ./...
sudo ./kerno
```

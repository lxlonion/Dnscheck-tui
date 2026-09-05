# Dnscheck

[English](README.md) | [简体中文](README.zh-CN.md)

A cross-platform, terminal UI (TUI) tool for multi-protocol DNS performance testing and IP geolocation lookup — built in pure Go, with no dependency on system tools like `nslookup`, `dig` or `ping`.

## Features

- **Two-tab TUI** (built with [bubbletea](https://github.com/charmbracelet/bubbletea)):
  - **Page 1 — DNS Performance Dashboard**: application-layer DNS RTT for every configured server (3 attempts each, per-attempt and average), response status (`NOERROR` / `SERVFAIL` / `NXDOMAIN` / `TIMEOUT` / `ERROR`) and success/failure rate. A single timing-out node never blocks the others (all servers are probed concurrently).
  - **Page 2 — Domain Resolution & IP Geo**: resolve the selected domains through every configured DNS server (A + AAAA), then show each resulting IP's country, city and ISP via the [ip-api.com](https://ip-api.com) API.
- **Four DNS transports** ([miekg/dns](https://github.com/miekg/dns) + `net/http`):
  - `UDP` / `TCP` — classic port 53
  - `DoT` (DNS over TLS) — port 853, **strict TLS certificate verification** (no bypass switch)
  - `DoH` (DNS over HTTPS) — RFC 8484 wire format, **HTTP/2 enforced**, strict certificate verification
- **Geo lookup protection**: a process-lifetime in-memory cache deduplicates IP lookups (the cache is never persisted and dies with the process), and a worker pool caps API concurrency (4 requests at a time) to avoid rate limiting.
- **Independent test modes**: the two pages have no dependency on each other — run Page 1 only, Page 2 only, or both at the same time. `[R]` / `[S]` always act on the active page only.
- **IPv4 / IPv6**: IPv6 literals (`[2001:4860:4860::8888]:53`) and AAAA queries are first-class.
- **JSON-driven configuration** with readable validation errors; a default `config.json` is created automatically on first launch.
- **Adaptive layout**: the table trims low-priority columns and truncates overflow on narrow terminals; scrolling with keyboard and mouse wheel.

## Quick Start

Requirements: Go 1.27+ (or any recent Go toolchain).

```bash
git clone git@github.com:lxlonion/Dnscheck.git
cd Dnscheck
go build -o dnscheck ./cmd/dnscheck
./dnscheck
```

- No arguments needed: the tool reads `./config.json`, and creates it with sensible defaults (Google/Cloudflare servers across all four protocols plus two probe domains) if the file does not exist.
- Use a different config: `./dnscheck -config /path/to/config.json`

## Configuration

```json
{
  "timeout_ms": 2000,
  "dns_servers": [
    { "name": "Google UDP", "address": "8.8.8.8:53", "protocol": "udp" },
    { "name": "Google TCP", "address": "8.8.8.8:53", "protocol": "tcp" },
    { "name": "Cloudflare DoT", "address": "1.1.1.1:853", "protocol": "dot" },
    { "name": "Google DoH", "address": "https://dns.google/dns-query", "protocol": "doh" },
    { "name": "Google IPv6 UDP", "address": "[2001:4860:4860::8888]:53", "protocol": "udp" }
  ],
  "domains": ["google.com", "github.com"]
}
```

| Field | Description |
|---|---|
| `timeout_ms` | Global per-query timeout in milliseconds (default `2000`). |
| `dns_servers[].name` | Display name shown in the dashboard. |
| `dns_servers[].address` | `host:port` for `udp`/`tcp`/`dot` (IPv6 must be bracketed), or an `https://` URL for `doh`. |
| `dns_servers[].protocol` | One of `udp`, `tcp`, `dot`, `doh`. |
| `domains` | Target domains for Page 2 resolution. |

Invalid configurations fail fast with a precise error, e.g. `invalid config config.json: dns_servers[2].protocol "icmp": must be one of udp, tcp, dot, doh`.

## Key Bindings

| Key | Action |
|---|---|
| `Tab` / `1` / `2` | Switch page |
| `R` | Re-run the active page's test |
| `S` | Stop / resume the active page's test |
| `D` | Re-open the domain picker on Page 2 |
| `↑` `↓` `PgUp` `PgDn` `Home` `End`, mouse wheel | Scroll the results (footer shows the position) |
| `Q` / `Ctrl+C` | Quit |

On first entry to Page 2 a **domain picker** opens: `↑`/`↓` to move, `Space` to toggle, `Enter` to start resolving the selected domains, `Esc` to dismiss.

## Project Layout

```
cmd/dnscheck/        entry point (flag parsing + program bootstrap)
config/              JSON config loading, defaults and validation
dnsclient/           DNS resolvers (UDP/TCP/DoT/DoH), RTT probing, resolution
geo/                 ip-api.com client with in-memory cache and worker pool
tui/                 bubbletea model, pages, domain picker, adaptive tables
```

## Testing

The test suite is fully offline — DNS and HTTP endpoints are local mocks (miekg/dns servers, `httptest` TLS/HTTP2 servers), so `go test` never touches the network:

```bash
go test ./... -race
go vet ./...
```

## Cross-Platform Builds

```bash
GOOS=darwin  go build -o dnscheck ./cmd/dnscheck
GOOS=linux   go build -o dnscheck ./cmd/dnscheck
GOOS=windows go build -o dnscheck.exe ./cmd/dnscheck
```

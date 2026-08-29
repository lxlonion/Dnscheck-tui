// Package config loads and validates the JSON configuration file that
// drives dnscheck: DNS servers under test, target domains and the global
// per-query timeout.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Protocol identifies the DNS transport protocol used by a server.
type Protocol string

const (
	ProtocolUDP Protocol = "udp"
	ProtocolTCP Protocol = "tcp"
	ProtocolDoT Protocol = "dot"
	ProtocolDoH Protocol = "doh"
)

// DefaultTimeoutMs is applied when timeout_ms is omitted from the config.
const DefaultTimeoutMs = 2000

// DNSServer is one DNS resolver entry from the configuration file.
type DNSServer struct {
	Name     string   `json:"name"`
	Address  string   `json:"address"`
	Protocol Protocol `json:"protocol"`
}

// Config is the root configuration document.
type Config struct {
	TimeoutMs  int         `json:"timeout_ms"`
	DNSServers []DNSServer `json:"dns_servers"`
	Domains   []string    `json:"domains"`
}

// Timeout returns the global per-query timeout as a Duration.
func (c *Config) Timeout() time.Duration {
	return time.Duration(c.TimeoutMs) * time.Millisecond
}

// Load reads, parses and validates a JSON configuration file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &c, nil
}

// Validate checks the configuration and fills in defaults (timeout_ms).
func (c *Config) Validate() error {
	if c.TimeoutMs == 0 {
		c.TimeoutMs = DefaultTimeoutMs
	}
	if c.TimeoutMs < 0 {
		return fmt.Errorf("timeout_ms: must be > 0 (got %d)", c.TimeoutMs)
	}
	if len(c.DNSServers) == 0 {
		return fmt.Errorf("dns_servers: at least one server is required")
	}
	for i, s := range c.DNSServers {
		if err := validateServer(s); err != nil {
			return fmt.Errorf("dns_servers[%d]: %w", i, err)
		}
	}
	if len(c.Domains) == 0 {
		return fmt.Errorf("domains: at least one domain is required")
	}
	for i, d := range c.Domains {
		if d == "" {
			return fmt.Errorf("domains[%d]: empty domain", i)
		}
	}
	return nil
}

func validateServer(s DNSServer) error {
	if s.Name == "" {
		return fmt.Errorf("name: required")
	}
	switch s.Protocol {
	case ProtocolUDP, ProtocolTCP, ProtocolDoT:
		return validateHostPort(s.Address)
	case ProtocolDoH:
		return validateHTTPSURL(s.Address)
	default:
		return fmt.Errorf("protocol %q: must be one of udp, tcp, dot, doh", s.Protocol)
	}
}

// validateHostPort accepts "host:port" with IPv6 literals in brackets,
// e.g. "8.8.8.8:53" or "[2001:4860:4860::8888]:53".
func validateHostPort(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("address %q: want host:port (IPv6 must be bracketed, e.g. [2001:db8::1]:53): %v", addr, err)
	}
	if host == "" {
		return fmt.Errorf("address %q: host is empty", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("address %q: port %q must be within 1-65535", addr, port)
	}
	return nil
}

// validateHTTPSURL accepts an https:// URL for DoH endpoints.
func validateHTTPSURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("address %q: %v", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("address %q: DoH endpoint must use https", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("address %q: host is empty", raw)
	}
	return nil
}

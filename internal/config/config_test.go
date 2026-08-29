package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return p
}

const validJSON = `{
  "timeout_ms": 1500,
  "dns_servers": [
    {"name": "Google UDP", "address": "8.8.8.8:53", "protocol": "udp"},
    {"name": "Cloudflare DoT", "address": "1.1.1.1:853", "protocol": "dot"},
    {"name": "Google DoH", "address": "https://dns.google/dns-query", "protocol": "doh"},
    {"name": "IPv6 TCP", "address": "[2001:4860:4860::8888]:53", "protocol": "tcp"}
  ],
  "domains": ["google.com", "github.com"]
}`

func TestLoadValid(t *testing.T) {
	c, err := Load(writeTemp(t, validJSON))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := c.TimeoutMs, 1500; got != want {
		t.Errorf("TimeoutMs = %d, want %d", got, want)
	}
	if got, want := c.Timeout(), 1500*time.Millisecond; got != want {
		t.Errorf("Timeout() = %v, want %v", got, want)
	}
	if len(c.DNSServers) != 4 {
		t.Fatalf("len(DNSServers) = %d, want 4", len(c.DNSServers))
	}
	if c.DNSServers[3].Address != "[2001:4860:4860::8888]:53" {
		t.Errorf("IPv6 address mismatch: %s", c.DNSServers[3].Address)
	}
	if len(c.Domains) != 2 || c.Domains[0] != "google.com" {
		t.Errorf("Domains = %v", c.Domains)
	}
}

func TestLoadDefaultTimeout(t *testing.T) {
	j := `{"dns_servers": [{"name": "G", "address": "8.8.8.8:53", "protocol": "udp"}], "domains": ["a.com"]}`
	c, err := Load(writeTemp(t, j))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := c.TimeoutMs, DefaultTimeoutMs; got != want {
		t.Errorf("TimeoutMs = %d, want default %d", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil || !strings.Contains(err.Error(), "read config") {
		t.Errorf("err = %v, want read config failure", err)
	}
}

func TestLoadOrCreateCreatesDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c, err := LoadOrCreate(p)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("default config not created: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("default config file is empty")
	}
	if c.TimeoutMs != DefaultTimeoutMs {
		t.Errorf("TimeoutMs = %d, want %d", c.TimeoutMs, DefaultTimeoutMs)
	}
	if len(c.DNSServers) == 0 || len(c.Domains) == 0 {
		t.Errorf("default config incomplete: %d servers, %d domains", len(c.DNSServers), len(c.Domains))
	}
	for _, want := range []Protocol{ProtocolUDP, ProtocolTCP, ProtocolDoT, ProtocolDoH} {
		found := false
		for _, s := range c.DNSServers {
			if s.Protocol == want {
				found = true
			}
		}
		if !found {
			t.Errorf("default config missing %q server", want)
		}
	}
}

func TestLoadOrCreateKeepsExistingFile(t *testing.T) {
	p := writeTemp(t, `{"timeout_ms": 1234, "dns_servers": [{"name": "X", "address": "8.8.8.8:53", "protocol": "udp"}], "domains": ["a.com"]}`)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	c, err := LoadOrCreate(p)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if c.TimeoutMs != 1234 {
		t.Errorf("existing config was overwritten: TimeoutMs = %d", c.TimeoutMs)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("existing config file must not be modified")
	}
}

func TestLoadOrCreateWriteError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "missing-dir", "config.json")
	_, err := LoadOrCreate(p)
	if err == nil || !strings.Contains(err.Error(), "write default config") {
		t.Errorf("err = %v, want write default config failure", err)
	}
}

func TestWriteDefaultProducesValidConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := WriteDefault(p); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if _, err := Load(p); err != nil {
		t.Errorf("default template must be valid: %v", err)
	}
}

func TestLoadInvalid(t *testing.T) {
	const oneServer = `"dns_servers": [{"name": "G", "address": "8.8.8.8:53", "protocol": "udp"}]`
	cases := []struct {
		name    string
		json    string
		wantErr string
	}{
		{"bad json", `{`, "parse config"},
		{"no servers", `{"dns_servers": [], "domains": ["a.com"]}`, "dns_servers"},
		{
			"invalid protocol",
			`{` + oneServer + `, "dns_servers": [{"name": "X", "address": "8.8.8.8:53", "protocol": "icmp"}], "domains": ["a.com"]}`,
			"protocol",
		},
		{
			"udp missing port",
			`{"dns_servers": [{"name": "X", "address": "8.8.8.8", "protocol": "udp"}], "domains": ["a.com"]}`,
			"host:port",
		},
		{
			"dot bad port",
			`{"dns_servers": [{"name": "X", "address": "1.1.1.1:99999", "protocol": "dot"}], "domains": ["a.com"]}`,
			"1-65535",
		},
		{
			"doh http scheme",
			`{"dns_servers": [{"name": "X", "address": "http://x/dns-query", "protocol": "doh"}], "domains": ["a.com"]}`,
			"https",
		},
		{
			"doh no host",
			`{"dns_servers": [{"name": "X", "address": "https:///dns-query", "protocol": "doh"}], "domains": ["a.com"]}`,
			"host is empty",
		},
		{
			"empty name",
			`{"dns_servers": [{"name": "", "address": "8.8.8.8:53", "protocol": "udp"}], "domains": ["a.com"]}`,
			"name",
		},
		{"no domains", `{` + oneServer + `, "domains": []}`, "domains"},
		{"empty domain", `{` + oneServer + `, "domains": [""]}`, "empty domain"},
		{
			"negative timeout",
			`{"timeout_ms": -5, ` + oneServer + `, "domains": ["a.com"]}`,
			"timeout_ms",
		},
		{
			"port zero",
			`{"dns_servers": [{"name": "X", "address": "8.8.8.8:0", "protocol": "udp"}], "domains": ["a.com"]}`,
			"1-65535",
		},
		{
			"port non-numeric",
			`{"dns_servers": [{"name": "X", "address": "8.8.8.8:abc", "protocol": "udp"}], "domains": ["a.com"]}`,
			"1-65535",
		},
		{
			"bare ipv6 no brackets",
			`{"dns_servers": [{"name": "X", "address": "2001:4860:4860::8888:53", "protocol": "udp"}], "domains": ["a.com"]}`,
			"host:port",
		},
		{
			"empty host",
			`{"dns_servers": [{"name": "X", "address": ":53", "protocol": "udp"}], "domains": ["a.com"]}`,
			"host is empty",
		},
		{
			"json type mismatch",
			`{"timeout_ms": "abc", ` + oneServer + `, "domains": ["a.com"]}`,
			"parse config",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTemp(t, tc.json))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want substring %q", err, tc.wantErr)
			}
		})
	}
}

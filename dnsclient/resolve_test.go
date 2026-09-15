package dnsclient

import (
	"context"
	"testing"
	"time"

	"github.com/lxlonion/Dnscheck-tui/config"
)

func TestResolveServerAAndAAAA(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	rr := ResolveServer(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "ok.test", time.Second)
	if rr.Status() != StatusNOERROR {
		t.Fatalf("status = %s (A=%s AAAA=%s)", rr.Status(), rr.AStatus, rr.AAAAStatus)
	}
	if len(rr.A) != 1 || rr.A[0] != "93.184.216.34" {
		t.Errorf("A = %v", rr.A)
	}
	if len(rr.AAAA) != 1 || rr.AAAA[0] != "2001:db8::1" {
		t.Errorf("AAAA = %v", rr.AAAA)
	}
	if rr.Empty() {
		t.Error("must not be empty")
	}
}

func TestResolveServerFailure(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	rr := ResolveServer(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "nxdomain.test", time.Second)
	if rr.Status() != StatusNXDOMAIN {
		t.Errorf("status = %s", rr.Status())
	}
	if !rr.Empty() {
		t.Errorf("A = %v AAAA = %v, want empty", rr.A, rr.AAAA)
	}
}

func TestResolveServerTimeout(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	rr := ResolveServer(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "slow.test", 80*time.Millisecond)
	if rr.Status() != StatusTIMEOUT {
		t.Errorf("status = %s (A=%s AAAA=%s)", rr.Status(), rr.AStatus, rr.AAAAStatus)
	}
}

func TestResolveAllOrderAndCoverage(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	servers := []config.DNSServer{
		server("udp-srv", mock.udpAddr, config.ProtocolUDP),
		server("dead-srv", "127.0.0.1:1", config.ProtocolTCP),
	}
	domains := []string{"ok.test", "empty.test"}
	results := ResolveAll(context.Background(), servers, domains, 500*time.Millisecond)

	if len(results) != 2 {
		t.Fatalf("results = %d domains, want 2", len(results))
	}
	if results[0].Domain != "ok.test" || results[1].Domain != "empty.test" {
		t.Errorf("domain order = [%s %s]", results[0].Domain, results[1].Domain)
	}
	for _, dr := range results {
		if len(dr.Entries) != 2 {
			t.Fatalf("entries = %d, want 2", len(dr.Entries))
		}
		if dr.Entries[0].Server.Name != "udp-srv" || dr.Entries[1].Server.Name != "dead-srv" {
			t.Errorf("server order mismatch: [%s %s]", dr.Entries[0].Server.Name, dr.Entries[1].Server.Name)
		}
	}
	if results[0].Entries[0].Status() != StatusNOERROR {
		t.Errorf("ok.test/udp = %s", results[0].Entries[0].Status())
	}
	if results[0].Entries[1].Status() == StatusNOERROR {
		t.Errorf("ok.test/dead should fail, got %s", results[0].Entries[1].Status())
	}
	if results[1].Entries[0].Status() != StatusNOERROR || !results[1].Entries[0].Empty() {
		t.Errorf("empty.test/udp: status %s empty=%v (want NOERROR + empty)", results[1].Entries[0].Status(), results[1].Entries[0].Empty())
	}

	ips := CollectIPs(results)
	if len(ips) != 2 || ips[0] != "93.184.216.34" || ips[1] != "2001:db8::1" {
		t.Errorf("CollectIPs = %v, want [93.184.216.34 2001:db8::1]", ips)
	}
}

func TestResolveServerUnsupportedProtocol(t *testing.T) {
	rr := ResolveServer(context.Background(), server("bad", "8.8.8.8:53", config.Protocol("icmp")), "ok.test", time.Second)
	if rr.Status() != StatusERROR {
		t.Errorf("status = %s, want ERROR", rr.Status())
	}
	if rr.Detail == "" {
		t.Error("error detail required")
	}
}

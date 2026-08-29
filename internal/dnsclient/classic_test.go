package dnsclient

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"

	"dnscheck/internal/config"
)

type mockDNS struct {
	udpAddr string
	tcpAddr string
	closers []func()
}

// startMock runs a local DNS server pair (UDP + TCP) on the given host with
// behavior keyed by query name prefix:
//
//	ok.*        -> NOERROR with A 93.184.216.34 / AAAA 2001:db8::1
//	servfail.*  -> SERVFAIL
//	nxdomain.*  -> NXDOMAIN
//	empty.*     -> NOERROR without answer records
//	slow.*      -> responds after 400ms
func startMock(t *testing.T, host string) *mockDNS {
	t.Helper()

	mux := dns.NewServeMux()
	flakyCalls := uint32(0)
	mux.HandleFunc("test.", func(w dns.ResponseWriter, r *dns.Msg) {
		q := r.Question[0]
		name := strings.ToLower(q.Name)
		m := new(dns.Msg)
		switch {
		case strings.HasPrefix(name, "slow."):
			time.Sleep(400 * time.Millisecond)
			m.SetReply(r)
		case strings.HasPrefix(name, "servfail."):
			m.SetRcode(r, dns.RcodeServerFailure)
		case strings.HasPrefix(name, "nxdomain."):
			m.SetRcode(r, dns.RcodeNameError)
		case strings.HasPrefix(name, "refused."):
			m.SetRcode(r, dns.RcodeRefused)
		case strings.HasPrefix(name, "empty."):
			m.SetReply(r)
		case strings.HasPrefix(name, "flaky."):
			if atomic.AddUint32(&flakyCalls, 1) == 1 {
				m.SetRcode(r, dns.RcodeServerFailure)
			} else {
				m.SetReply(r)
			}
		default:
			m.SetReply(r)
			hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: 60}
			if q.Qtype == dns.TypeAAAA {
				hdr.Rrtype = dns.TypeAAAA
				m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr, AAAA: net.ParseIP("2001:db8::1")})
			} else {
				hdr.Rrtype = dns.TypeA
				m.Answer = append(m.Answer, &dns.A{Hdr: hdr, A: net.ParseIP("93.184.216.34")})
			}
		}
		w.WriteMsg(m)
	})

	m := &mockDNS{}
	hostPort := func(port string) string {
		if strings.Contains(host, ":") {
			return "[" + host + "]:" + port
		}
		return host + ":" + port
	}

	pc, err := net.ListenPacket("udp", hostPort("0"))
	if err != nil {
		t.Fatalf("udp listen: %v", err)
	}
	udpSrv := &dns.Server{PacketConn: pc, Handler: mux}
	go udpSrv.ActivateAndServe()
	m.udpAddr = pc.LocalAddr().String()

	lc, err := net.Listen("tcp", hostPort("0"))
	if err != nil {
		t.Fatalf("tcp listen: %v", err)
	}
	tcpSrv := &dns.Server{Listener: lc, Handler: mux}
	go tcpSrv.ActivateAndServe()
	m.tcpAddr = lc.Addr().String()

	t.Cleanup(func() {
		udpSrv.Shutdown()
		tcpSrv.Shutdown()
	})
	return m
}

func server(name, addr string, proto config.Protocol) config.DNSServer {
	return config.DNSServer{Name: name, Address: addr, Protocol: proto}
}

func TestExchangeUDPSuccess(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	res := classicResolver{net: "udp"}
	got := res.Exchange(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "ok.test", dns.TypeA)
	if got.Status != StatusNOERROR || !got.Success() {
		t.Fatalf("status = %s, detail = %q", got.Status, got.Detail)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "93.184.216.34" {
		t.Errorf("IPs = %v", got.IPs)
	}
	if got.RTT <= 0 || got.RTT > time.Second {
		t.Errorf("RTT = %v, out of sane range", got.RTT)
	}
	if got.Truncated {
		t.Error("unexpected truncation")
	}
}

func TestExchangeTCPSuccess(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	res := classicResolver{net: "tcp"}
	got := res.Exchange(context.Background(), server("mock", mock.tcpAddr, config.ProtocolTCP), "ok.test", dns.TypeA)
	if got.Status != StatusNOERROR || !got.Success() {
		t.Fatalf("status = %s, detail = %q", got.Status, got.Detail)
	}
	if len(got.IPs) != 1 {
		t.Errorf("IPs = %v", got.IPs)
	}
}

func TestExchangeRcodeClassification(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	res := classicResolver{net: "udp"}
	for name, want := range map[string]Status{
		"servfail.test": StatusSERVFAIL,
		"nxdomain.test": StatusNXDOMAIN,
		"refused.test":  Status("REFUSED"),
	} {
		got := res.Exchange(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), name, dns.TypeA)
		if got.Status != want {
			t.Errorf("%s: status = %s, want %s", name, got.Status, want)
		}
		if got.Success() {
			t.Errorf("%s: should not be success", name)
		}
	}
}

func TestExchangeAAAA(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	res := classicResolver{net: "udp"}
	got := res.Exchange(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "ok.test", dns.TypeAAAA)
	if got.Status != StatusNOERROR {
		t.Fatalf("status = %s", got.Status)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "2001:db8::1" {
		t.Errorf("IPs = %v, want [2001:db8::1]", got.IPs)
	}
}

func TestExchangeTimeout(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	res := classicResolver{net: "udp"}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := res.Exchange(ctx, server("mock", mock.udpAddr, config.ProtocolUDP), "slow.test", dns.TypeA)
	elapsed := time.Since(start)
	if got.Status != StatusTIMEOUT {
		t.Fatalf("status = %s, want TIMEOUT (detail %q)", got.Status, got.Detail)
	}
	if got.Success() {
		t.Error("timeout must not be success")
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("exchange blocked %v; must abort at ~80ms", elapsed)
	}
}

func TestExchangeEmptyAnswer(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	res := classicResolver{net: "udp"}
	got := res.Exchange(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "empty.test", dns.TypeA)
	if got.Status != StatusNOERROR {
		t.Fatalf("status = %s", got.Status)
	}
	if len(got.IPs) != 0 {
		t.Errorf("IPs = %v, want empty", got.IPs)
	}
}

func TestExchangeRefused(t *testing.T) {
	res := classicResolver{net: "tcp"}
	start := time.Now()
	got := res.Exchange(context.Background(), server("dead", "127.0.0.1:1", config.ProtocolTCP), "ok.test", dns.TypeA)
	elapsed := time.Since(start)
	if got.Status != StatusERROR {
		t.Fatalf("status = %s, want ERROR (detail %q)", got.Status, got.Detail)
	}
	if got.Detail == "" {
		t.Error("ERROR result should carry a detail")
	}
	if elapsed > 2*time.Second {
		t.Errorf("refused exchange took %v", elapsed)
	}
}

func TestExchangeIPv6Transport(t *testing.T) {
	mock := startMock(t, "::1")
	if !strings.HasPrefix(mock.udpAddr, "[::1]") {
		t.Skipf("no IPv6 loopback: %s", mock.udpAddr)
	}
	res := classicResolver{net: "udp"}
	got := res.Exchange(context.Background(), server("mock6", mock.udpAddr, config.ProtocolUDP), "ok.test", dns.TypeA)
	if got.Status != StatusNOERROR {
		t.Fatalf("IPv6 exchange failed: %s (%q)", got.Status, got.Detail)
	}
}

func TestProbeServerThreeAttempts(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	pr := ProbeServer(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "ok.test", dns.TypeA, 3, time.Second)
	if len(pr.Attempts) != 3 {
		t.Fatalf("attempts = %d, want 3", len(pr.Attempts))
	}
	if pr.Successes != 3 || pr.Failures != 0 {
		t.Errorf("successes = %d, failures = %d", pr.Successes, pr.Failures)
	}
	if pr.SuccessRate() != 1 {
		t.Errorf("SuccessRate = %f", pr.SuccessRate())
	}
	if pr.AvgRTT <= 0 {
		t.Errorf("AvgRTT = %v", pr.AvgRTT)
	}
}

func TestProbeServerAllFail(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	pr := ProbeServer(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "servfail.test", dns.TypeA, 3, time.Second)
	if pr.Successes != 0 || pr.Failures != 3 {
		t.Errorf("successes = %d, failures = %d", pr.Successes, pr.Failures)
	}
	if pr.AvgRTT != 0 {
		t.Errorf("AvgRTT = %v, want 0 with no successes", pr.AvgRTT)
	}
}

func TestProbeServerTimeoutDoesNotBlockRemainingAttempts(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	start := time.Now()
	pr := ProbeServer(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "slow.test", dns.TypeA, 3, 80*time.Millisecond)
	elapsed := time.Since(start)
	if len(pr.Attempts) != 3 {
		t.Fatalf("attempts = %d, want 3", len(pr.Attempts))
	}
	for _, a := range pr.Attempts {
		if a.Status != StatusTIMEOUT {
			t.Errorf("attempt status = %s, want TIMEOUT", a.Status)
		}
	}
	if elapsed > 900*time.Millisecond {
		t.Errorf("3x80ms timeouts took %v; timeouts must not stack up", elapsed)
	}
}

func TestProbeAllConcurrent(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	servers := []config.DNSServer{
		server("ok-udp", mock.udpAddr, config.ProtocolUDP),
		server("ok-tcp", mock.tcpAddr, config.ProtocolTCP),
		server("refused", "127.0.0.1:1", config.ProtocolTCP),
	}
	start := time.Now()
	results := ProbeAll(context.Background(), servers, "ok.test", dns.TypeA, 2, time.Second)
	elapsed := time.Since(start)

	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results[0].Successes != 2 || results[1].Successes != 2 {
		t.Errorf("healthy servers: successes = %d, %d", results[0].Successes, results[1].Successes)
	}
	if results[2].Successes != 0 {
		t.Errorf("refused server should fail, got %d successes", results[2].Successes)
	}
	if elapsed > 3*time.Second {
		t.Errorf("ProbeAll took %v; servers should run concurrently", elapsed)
	}
}

func TestNewResolverFactory(t *testing.T) {
	if _, err := NewResolver(config.ProtocolUDP); err != nil {
		t.Errorf("udp: %v", err)
	}
	if _, err := NewResolver(config.ProtocolTCP); err != nil {
		t.Errorf("tcp: %v", err)
	}
	for _, p := range []config.Protocol{config.ProtocolDoT, config.ProtocolDoH, config.Protocol("icmp")} {
		if _, err := NewResolver(p); err == nil {
			t.Errorf("protocol %q should be unsupported until Phase 4", p)
		}
	}
}

func TestProbeServerAttemptsClamp(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	pr := ProbeServer(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "ok.test", dns.TypeA, 0, time.Second)
	if len(pr.Attempts) != 1 {
		t.Errorf("attempts = %d, want clamped to 1", len(pr.Attempts))
	}
}

func TestProbeServerMixedAttempts(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	pr := ProbeServer(context.Background(), server("mock", mock.udpAddr, config.ProtocolUDP), "flaky.test", dns.TypeA, 3, time.Second)
	if pr.Successes != 2 || pr.Failures != 1 {
		t.Errorf("successes = %d, failures = %d, want 2/1", pr.Successes, pr.Failures)
	}
	if pr.AvgRTT <= 0 {
		t.Error("AvgRTT should average successful attempts only")
	}
}

func TestProbeAllCtxCancel(t *testing.T) {
	mock := startMock(t, "127.0.0.1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := ProbeAll(ctx, []config.DNSServer{server("mock", mock.udpAddr, config.ProtocolUDP)}, "slow.test", dns.TypeA, 3, time.Second)
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	if len(results[0].Attempts) > 3 {
		t.Errorf("attempts = %d, want <= 3 after cancel", len(results[0].Attempts))
	}
}

package dnsclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/lxlonion/Dnscheck-tui/config"
)

func startDoTMock(t *testing.T) (addr string, pool *x509.CertPool) {
	t.Helper()
	cert, pool := genSelfSignedCert(t)
	lc, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	srv := &dns.Server{
		Listener: lc,
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			if isSlowQuery(r) {
				time.Sleep(400 * time.Millisecond)
			}
			w.WriteMsg(mockReply(r))
		}),
	}
	go srv.ActivateAndServe()
	t.Cleanup(func() { srv.Shutdown() })
	return lc.Addr().String(), pool
}

func TestDoTExchangeSuccess(t *testing.T) {
	addr, pool := startDoTMock(t)
	r := &classicResolver{net: "tcp-tls", tlsConfig: &tls.Config{RootCAs: pool}}
	got := r.Exchange(context.Background(), server("dot-mock", addr, config.ProtocolDoT), "ok.test", dns.TypeA)
	if got.Status != StatusNOERROR || !got.Success() {
		t.Fatalf("status = %s, detail = %q", got.Status, got.Detail)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "93.184.216.34" {
		t.Errorf("IPs = %v", got.IPs)
	}
	if got.RTT <= 0 || got.RTT > 2*time.Second {
		t.Errorf("RTT = %v", got.RTT)
	}
}

func TestDoTStrictCertificateVerification(t *testing.T) {
	addr, _ := startDoTMock(t)
	r := &classicResolver{net: "tcp-tls"}
	got := r.Exchange(context.Background(), server("dot-mock", addr, config.ProtocolDoT), "ok.test", dns.TypeA)
	if got.Status != StatusERROR {
		t.Fatalf("self-signed cert must be rejected, status = %s", got.Status)
	}
	if !strings.Contains(got.Detail, "certificate") {
		t.Errorf("detail = %q, want certificate verification failure", got.Detail)
	}
	if got.Success() {
		t.Error("rejected exchange must not count as success")
	}
}

func TestDoTRcodeClassification(t *testing.T) {
	addr, pool := startDoTMock(t)
	r := &classicResolver{net: "tcp-tls", tlsConfig: &tls.Config{RootCAs: pool}}
	for name, want := range map[string]Status{
		"servfail.test": StatusSERVFAIL,
		"nxdomain.test": StatusNXDOMAIN,
		"refused.test":  Status("REFUSED"),
	} {
		got := r.Exchange(context.Background(), server("dot-mock", addr, config.ProtocolDoT), name, dns.TypeA)
		if got.Status != want {
			t.Errorf("%s: status = %s, want %s", name, got.Status, want)
		}
	}
}

func TestDoTTimeout(t *testing.T) {
	addr, pool := startDoTMock(t)
	r := &classicResolver{net: "tcp-tls", tlsConfig: &tls.Config{RootCAs: pool}}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := r.Exchange(ctx, server("dot-mock", addr, config.ProtocolDoT), "slow.test", dns.TypeA)
	if got.Status != StatusTIMEOUT {
		t.Fatalf("status = %s, want TIMEOUT (detail %q)", got.Status, got.Detail)
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Error("DoT exchange must abort at the context deadline")
	}
}

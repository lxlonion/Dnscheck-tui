package dnsclient

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/lxlonion/Dnscheck-tui/config"
)

// dohHandler serves RFC 8484 wire-format DNS over the mock behaviors;
// http500.test responses are mapped to an internal server error.
func dohHandler(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("Content-Type") != dohContentType {
		http.Error(w, "bad content type", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, dohMaxBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	m := new(dns.Msg)
	if err := m.Unpack(body); err != nil {
		http.Error(w, "bad message", http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(strings.ToLower(m.Question[0].Name), "http500.") {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	if isSlowQuery(m) {
		time.Sleep(400 * time.Millisecond)
	}
	wire, err := mockReply(m).Pack()
	if err != nil {
		http.Error(w, "pack", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", dohContentType)
	w.Write(wire)
}

func testDoHClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
	}}
}

func startDoHMock(t *testing.T, h2 bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(dohHandler))
	if h2 {
		srv.EnableHTTP2 = true
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestDoHExchangeSuccess(t *testing.T) {
	srv := startDoHMock(t, true)
	r := &dohResolver{client: testDoHClient()}
	got := r.Exchange(context.Background(), server("doh-mock", srv.URL, config.ProtocolDoH), "ok.test", dns.TypeA)
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

func TestDoHAAAA(t *testing.T) {
	srv := startDoHMock(t, true)
	r := &dohResolver{client: testDoHClient()}
	got := r.Exchange(context.Background(), server("doh-mock", srv.URL, config.ProtocolDoH), "ok.test", dns.TypeAAAA)
	if got.Status != StatusNOERROR {
		t.Fatalf("status = %s", got.Status)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "2001:db8::1" {
		t.Errorf("IPs = %v", got.IPs)
	}
}

func TestDoHRejectsNonHTTP2(t *testing.T) {
	srv := startDoHMock(t, false)
	r := &dohResolver{client: testDoHClient()}
	got := r.Exchange(context.Background(), server("doh-mock", srv.URL, config.ProtocolDoH), "ok.test", dns.TypeA)
	if got.Status != StatusERROR {
		t.Fatalf("status = %s, want ERROR (detail %q)", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "HTTP/2") {
		t.Errorf("detail = %q, want HTTP/2 rejection", got.Detail)
	}
	if got.Success() {
		t.Error("HTTP/1.1 response must not count as success")
	}
}

func TestDoHStrictCertificateVerification(t *testing.T) {
	srv := startDoHMock(t, true)
	r := newDoHResolver()
	got := r.Exchange(context.Background(), server("doh-mock", srv.URL, config.ProtocolDoH), "ok.test", dns.TypeA)
	if got.Status != StatusERROR {
		t.Fatalf("self-signed cert must be rejected by default client, status = %s (detail %q)", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "certificate") {
		t.Errorf("detail = %q, want certificate failure", got.Detail)
	}
}

func TestDoHRcodeAndHTTPStatus(t *testing.T) {
	srv := startDoHMock(t, true)
	r := &dohResolver{client: testDoHClient()}
	sv := server("doh-mock", srv.URL, config.ProtocolDoH)

	got := r.Exchange(context.Background(), sv, "servfail.test", dns.TypeA)
	if got.Status != StatusSERVFAIL {
		t.Errorf("servfail: status = %s", got.Status)
	}

	got = r.Exchange(context.Background(), sv, "nxdomain.test", dns.TypeA)
	if got.Status != StatusNXDOMAIN {
		t.Errorf("nxdomain: status = %s", got.Status)
	}

	got = r.Exchange(context.Background(), sv, "http500.test", dns.TypeA)
	if got.Status != StatusERROR || !strings.Contains(got.Detail, "500") {
		t.Errorf("http500: status = %s detail = %q", got.Status, got.Detail)
	}
}

func TestDoHTimeout(t *testing.T) {
	srv := startDoHMock(t, true)
	r := &dohResolver{client: testDoHClient()}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := r.Exchange(ctx, server("doh-mock", srv.URL, config.ProtocolDoH), "slow.test", dns.TypeA)
	if got.Status != StatusTIMEOUT {
		t.Fatalf("status = %s, want TIMEOUT (detail %q)", got.Status, got.Detail)
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Error("DoH exchange must abort at the context deadline")
	}
}

func TestDoHProbeAllIntegration(t *testing.T) {
	srv := startDoHMock(t, true)
	res := &dohResolver{client: testDoHClient()}
	s := server("doh-ok", srv.URL, config.ProtocolDoH)
	pr := ProbeWithResolver(context.Background(), res, s, "ok.test", dns.TypeA, 3, time.Second)
	if pr.Successes != 3 {
		t.Errorf("successes = %d, want 3", pr.Successes)
	}
	if pr.AvgRTT <= 0 {
		t.Errorf("AvgRTT = %v", pr.AvgRTT)
	}
}

package dnsclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/miekg/dns"

	"dnscheck/config"
)

const (
	dohContentType = "application/dns-message"
	dohMaxBody     = 64 << 10
)

// dohResolver implements DNS-over-HTTPS (RFC 8484). The transport is locked
// to HTTP/2 and TLS certificates are verified with the default rules (the
// http.Transport never disables verification).
type dohResolver struct {
	client *http.Client
}

func newDoHResolver() *dohResolver {
	return &dohResolver{
		client: &http.Client{Transport: &http.Transport{ForceAttemptHTTP2: true}},
	}
}

func (r *dohResolver) Exchange(ctx context.Context, server config.DNSServer, question string, qtype uint16) Result {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(question), qtype)
	m.RecursionDesired = true

	res := Result{ServerName: server.Name, Protocol: server.Protocol}

	wire, err := m.Pack()
	if err != nil {
		res.Status, res.Detail = StatusERROR, err.Error()
		return res
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.Address, bytes.NewReader(wire))
	if err != nil {
		res.Status, res.Detail = StatusERROR, err.Error()
		return res
	}
	req.Header.Set("Content-Type", dohContentType)
	req.Header.Set("Accept", dohContentType)

	start := time.Now()
	resp, err := r.client.Do(req)
	res.RTT = time.Since(start)
	if err != nil {
		res.Status, res.Detail = classifyTransportError(ctx, err)
		return res
	}
	defer resp.Body.Close()

	if resp.Proto != "HTTP/2.0" {
		res.Status, res.Detail = StatusERROR, fmt.Sprintf("DoH requires HTTP/2, got %s", resp.Proto)
		return res
	}
	if resp.StatusCode != http.StatusOK {
		res.Status, res.Detail = StatusERROR, fmt.Sprintf("DoH HTTP status %d", resp.StatusCode)
		return res
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, dohMaxBody))
	if err != nil {
		res.Status, res.Detail = classifyTransportError(ctx, err)
		return res
	}
	reply := new(dns.Msg)
	if err := reply.Unpack(body); err != nil {
		res.Status, res.Detail = StatusERROR, err.Error()
		return res
	}

	res.Status, res.IPs = classifyReply(reply, qtype)
	res.Truncated = reply.Truncated
	return res
}

package dnsclient

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"time"

	"github.com/miekg/dns"

	"dnscheck/config"
)

// classicResolver exchanges DNS messages over plain UDP, plain TCP or
// DNS-over-TLS ("tcp-tls") using github.com/miekg/dns. TLS verification is
// strict by default: tlsConfig stays nil in production so the system root
// store and normal certificate verification apply.
type classicResolver struct {
	net       string
	tlsConfig *tls.Config
}

func (r *classicResolver) Exchange(ctx context.Context, server config.DNSServer, question string, qtype uint16) Result {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(question), qtype)
	m.RecursionDesired = true

	c := &dns.Client{Net: r.net, TLSConfig: r.tlsConfig}
	res := Result{ServerName: server.Name, Protocol: server.Protocol}

	start := time.Now()
	resp, _, err := c.ExchangeContext(ctx, m, server.Address)
	if err == nil && r.net == "udp" && resp.Truncated {
		// A TC=1 UDP answer is incomplete by definition (RFC 1035 4.2.1),
		// and reporting NOERROR would hide the missing records. Retry the
		// very same pre-built message over TCP against the same address:
		// identical ID and flags, and the caller's ctx still bounds the
		// whole exchange. tcp/tcp-tls transports never truncate.
		resp, _, err = (&dns.Client{Net: "tcp"}).ExchangeContext(ctx, m, server.Address)
	}
	// RTT covers the user-perceived exchange including any TCP fallback; a
	// failed fallback must surface as TIMEOUT/ERROR, never as an
	// incomplete "success" carrying the UDP truncation flag.
	res.RTT = time.Since(start)
	if err != nil {
		res.Status, res.Detail = classifyTransportError(ctx, err)
		return res
	}

	res.Status, res.IPs = classifyReply(resp, qtype)
	res.Truncated = resp.Truncated
	return res
}

func isTimeoutErr(err error) bool {
	var nerr net.Error
	return errors.As(err, &nerr) && nerr.Timeout()
}

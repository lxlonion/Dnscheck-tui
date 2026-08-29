package dnsclient

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/miekg/dns"

	"dnscheck/internal/config"
)

// classicResolver exchanges DNS messages over plain UDP or TCP (port 53 style
// transports) using github.com/miekg/dns.
type classicResolver struct {
	net string
}

func (r *classicResolver) Exchange(ctx context.Context, server config.DNSServer, question string, qtype uint16) Result {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(question), qtype)
	m.RecursionDesired = true

	c := &dns.Client{Net: r.net}
	res := Result{ServerName: server.Name, Protocol: server.Protocol}

	start := time.Now()
	resp, _, err := c.ExchangeContext(ctx, m, server.Address)
	res.RTT = time.Since(start)
	if err != nil {
		res.Status = StatusERROR
		res.Detail = err.Error()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || isTimeoutErr(err) {
			res.Status = StatusTIMEOUT
			res.Detail = ""
		}
		return res
	}

	res.Status = Status(dns.RcodeToString[resp.Rcode])
	res.Truncated = resp.Truncated
	for _, rr := range resp.Answer {
		switch v := rr.(type) {
		case *dns.A:
			if qtype == dns.TypeA {
				res.IPs = append(res.IPs, v.A.String())
			}
		case *dns.AAAA:
			if qtype == dns.TypeAAAA {
				res.IPs = append(res.IPs, v.AAAA.String())
			}
		}
	}
	return res
}

func isTimeoutErr(err error) bool {
	var nerr net.Error
	return errors.As(err, &nerr) && nerr.Timeout()
}

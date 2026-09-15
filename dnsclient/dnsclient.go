// Package dnsclient implements DNS exchanges over multiple transports with
// application-layer RTT measurement and response status classification.
package dnsclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/miekg/dns"

	"github.com/lxlonion/Dnscheck-tui/config"
)

// Query types used by probes and resolution.
const (
	QTypeA    uint16 = 1
	QTypeAAAA uint16 = 28
)

// Status is the classification of a single DNS exchange outcome.
type Status string

const (
	StatusNOERROR  Status = "NOERROR"
	StatusSERVFAIL Status = "SERVFAIL"
	StatusNXDOMAIN Status = "NXDOMAIN"
	StatusTIMEOUT  Status = "TIMEOUT"
	StatusERROR    Status = "ERROR"
)

// Success reports whether the status represents a valid successful response.
func (s Status) Success() bool { return s == StatusNOERROR }

// Result is the outcome of a single DNS exchange.
type Result struct {
	ServerName string
	Protocol   config.Protocol
	RTT        time.Duration
	Status     Status
	Detail     string
	IPs        []string
	Truncated  bool
}

// Success reports whether this exchange produced a NOERROR response.
func (r Result) Success() bool { return r.Status.Success() }

// Resolver performs a single DNS exchange against one server.
type Resolver interface {
	Exchange(ctx context.Context, server config.DNSServer, question string, qtype uint16) Result
}

// NewResolver returns a Resolver for the given protocol.
func NewResolver(p config.Protocol) (Resolver, error) {
	switch p {
	case config.ProtocolUDP:
		return &classicResolver{net: "udp"}, nil
	case config.ProtocolTCP:
		return &classicResolver{net: "tcp"}, nil
	case config.ProtocolDoT:
		return &classicResolver{net: "tcp-tls"}, nil
	case config.ProtocolDoH:
		return newDoHResolver(), nil
	default:
		return nil, fmt.Errorf("unsupported protocol %q", p)
	}
}

// classifyReply maps a DNS response message to a Status and the answer IPs
// matching the queried type.
func classifyReply(reply *dns.Msg, qtype uint16) (Status, []string) {
	st := Status(dns.RcodeToString[reply.Rcode])
	var ips []string
	for _, rr := range reply.Answer {
		switch v := rr.(type) {
		case *dns.A:
			if qtype == dns.TypeA {
				ips = append(ips, v.A.String())
			}
		case *dns.AAAA:
			if qtype == dns.TypeAAAA {
				ips = append(ips, v.AAAA.String())
			}
		}
	}
	return st, ips
}

// classifyTransportError distinguishes timeouts from other transport errors.
func classifyTransportError(ctx context.Context, err error) (Status, string) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || isTimeoutErr(err) {
		return StatusTIMEOUT, ""
	}
	return StatusERROR, err.Error()
}

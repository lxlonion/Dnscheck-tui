// Package dnsclient implements DNS exchanges over multiple transports with
// application-layer RTT measurement and response status classification.
package dnsclient

import (
	"context"
	"fmt"
	"time"

	"dnscheck/internal/config"
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
	default:
		return nil, fmt.Errorf("unsupported protocol %q", p)
	}
}

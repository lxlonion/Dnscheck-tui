package dnsclient

import (
	"context"
	"sync"
	"time"

	"github.com/lxlonion/Dnscheck-tui/config"
)

// ResolveResult is the outcome of resolving one domain through one DNS
// server (both A and AAAA queries).
type ResolveResult struct {
	Server     config.DNSServer
	Domain     string
	AStatus    Status
	AAAAStatus Status
	Detail     string
	A          []string
	AAAA       []string
}

// Status returns the overall exchange status, preferring the first
// successful family, then the IPv4 status.
func (r ResolveResult) Status() Status {
	if r.AStatus == StatusNOERROR || r.AAAAStatus == StatusNOERROR {
		return StatusNOERROR
	}
	if r.AStatus != "" {
		return r.AStatus
	}
	return r.AAAAStatus
}

// Empty reports whether the resolution produced no IP addresses.
func (r ResolveResult) Empty() bool {
	return len(r.A) == 0 && len(r.AAAA) == 0
}

// DomainResult holds the per-server resolutions of one domain.
type DomainResult struct {
	Domain  string
	Entries []ResolveResult
}

// ResolveServer resolves a domain through one server via its configured
// protocol, querying both A and AAAA with independent timeouts.
func ResolveServer(ctx context.Context, server config.DNSServer, domain string, timeout time.Duration) ResolveResult {
	res, err := NewResolver(server.Protocol)
	if err != nil {
		st := StatusERROR
		return ResolveResult{
			Server: server, Domain: domain,
			AStatus: st, AAAAStatus: st, Detail: err.Error(),
		}
	}

	rr := ResolveResult{Server: server, Domain: domain}
	families := []struct {
		qtype uint16
		ips   *[]string
		st    *Status
	}{
		{QTypeA, &rr.A, &rr.AStatus},
		{QTypeAAAA, &rr.AAAA, &rr.AAAAStatus},
	}
	for _, f := range families {
		if ctx.Err() != nil {
			*f.st = StatusERROR
			break
		}
		actx, cancel := context.WithTimeout(ctx, timeout)
		r := res.Exchange(actx, server, domain, f.qtype)
		cancel()
		*f.st = r.Status
		*f.ips = r.IPs
		if r.Status == StatusERROR && rr.Detail == "" {
			rr.Detail = r.Detail
		}
	}
	return rr
}

// ResolveAll resolves every domain through every server concurrently and
// returns results in (domain, server) input order.
func ResolveAll(ctx context.Context, servers []config.DNSServer, domains []string, timeout time.Duration) []DomainResult {
	out := make([]DomainResult, len(domains))
	for i, d := range domains {
		out[i] = DomainResult{Domain: d, Entries: make([]ResolveResult, len(servers))}
	}

	var wg sync.WaitGroup
	for di := range domains {
		for si := range servers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out[di].Entries[si] = ResolveServer(ctx, servers[si], domains[di], timeout)
			}()
		}
	}
	wg.Wait()
	return out
}

// CollectIPs gathers every resolved IP (A and AAAA) from ResolveAll output.
func CollectIPs(results []DomainResult) []string {
	var ips []string
	for _, dr := range results {
		for _, e := range dr.Entries {
			ips = append(ips, e.A...)
			ips = append(ips, e.AAAA...)
		}
	}
	return ips
}

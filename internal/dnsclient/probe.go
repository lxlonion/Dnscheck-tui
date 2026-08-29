package dnsclient

import (
	"context"
	"sync"
	"time"

	"dnscheck/internal/config"
)

// ProbeResult aggregates several repeated exchanges against one server.
type ProbeResult struct {
	Server    config.DNSServer
	Attempts  []Result
	Successes int
	Failures  int
	AvgRTT    time.Duration
}

// SuccessRate returns successes / attempts in [0, 1].
func (p ProbeResult) SuccessRate() float64 {
	if len(p.Attempts) == 0 {
		return 0
	}
	return float64(p.Successes) / float64(len(p.Attempts))
}

// ProbeServer queries one server `attempts` times sequentially, each exchange
// bounded by `timeout`. It stops early when ctx is canceled. A single timing
// out or failing server never blocks other callers (see ProbeAll).
func ProbeServer(ctx context.Context, server config.DNSServer, question string, qtype uint16, attempts int, timeout time.Duration) ProbeResult {
	if attempts < 1 {
		attempts = 1
	}
	res, err := NewResolver(server.Protocol)
	if err != nil {
		return ProbeResult{
			Server:    server,
			Attempts:  []Result{{ServerName: server.Name, Protocol: server.Protocol, Status: StatusERROR, Detail: err.Error()}},
			Successes: 0,
			Failures:  1,
		}
	}

	pr := ProbeResult{Server: server}
	var rttSum time.Duration
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			break
		}
		actx, cancel := context.WithTimeout(ctx, timeout)
		r := res.Exchange(actx, server, question, qtype)
		cancel()
		pr.Attempts = append(pr.Attempts, r)
		if r.Success() {
			pr.Successes++
			rttSum += r.RTT
		} else {
			pr.Failures++
		}
	}
	if pr.Successes > 0 {
		pr.AvgRTT = rttSum / time.Duration(pr.Successes)
	}
	return pr
}

// ProbeAll probes every server concurrently and returns results in input
// order. ctx cancellation aborts pending work on all servers.
func ProbeAll(ctx context.Context, servers []config.DNSServer, question string, qtype uint16, attempts int, timeout time.Duration) []ProbeResult {
	out := make([]ProbeResult, len(servers))
	var wg sync.WaitGroup
	for i, s := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = ProbeServer(ctx, s, question, qtype, attempts, timeout)
		}()
	}
	wg.Wait()
	return out
}

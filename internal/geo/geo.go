// Package geo resolves IP geolocation (country, city, ISP) through the
// ip-api.com HTTP API. Lookups are deduplicated through a process-lifetime
// in-memory cache and can be rate-limited through a worker pool; the cache
// is destroyed with the process and never persisted.
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Info is the geolocation of one IP address.
type Info struct {
	IP          string
	Country     string
	CountryCode string
	City        string
	ISP         string
	Err         error
}

// Location renders "Country · City" (or just the country when the city is
// unknown); empty when nothing is known.
func (i Info) Location() string {
	switch {
	case i.Country == "":
		return ""
	case i.City == "":
		return i.Country
	default:
		return i.Country + " · " + i.City
	}
}

// Client queries the geo API with an in-memory cache.
type Client struct {
	endpoint string
	http     *http.Client

	mu    sync.RWMutex
	cache map[string]Info
}

// NewClient returns a client pointed at the public ip-api.com endpoint.
func NewClient() *Client {
	return &Client{
		endpoint: "http://ip-api.com/json/",
		http:     &http.Client{Timeout: 10 * time.Second},
		cache:    make(map[string]Info),
	}
}

// NewClientWithEndpoint returns a client pointed at a custom API endpoint
// of the same "<base>/<ip>" shape (used by tests to stay offline).
func NewClientWithEndpoint(endpoint string) *Client {
	c := NewClient()
	c.endpoint = endpoint
	return c
}

// Lookup returns the geo info of one IP, serving repeated calls from the
// cache. Transport failures are not cached (they can be retried); definitive
// API answers (including "private range" style failures) are cached.
func (c *Client) Lookup(ctx context.Context, ip string) Info {
	c.mu.RLock()
	info, ok := c.cache[ip]
	c.mu.RUnlock()
	if ok {
		return info
	}

	info, definitive := c.fetch(ctx, ip)
	if definitive {
		c.mu.Lock()
		c.cache[ip] = info
		c.mu.Unlock()
	}
	return info
}

func (c *Client) fetch(ctx context.Context, ip string) (Info, bool) {
	info := Info{IP: ip}
	url := c.endpoint + ip + "?fields=status,message,country,countryCode,city,isp"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		info.Err = err
		return info, false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		info.Err = err
		return info, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		info.Err = fmt.Errorf("geo API HTTP status %d", resp.StatusCode)
		return info, false
	}

	var api struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		City        string `json:"city"`
		ISP         string `json:"isp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&api); err != nil {
		info.Err = err
		return info, false
	}
	if api.Status != "success" {
		info.Err = fmt.Errorf("geo API: %s", strings.TrimSpace(api.Message))
		return info, true
	}

	info.Country = api.Country
	info.CountryCode = api.CountryCode
	info.City = api.City
	info.ISP = api.ISP
	return info, true
}

// LookupMany resolves a batch of IPs with at most `workers` concurrent API
// requests (rate-limit protection). Duplicate IPs are requested once. The
// returned map has an entry for every unique input IP.
func (c *Client) LookupMany(ctx context.Context, ips []string, workers int) map[string]Info {
	if workers < 1 {
		workers = 1
	}
	seen := make(map[string]bool, len(ips))
	uniq := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != "" && !seen[ip] {
			seen[ip] = true
			uniq = append(uniq, ip)
		}
	}

	out := make(map[string]Info, len(uniq))
	if len(uniq) == 0 {
		return out
	}

	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ip := range uniq {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			info := c.Lookup(ctx, ip)
			mu.Lock()
			out[ip] = info
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

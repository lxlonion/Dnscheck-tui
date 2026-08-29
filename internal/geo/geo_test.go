package geo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeAPIServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		ip := strings.TrimPrefix(r.URL.Path, "/json/")
		w.Header().Set("Content-Type", "application/json")
		switch ip {
		case "10.0.0.1":
			fmt.Fprint(w, `{"status":"fail","message":"private range","query":"10.0.0.1"}`)
		case "slow-ip":
			time.Sleep(60 * time.Millisecond)
			fmt.Fprint(w, `{"status":"success","country":"Testland","countryCode":"TL","city":"Slowcity","isp":"SlowISP","query":"slow-ip"}`)
		default:
			fmt.Fprintf(w, `{"status":"success","country":"United States","countryCode":"US","city":"Mountain View","isp":"Google LLC","query":"%s"}`, ip)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func newTestClient(t *testing.T) (*Client, *httptest.Server, *atomic.Int32) {
	t.Helper()
	srv, counter := fakeAPIServer(t)
	c := &Client{endpoint: srv.URL + "/json/", http: srv.Client(), cache: make(map[string]Info)}
	return c, srv, counter
}

func TestLookupSuccess(t *testing.T) {
	c, _, _ := newTestClient(t)
	info := c.Lookup(context.Background(), "142.250.71.14")
	if info.Err != nil {
		t.Fatalf("lookup: %v", info.Err)
	}
	if info.Country != "United States" || info.City != "Mountain View" || info.ISP != "Google LLC" {
		t.Errorf("unexpected info: %+v", info)
	}
	if info.CountryCode != "US" {
		t.Errorf("CountryCode = %q", info.CountryCode)
	}
	if loc := info.Location(); loc != "United States · Mountain View" {
		t.Errorf("Location = %q", loc)
	}
}

func TestLookupCachePreventsRepeatRequests(t *testing.T) {
	c, _, counter := newTestClient(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		info := c.Lookup(ctx, "1.1.1.1")
		if info.Err != nil {
			t.Fatalf("lookup %d: %v", i, info.Err)
		}
	}
	if n := counter.Load(); n != 1 {
		t.Errorf("API requests = %d, want 1 (cache must dedupe)", n)
	}
}

func TestLookupDefinitiveFailureCached(t *testing.T) {
	c, _, counter := newTestClient(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		info := c.Lookup(ctx, "10.0.0.1")
		if info.Err == nil {
			t.Fatal("private range must be an error")
		}
	}
	if n := counter.Load(); n != 1 {
		t.Errorf("API requests = %d, want 1 (definitive failures must be cached)", n)
	}
}

func TestTransportFailureNotCached(t *testing.T) {
	c, srv, counter := newTestClient(t)
	c.Lookup(context.Background(), "1.1.1.1")

	srv.Close() // transport now fails
	info := c.Lookup(context.Background(), "2.2.2.2")
	if info.Err == nil {
		t.Fatal("transport failure expected")
	}
	info = c.Lookup(context.Background(), "2.2.2.2")
	if info.Err == nil {
		t.Fatal("transport failure expected again")
	}
	if n := counter.Load(); n != 1 {
		t.Errorf("API requests = %d, want 1 (successful 1.1.1.1 only; failures must not add)", n)
	}
	if len(c.cache) != 1 {
		t.Errorf("cache = %d entries, want 1 (only the definitive success)", len(c.cache))
	}
}

func TestLookupManyWorkerLimit(t *testing.T) {
	var requests atomic.Int32
	var inflight, maxInflight atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		cur := inflight.Add(1)
		for {
			old := maxInflight.Load()
			if cur <= old || maxInflight.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		inflight.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","country":"X","city":"Y","isp":"Z"}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{endpoint: srv.URL + "/json/", http: srv.Client(), cache: make(map[string]Info)}

	ips := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4", "5.5.5.5", "6.6.6.6", "7.7.7.7", "8.8.8.8"}
	infos := c.LookupMany(context.Background(), ips, 2)
	if len(infos) != len(ips) {
		t.Fatalf("infos = %d, want %d", len(infos), len(ips))
	}
	if m := maxInflight.Load(); m > 2 {
		t.Errorf("max concurrent API requests = %d, want <= 2 (worker pool limit)", m)
	}
	if n := requests.Load(); n != int32(len(ips)) {
		t.Errorf("API requests = %d, want %d", n, len(ips))
	}
}

func TestLookupManyDeduplicates(t *testing.T) {
	c, _, counter := newTestClient(t)
	ips := []string{"9.9.9.9", "9.9.9.9", "9.9.9.9", "8.8.8.8"}
	infos := c.LookupMany(context.Background(), ips, 4)
	if len(infos) != 2 {
		t.Fatalf("infos = %d, want 2 unique", len(infos))
	}
	if n := counter.Load(); n != 2 {
		t.Errorf("API requests = %d, want 2", n)
	}
}

func TestLookupManyEmpty(t *testing.T) {
	c, _, counter := newTestClient(t)
	infos := c.LookupMany(context.Background(), nil, 4)
	if len(infos) != 0 {
		t.Errorf("infos = %d, want 0", len(infos))
	}
	if n := counter.Load(); n != 0 {
		t.Errorf("API requests = %d, want 0", n)
	}
}

func TestLookupCtxCancel(t *testing.T) {
	c, _, _ := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info := c.Lookup(ctx, "1.1.1.1")
	if info.Err == nil {
		t.Fatal("canceled context must produce an error")
	}
}

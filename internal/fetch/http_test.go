package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xTraceSentinel/MediaScraper/internal/proxy"
)

var fastRetry = RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}

func newTestFetcher(t *testing.T, opts Options) *HTTPFetcher {
	t.Helper()
	if opts.Retry.MaxAttempts == 0 {
		opts.Retry = fastRetry
	}
	f, err := NewHTTPFetcher(opts)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFetchSuccessSetsHeaders(t *testing.T) {
	var gotUA, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotCustom = r.UserAgent(), r.Header.Get("X-Test")
		fmt.Fprint(w, "<html>ok</html>")
	}))
	defer srv.Close()

	f := newTestFetcher(t, Options{UserAgents: StaticUserAgent("test-agent/1.0"), Headers: map[string]string{"X-Test": "yes"}})
	resp, err := f.Fetch(context.Background(), srv.URL+"/page")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "<html>ok</html>" || resp.StatusCode != 200 || resp.Attempts != 1 {
		t.Errorf("unexpected response: %+v", resp)
	}
	if gotUA != "test-agent/1.0" || gotCustom != "yes" {
		t.Errorf("headers: UA=%q X-Test=%q", gotUA, gotCustom)
	}
}

func TestFetchRetriesTransientStatus(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	start := time.Now()
	resp, err := newTestFetcher(t, Options{}).Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", resp.Attempts)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("Retry-After was not capped by MaxDelay")
	}
}

func TestFetchDoesNotRetryPermanentStatus(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	resp, err := newTestFetcher(t, Options{}).Fetch(context.Background(), srv.URL)
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != 404 {
		t.Fatalf("err = %v, want 404 StatusError", err)
	}
	if resp == nil || resp.StatusCode != 404 {
		t.Errorf("response should accompany StatusError, got %+v", resp)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestFetchGivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := newTestFetcher(t, Options{}).Fetch(context.Background(), srv.URL)
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != 502 {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}

func TestFetchBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 100))
	}))
	defer srv.Close()

	_, err := newTestFetcher(t, Options{MaxBodyBytes: 10}).Fetch(context.Background(), srv.URL)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("err = %v, want ErrBodyTooLarge", err)
	}
}

func TestFetchRejectsBadURLs(t *testing.T) {
	f := newTestFetcher(t, Options{})
	for _, u := range []string{"ftp://x.test/", "http://", "::nope"} {
		if _, err := f.Fetch(context.Background(), u); err == nil {
			t.Errorf("Fetch(%q) should fail", u)
		}
	}
}

func TestFetchRespectsRobots(t *testing.T) {
	var robotsCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsCalls.Add(1)
			fmt.Fprint(w, "User-agent: *\nDisallow: /private\n")
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	f := newTestFetcher(t, Options{RespectRobots: true})
	if _, err := f.Fetch(context.Background(), srv.URL+"/private/page"); !errors.Is(err, ErrDisallowed) {
		t.Errorf("err = %v, want ErrDisallowed", err)
	}
	if _, err := f.Fetch(context.Background(), srv.URL+"/public"); err != nil {
		t.Errorf("public page: %v", err)
	}
	if robotsCalls.Load() != 1 {
		t.Errorf("robots.txt fetched %d times, want 1 (cached)", robotsCalls.Load())
	}
}

func TestFetchMissingRobotsAllowsAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	if _, err := newTestFetcher(t, Options{RespectRobots: true}).Fetch(context.Background(), srv.URL+"/x"); err != nil {
		t.Fatal(err)
	}
}

type recordingProvider struct {
	proxy *url.URL
	mu    sync.Mutex
	errs  []error
}

func (p *recordingProvider) Next(context.Context, *url.URL) (*url.URL, error) { return p.proxy, nil }
func (p *recordingProvider) Report(_ *url.URL, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errs = append(p.errs, err)
}

func TestFetchRoutesThroughProxyAndReports(t *testing.T) {
	var seenHost string
	blocked := true
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHost = r.URL.Host
		if blocked {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "via proxy")
	}))
	defer proxySrv.Close()

	pu, _ := url.Parse(proxySrv.URL)
	rec := &recordingProvider{proxy: pu}
	f := newTestFetcher(t, Options{Proxy: rec, Retry: RetryPolicy{MaxAttempts: 1}})

	_, err := f.Fetch(context.Background(), "http://shop.invalid/item")
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != 403 {
		t.Fatalf("err = %v, want 403", err)
	}
	if seenHost != "shop.invalid" {
		t.Errorf("proxy saw host %q", seenHost)
	}

	blocked = false
	resp, err := f.Fetch(context.Background(), "http://shop.invalid/item")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "via proxy" || resp.Proxy == "" {
		t.Errorf("resp = %q proxy=%q", resp.Body, resp.Proxy)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.errs) != 2 || rec.errs[0] == nil || rec.errs[1] != nil {
		t.Errorf("reports = %v, want [403-error, nil]", rec.errs)
	}
}

func TestFetchHonorsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	f := newTestFetcher(t, Options{Retry: RetryPolicy{MaxAttempts: 5, BaseDelay: time.Hour, MaxDelay: time.Hour}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.Fetch(ctx, srv.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := map[string]time.Duration{
		"":                              0,
		"5":                             5 * time.Second,
		"-1":                            0,
		"soon":                          0,
		"Thu, 01 Jan 2026 00:00:30 GMT": 30 * time.Second,
		"Wed, 31 Dec 2025 23:00:00 GMT": 0,
	}
	for in, want := range tests {
		if got := parseRetryAfter(in, now); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestHostLimiterCrawlDelayOnlySlowsDown(t *testing.T) {
	h := newHostLimiter(10, 1)
	h.AtMost("a", time.Second)
	if got := h.get("a").Limit(); got != 1 {
		t.Errorf("limit = %v, want 1/s", got)
	}
	h.AtMost("a", time.Millisecond)
	if got := h.get("a").Limit(); got != 1 {
		t.Errorf("limit = %v, want unchanged 1/s", got)
	}
	if got := h.get("b").Limit(); got != 10 {
		t.Errorf("other host limit = %v, want 10", got)
	}
}

var _ proxy.Provider = (*recordingProvider)(nil)

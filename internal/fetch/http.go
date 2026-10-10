package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/temoto/robotstxt"

	"github.com/0xTraceSentinel/MediaScraper/internal/proxy"
)

type RetryPolicy struct {
	MaxAttempts   int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	RetryStatuses []int
}

var DefaultRetryStatuses = []int{
	http.StatusRequestTimeout,
	http.StatusTooEarly,
	http.StatusTooManyRequests,
	http.StatusInternalServerError,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

var blockedStatuses = []int{
	http.StatusForbidden,
	http.StatusProxyAuthRequired,
	http.StatusTooManyRequests,
}

type Options struct {
	Timeout           time.Duration
	MaxBodyBytes      int64
	Headers           map[string]string
	UserAgents        UserAgentPicker
	Proxy             proxy.Provider
	RequestsPerSecond float64
	Burst             int
	Retry             RetryPolicy
	RespectRobots     bool
	RobotsAgent       string
	Transport         http.RoundTripper
	Logger            *slog.Logger
}

type HTTPFetcher struct {
	client  *http.Client
	opts    Options
	limiter *hostLimiter
	robots  *robotsCache
	log     *slog.Logger
}

var _ Fetcher = (*HTTPFetcher)(nil)

func NewHTTPFetcher(opts Options) (*HTTPFetcher, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 10 << 20
	}
	if opts.UserAgents == nil {
		opts.UserAgents = RandomUserAgent(DefaultUserAgents)
	}
	if opts.Proxy == nil {
		opts.Proxy = proxy.Direct{}
	}
	if opts.Retry.MaxAttempts <= 0 {
		opts.Retry.MaxAttempts = 3
	}
	if opts.Retry.BaseDelay <= 0 {
		opts.Retry.BaseDelay = 500 * time.Millisecond
	}
	if opts.Retry.MaxDelay <= 0 {
		opts.Retry.MaxDelay = 30 * time.Second
	}
	if opts.Retry.RetryStatuses == nil {
		opts.Retry.RetryStatuses = DefaultRetryStatuses
	}
	if opts.RobotsAgent == "" {
		opts.RobotsAgent = "MediaScraper"
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}

	transport := opts.Transport
	if transport == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = proxy.FromRequest
		t.MaxIdleConnsPerHost = 10
		transport = t
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("fetch: cookie jar: %w", err)
	}

	f := &HTTPFetcher{
		client:  &http.Client{Transport: transport, Jar: jar, Timeout: opts.Timeout},
		opts:    opts,
		limiter: newHostLimiter(opts.RequestsPerSecond, opts.Burst),
		log:     opts.Logger,
	}
	if opts.RespectRobots {
		f.robots = newRobotsCache(opts.RobotsAgent, f.fetch, func(host string, g *robotstxt.Group) {
			if g != nil && g.CrawlDelay > 0 {
				f.log.Debug("applying robots.txt crawl-delay", "host", host, "delay", g.CrawlDelay)
				f.limiter.AtMost(host, g.CrawlDelay)
			}
		})
	}
	return f, nil
}

func (f *HTTPFetcher) Fetch(ctx context.Context, rawURL string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch: parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("fetch: unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("fetch: url %q has no host", rawURL)
	}
	if f.robots != nil && !f.robots.Allowed(ctx, u) {
		return nil, ErrDisallowed
	}
	return f.fetch(ctx, u)
}

func (f *HTTPFetcher) fetch(ctx context.Context, u *url.URL) (*Response, error) {
	start := time.Now()
	var lastErr error
	for attempt := 1; attempt <= f.opts.Retry.MaxAttempts; attempt++ {
		if err := f.limiter.Wait(ctx, u.Host); err != nil {
			return nil, err
		}
		p, err := f.opts.Proxy.Next(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("fetch: select proxy: %w", err)
		}

		resp, retryAfter, err := f.do(ctx, u, p)
		f.opts.Proxy.Report(p, proxyOutcome(resp, err))

		if err == nil {
			resp.Attempts = attempt
			resp.Duration = time.Since(start)
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return resp, nil
			}
			lastErr = &StatusError{StatusCode: resp.StatusCode, URL: u.String()}
			if !slices.Contains(f.opts.Retry.RetryStatuses, resp.StatusCode) || attempt == f.opts.Retry.MaxAttempts {
				return resp, lastErr
			}
		} else {
			lastErr = err
			if !retryable(ctx, err) {
				return nil, err
			}
		}

		if attempt == f.opts.Retry.MaxAttempts {
			break
		}
		delay := f.backoff(attempt, retryAfter)
		f.log.Debug("retrying request", "url", u.String(), "attempt", attempt, "delay", delay, "error", lastErr)
		if err := sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (f *HTTPFetcher) do(ctx context.Context, u *url.URL, p *url.URL) (*Response, time.Duration, error) {
	req, err := http.NewRequestWithContext(proxy.WithProxy(ctx, p), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	for k, v := range f.opts.Headers {
		req.Header.Set(k, v)
	}
	if ua := f.opts.UserAgents.UserAgent(); ua != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", ua)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.opts.MaxBodyBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("fetch: read body: %w", err)
	}
	if int64(len(body)) > f.opts.MaxBodyBytes {
		return nil, 0, ErrBodyTooLarge
	}

	out := &Response{
		URL:        resp.Request.URL,
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       body,
	}
	if p != nil {
		out.Proxy = p.Redacted()
	}
	return out, parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()), nil
}

func (f *HTTPFetcher) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, f.opts.Retry.MaxDelay)
	}
	d := f.opts.Retry.BaseDelay << (attempt - 1)
	if d <= 0 || d > f.opts.Retry.MaxDelay {
		d = f.opts.Retry.MaxDelay
	}
	half := d / 2
	return half + rand.N(half+1)
}

func proxyOutcome(resp *Response, err error) error {
	if err != nil {
		if errors.Is(err, ErrBodyTooLarge) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if slices.Contains(blockedStatuses, resp.StatusCode) {
		return &StatusError{StatusCode: resp.StatusCode, URL: resp.URL.String()}
	}
	return nil
}

func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, ErrBodyTooLarge) {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		var netErr interface{ Timeout() bool }
		return urlErr.Timeout() || errors.As(urlErr.Err, &netErr) || errors.Is(urlErr.Err, io.ErrUnexpectedEOF) || errors.Is(urlErr.Err, io.EOF)
	}
	return true
}

func parseRetryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

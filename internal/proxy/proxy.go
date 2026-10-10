package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var ErrNoProxyAvailable = errors.New("proxy: no healthy proxy available")

type Provider interface {
	Next(ctx context.Context, target *url.URL) (*url.URL, error)
	Report(p *url.URL, err error)
}

type Direct struct{}

func (Direct) Next(context.Context, *url.URL) (*url.URL, error) { return nil, nil }

func (Direct) Report(*url.URL, error) {}

type ctxKey struct{}

func WithProxy(ctx context.Context, p *url.URL) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func FromRequest(r *http.Request) (*url.URL, error) {
	p, _ := r.Context().Value(ctxKey{}).(*url.URL)
	return p, nil
}

func Parse(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("proxy: empty address")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("proxy: parse %q: %w", Redact(raw), err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("proxy: unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" || u.Port() == "" {
		return nil, fmt.Errorf("proxy: %q must include host and port", u.Redacted())
	}
	return u, nil
}

func ParseList(lines []string) ([]*url.URL, error) {
	var out []*url.URL
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := Parse(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out = append(out, u)
	}
	return out, nil
}

func Redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid proxy>"
	}
	return u.Redacted()
}

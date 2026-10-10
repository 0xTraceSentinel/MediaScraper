package proxy

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"time"
)

type RotatingOptions struct {
	MaxFailures int
	Cooldown    time.Duration
	Now         func() time.Time
}

type proxyState struct {
	url           *url.URL
	failures      int
	cooldownUntil time.Time
}

type Rotating struct {
	mu      sync.Mutex
	proxies []*proxyState
	index   map[string]*proxyState
	next    int
	opts    RotatingOptions
}

func NewRotating(proxies []*url.URL, opts RotatingOptions) (*Rotating, error) {
	if len(proxies) == 0 {
		return nil, errors.New("proxy: rotating provider requires at least one proxy")
	}
	if opts.MaxFailures <= 0 {
		opts.MaxFailures = 3
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	r := &Rotating{opts: opts, index: make(map[string]*proxyState, len(proxies))}
	for _, p := range proxies {
		s := &proxyState{url: p}
		r.proxies = append(r.proxies, s)
		r.index[p.String()] = s
	}
	return r, nil
}

func (r *Rotating) Next(ctx context.Context, _ *url.URL) (*url.URL, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.opts.Now()
	for range r.proxies {
		s := r.proxies[r.next]
		r.next = (r.next + 1) % len(r.proxies)
		if now.Before(s.cooldownUntil) {
			continue
		}
		return s.url, nil
	}
	return nil, ErrNoProxyAvailable
}

func (r *Rotating) Report(p *url.URL, err error) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.index[p.String()]
	if !ok {
		return
	}
	if err == nil {
		s.failures = 0
		return
	}
	s.failures++
	if s.failures >= r.opts.MaxFailures {
		s.cooldownUntil = r.opts.Now().Add(r.opts.Cooldown)
		s.failures = 0
	}
}

func (r *Rotating) Len() int { return len(r.proxies) }

package fetch

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type hostLimiter struct {
	mu       sync.Mutex
	limit    rate.Limit
	burst    int
	limiters map[string]*rate.Limiter
}

func newHostLimiter(rps float64, burst int) *hostLimiter {
	l := rate.Inf
	if rps > 0 {
		l = rate.Limit(rps)
	}
	if burst <= 0 {
		burst = 1
	}
	return &hostLimiter{limit: l, burst: burst, limiters: make(map[string]*rate.Limiter)}
}

func (h *hostLimiter) get(host string) *rate.Limiter {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, ok := h.limiters[host]
	if !ok {
		l = rate.NewLimiter(h.limit, h.burst)
		h.limiters[host] = l
	}
	return l
}

func (h *hostLimiter) Wait(ctx context.Context, host string) error {
	return h.get(host).Wait(ctx)
}

func (h *hostLimiter) AtMost(host string, interval time.Duration) {
	if interval <= 0 {
		return
	}
	l := h.get(host)
	if want := rate.Every(interval); want < l.Limit() {
		l.SetLimit(want)
		l.SetBurst(1)
	}
}

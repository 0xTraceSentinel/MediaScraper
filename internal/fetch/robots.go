package fetch

import (
	"context"
	"errors"
	"net/url"
	"sync"

	"github.com/temoto/robotstxt"
)

type robotsCache struct {
	agent  string
	fetch  func(ctx context.Context, u *url.URL) (*Response, error)
	onLoad func(host string, g *robotstxt.Group)

	mu      sync.Mutex
	entries map[string]*robotsEntry
}

type robotsEntry struct {
	once sync.Once
	data *robotstxt.RobotsData
}

func newRobotsCache(agent string, fetch func(context.Context, *url.URL) (*Response, error), onLoad func(string, *robotstxt.Group)) *robotsCache {
	return &robotsCache{agent: agent, fetch: fetch, onLoad: onLoad, entries: make(map[string]*robotsEntry)}
}

func (c *robotsCache) Allowed(ctx context.Context, u *url.URL) bool {
	origin := u.Scheme + "://" + u.Host

	c.mu.Lock()
	e, ok := c.entries[origin]
	if !ok {
		e = &robotsEntry{}
		c.entries[origin] = e
	}
	c.mu.Unlock()

	e.once.Do(func() {
		e.data = c.load(ctx, u)
		if c.onLoad != nil {
			c.onLoad(u.Host, e.data.FindGroup(c.agent))
		}
	})
	return e.data.TestAgent(u.RequestURI(), c.agent)
}

func (c *robotsCache) load(ctx context.Context, u *url.URL) *robotstxt.RobotsData {
	robotsURL := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/robots.txt"}
	resp, err := c.fetch(ctx, robotsURL)
	var statusErr *StatusError
	if err != nil && !errors.As(err, &statusErr) {
		data, _ := robotstxt.FromStatusAndBytes(500, nil)
		return data
	}
	data, err := robotstxt.FromStatusAndBytes(resp.StatusCode, resp.Body)
	if err != nil {
		data, _ = robotstxt.FromStatusAndBytes(404, nil)
	}
	return data
}

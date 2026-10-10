package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) (*Response, error)
}

type Response struct {
	URL        *url.URL
	StatusCode int
	Header     http.Header
	Body       []byte
	Attempts   int
	Proxy      string
	Duration   time.Duration
}

var (
	ErrDisallowed   = errors.New("fetch: disallowed by robots.txt")
	ErrBodyTooLarge = errors.New("fetch: response body exceeds limit")
)

type StatusError struct {
	StatusCode int
	URL        string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("fetch: %s returned HTTP %d", e.URL, e.StatusCode)
}

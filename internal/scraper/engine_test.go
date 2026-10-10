package scraper

import (
	"context"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xTraceSentinel/MediaScraper/internal/extract"
	"github.com/0xTraceSentinel/MediaScraper/internal/fetch"
)

type fakeFetcher struct {
	pages map[string]string
	delay time.Duration
	calls atomic.Int32
	mu    sync.Mutex
	seen  []string
}

func (f *fakeFetcher) Fetch(ctx context.Context, rawURL string) (*fetch.Response, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.seen = append(f.seen, rawURL)
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	u, _ := url.Parse(rawURL)
	body, ok := f.pages[rawURL]
	if !ok {
		return &fetch.Response{URL: u, StatusCode: 404}, &fetch.StatusError{StatusCode: 404, URL: rawURL}
	}
	return &fetch.Response{URL: u, StatusCode: 200, Body: []byte(body), Attempts: 1}, nil
}

func site() map[string]string {
	return map[string]string{
		"https://shop.test/": `<a class="p" href="/p/1">1</a><a class="p" href="/p/2#reviews">2</a>
<a class="p" href="/p/1">dup</a><a class="p" href="https://other.test/p/9">ext</a><a class="p" href="/missing">x</a>`,
		"https://shop.test/p/1":  `<script type="application/ld+json">{"@type":"Product","name":"One","offers":{"price":"1.00"}}</script><a class="p" href="/p/3">3</a>`,
		"https://shop.test/p/2":  `<script type="application/ld+json">{"@type":"Product","name":"Two","offers":{"price":"2.00"}}</script>`,
		"https://shop.test/p/3":  `<script type="application/ld+json">{"@type":"Product","name":"Three","offers":{"price":"3.00"}}</script>`,
		"https://other.test/p/9": `<script type="application/ld+json">{"@type":"Product","name":"Nine","offers":{"price":"9.00"}}</script>`,
	}
}

func runEngine(t *testing.T, f fetch.Fetcher, opts Options, seeds ...string) []Result {
	t.Helper()
	pl, err := extract.NewPipeline(nil, []string{"a.p"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := New(f, pl, opts, nil).Run(context.Background(), seeds)
	if err != nil {
		t.Fatal(err)
	}
	var out []Result
	for r := range ch {
		out = append(out, r)
	}
	return out
}

func urlsOf(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.URL)
	}
	slices.Sort(out)
	return out
}

func TestEngineSeedsOnlyAtDepthZero(t *testing.T) {
	res := runEngine(t, &fakeFetcher{pages: site()}, Options{Concurrency: 2}, "https://shop.test/p/1")
	if len(res) != 1 || res[0].Products[0].Name != "One" {
		t.Fatalf("results = %+v", res)
	}
	if res[0].LinksFound != 1 {
		t.Errorf("LinksFound = %d", res[0].LinksFound)
	}
}

func TestEngineFollowsLinksWithDedupAndSameHost(t *testing.T) {
	res := runEngine(t, &fakeFetcher{pages: site()}, Options{Concurrency: 3, MaxDepth: 1, SameHost: true}, "https://shop.test/")
	want := []string{"https://shop.test/", "https://shop.test/missing", "https://shop.test/p/1", "https://shop.test/p/2"}
	if got := urlsOf(res); !slices.Equal(got, want) {
		t.Fatalf("urls = %v, want %v", got, want)
	}
	for _, r := range res {
		switch r.URL {
		case "https://shop.test/missing":
			if r.OK() || r.StatusCode != 404 {
				t.Errorf("missing page: %+v", r)
			}
		case "https://shop.test/p/2":
			if r.Depth != 1 || len(r.Products) != 1 {
				t.Errorf("p/2: %+v", r)
			}
		}
	}
}

func TestEngineDepthTwoAndOtherHosts(t *testing.T) {
	res := runEngine(t, &fakeFetcher{pages: site()}, Options{Concurrency: 2, MaxDepth: 2, SameHost: false}, "https://shop.test/")
	got := urlsOf(res)
	for _, u := range []string{"https://shop.test/p/3", "https://other.test/p/9"} {
		if !slices.Contains(got, u) {
			t.Errorf("expected %s in %v", u, got)
		}
	}
}

func TestEngineMaxPages(t *testing.T) {
	f := &fakeFetcher{pages: site()}
	res := runEngine(t, f, Options{Concurrency: 4, MaxDepth: 5, MaxPages: 2}, "https://shop.test/")
	if len(res) != 2 || f.calls.Load() != 2 {
		t.Fatalf("got %d results / %d fetches, want 2", len(res), f.calls.Load())
	}
}

func TestEngineDedupesSeeds(t *testing.T) {
	f := &fakeFetcher{pages: site()}
	res := runEngine(t, f, Options{Concurrency: 2}, "https://shop.test/p/1", "https://SHOP.test/p/1#x")
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1", len(res))
	}
}

func TestEngineRejectsInvalidSeeds(t *testing.T) {
	pl, _ := extract.NewPipeline(nil, nil)
	e := New(&fakeFetcher{}, pl, Options{}, nil)
	for _, seeds := range [][]string{nil, {"not a url"}, {"ftp://x.test/"}} {
		if _, err := e.Run(context.Background(), seeds); err == nil {
			t.Errorf("Run(%v) should fail", seeds)
		}
	}
}

func TestEngineCancellationTerminates(t *testing.T) {
	pages := map[string]string{}
	var seeds []string
	for i := range 50 {
		u := "https://shop.test/p/" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		pages[u] = "<p>x</p>"
		seeds = append(seeds, u)
	}
	f := &fakeFetcher{pages: pages, delay: 20 * time.Millisecond}
	pl, _ := extract.NewPipeline(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := New(f, pl, Options{Concurrency: 2}, nil).Run(ctx, seeds)
	if err != nil {
		t.Fatal(err)
	}
	<-ch
	cancel()

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not shut down after cancellation")
	}
	if n := f.calls.Load(); n >= 50 {
		t.Errorf("fetched %d pages after cancellation; dispatch should stop", n)
	}
}

func TestCanonical(t *testing.T) {
	a, _ := url.Parse("HTTPS://Shop.Test#frag")
	b, _ := url.Parse("https://shop.test/")
	if canonical(a) != canonical(b) {
		t.Errorf("%q != %q", canonical(a), canonical(b))
	}
}

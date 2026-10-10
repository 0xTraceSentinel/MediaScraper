package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{in: "http://user:pass@proxy.local:3128", want: "http://user:pass@proxy.local:3128"},
		{in: "socks5://10.0.0.1:1080", want: "socks5://10.0.0.1:1080"},
		{in: "ftp://proxy.local:21", wantErr: true},
		{in: "proxy.local", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseListSkipsCommentsAndBlanks(t *testing.T) {
	got, err := ParseList([]string{"# comment", "", "  1.2.3.4:80  ", "socks5://5.6.7.8:1080"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d proxies, want 2", len(got))
	}
}

func TestRedactHidesPassword(t *testing.T) {
	got := Redact("http://user:secret@proxy.local:3128")
	if got != "http://user:xxxxx@proxy.local:3128" {
		t.Errorf("Redact = %q", got)
	}
}

func mustURLs(t *testing.T, raws ...string) []*url.URL {
	t.Helper()
	out, err := ParseList(raws)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRotatingRoundRobin(t *testing.T) {
	r, err := NewRotating(mustURLs(t, "a.local:1", "b.local:1", "c.local:1"), RotatingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.local", "b.local", "c.local", "a.local"}
	for i, w := range want {
		p, err := r.Next(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.Hostname() != w {
			t.Errorf("call %d: got %s, want %s", i, p.Hostname(), w)
		}
	}
}

func TestRotatingCooldown(t *testing.T) {
	now := time.Unix(0, 0)
	r, err := NewRotating(mustURLs(t, "a.local:1", "b.local:1"), RotatingOptions{
		MaxFailures: 2,
		Cooldown:    time.Minute,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := r.Next(context.Background(), nil)
	r.Report(a, errors.New("boom"))
	r.Report(a, errors.New("boom"))

	for i := 0; i < 3; i++ {
		p, err := r.Next(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.Hostname() != "b.local" {
			t.Fatalf("got %s during cooldown, want b.local", p.Hostname())
		}
	}

	b, _ := r.Next(context.Background(), nil)
	r.Report(b, errors.New("boom"))
	r.Report(b, errors.New("boom"))
	if _, err := r.Next(context.Background(), nil); !errors.Is(err, ErrNoProxyAvailable) {
		t.Fatalf("err = %v, want ErrNoProxyAvailable", err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := r.Next(context.Background(), nil); err != nil {
		t.Fatalf("after cooldown: %v", err)
	}
}

func TestRotatingSuccessResetsFailures(t *testing.T) {
	r, _ := NewRotating(mustURLs(t, "a.local:1"), RotatingOptions{MaxFailures: 2})
	a, _ := r.Next(context.Background(), nil)
	r.Report(a, errors.New("boom"))
	r.Report(a, nil)
	r.Report(a, errors.New("boom"))
	if _, err := r.Next(context.Background(), nil); err != nil {
		t.Fatalf("proxy should still be healthy: %v", err)
	}
}

func TestFromRequest(t *testing.T) {
	p := mustURLs(t, "a.local:1")[0]
	req, _ := http.NewRequestWithContext(WithProxy(context.Background(), p), http.MethodGet, "http://example.com", nil)
	got, err := FromRequest(req)
	if err != nil || got != p {
		t.Fatalf("FromRequest = %v, %v", got, err)
	}
	req, _ = http.NewRequest(http.MethodGet, "http://example.com", nil)
	if got, _ := FromRequest(req); got != nil {
		t.Fatalf("FromRequest without proxy = %v, want nil", got)
	}
}

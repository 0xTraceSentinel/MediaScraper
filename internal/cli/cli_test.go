package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xTraceSentinel/MediaScraper/internal/scraper"
)

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Execute(context.Background(), args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func shopServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "User-agent: *\nDisallow: /admin\n")
	})
	mux.HandleFunc("/category", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<a class="card" href="/p/1">1</a><a class="card" href="/p/2">2</a>`)
	})
	mux.HandleFunc("/p/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/p/")
		fmt.Fprintf(w, `<script type="application/ld+json">{"@type":"Product","name":"Item %s","sku":"%s","offers":{"price":"%s.99","priceCurrency":"USD"}}</script>`, id, id, id)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestScrapeEndToEnd(t *testing.T) {
	srv := shopServer(t)
	stdout, stderr, code := run(t, "scrape", srv.URL+"/category",
		"--depth", "1", "--follow", "a.card", "--rps", "0", "--log-level", "warn")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	var products []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var r scraper.Result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad JSONL line %q: %v", line, err)
		}
		for _, p := range r.Products {
			products = append(products, p.Name+"="+p.Price)
		}
	}
	if len(products) != 2 {
		t.Fatalf("products = %v", products)
	}
}

func TestScrapeCSVToFileWithInputList(t *testing.T) {
	srv := shopServer(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "urls.txt")
	outPath := filepath.Join(dir, "out.csv")
	os.WriteFile(input, []byte("# products\n"+srv.URL+"/p/7\n\n"+srv.URL+"/p/8\n"), 0o600)

	_, stderr, code := run(t, "scrape", "-i", input, "-o", outPath, "-f", "csv", "--rps", "0", "--log-level", "error")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Item 7") || !strings.Contains(string(data), "8.99") {
		t.Errorf("csv = %s", data)
	}
}

func TestScrapeRobotsDisallowedFails(t *testing.T) {
	srv := shopServer(t)
	stdout, stderr, code := run(t, "scrape", srv.URL+"/admin", "--rps", "0", "--log-level", "error")
	if code == 0 {
		t.Fatal("expected non-zero exit when every page fails")
	}
	if !strings.Contains(stdout, "robots.txt") || !strings.Contains(stderr, "all 1 pages failed") {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestScrapeRequiresURLs(t *testing.T) {
	_, stderr, code := run(t, "scrape")
	if code == 0 || !strings.Contains(stderr, "no URLs") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestConfigValidate(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(good, []byte("scraper:\n  concurrency: 2\n"), 0o600)
	os.WriteFile(bad, []byte("scraper:\n  concurrency: 0\n"), 0o600)

	if out, stderr, code := run(t, "config", "validate", "--config", good); code != 0 || !strings.Contains(out, "valid") {
		t.Errorf("good config: code=%d out=%q err=%q", code, out, stderr)
	}
	if _, stderr, code := run(t, "config", "validate", "--config", bad); code == 0 || !strings.Contains(stderr, "concurrency") {
		t.Errorf("bad config: code=%d err=%q", code, stderr)
	}
}

func TestShippedExampleConfigIsValid(t *testing.T) {
	out, stderr, code := run(t, "config", "validate", "--config", filepath.Join("..", "..", "configs", "example.yaml"))
	if code != 0 {
		t.Fatalf("configs/example.yaml invalid: %s %s", out, stderr)
	}
}

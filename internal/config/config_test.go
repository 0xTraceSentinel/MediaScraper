package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOverlaysDefaults(t *testing.T) {
	cfg, err := Load(writeFile(t, `
scraper:
  concurrency: 8
http:
  timeout: 5s
  retry:
    statuses: [503]
profiles:
  - name: shop
    domains: [shop.test]
    product:
      fields:
        name: {selector: h1}
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scraper.Concurrency != 8 || cfg.HTTP.Timeout != 5*time.Second {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.HTTP.RequestsPerSecond != 1 || !cfg.HTTP.RespectRobots {
		t.Errorf("defaults lost: %+v", cfg.HTTP)
	}
	if len(cfg.HTTP.Retry.Statuses) != 1 || cfg.HTTP.Retry.Statuses[0] != 503 {
		t.Errorf("statuses = %v", cfg.HTTP.Retry.Statuses)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	_, err := Load(writeFile(t, "scraper:\n  concurency: 8\n"))
	if err == nil || !strings.Contains(err.Error(), "concurency") {
		t.Fatalf("err = %v, want unknown field error", err)
	}
}

func TestLoadEmptyFile(t *testing.T) {
	cfg, err := Load(writeFile(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scraper.Concurrency != Default().Scraper.Concurrency {
		t.Error("empty file should yield defaults")
	}
}

func TestValidateReportsAllErrors(t *testing.T) {
	cfg := Default()
	cfg.Scraper.Concurrency = 0
	cfg.Output.Format = "xml"
	cfg.Log.Level = "loud"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"concurrency", "output.format", "log.level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

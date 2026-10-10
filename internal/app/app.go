package app

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/0xTraceSentinel/MediaScraper/internal/config"
	"github.com/0xTraceSentinel/MediaScraper/internal/extract"
	"github.com/0xTraceSentinel/MediaScraper/internal/fetch"
	"github.com/0xTraceSentinel/MediaScraper/internal/proxy"
	"github.com/0xTraceSentinel/MediaScraper/internal/scraper"
)

func NewLogger(cfg config.LogConfig, w io.Writer) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.Level))
	opts := &slog.HandlerOptions{Level: level}
	if cfg.Format == "json" {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

func NewProxyProvider(cfg config.ProxyConfig) (proxy.Provider, error) {
	lines := append([]string(nil), cfg.URLs...)
	if cfg.File != "" {
		data, err := os.ReadFile(cfg.File)
		if err != nil {
			return nil, fmt.Errorf("read proxy file: %w", err)
		}
		lines = append(lines, strings.Split(string(data), "\n")...)
	}
	urls, err := proxy.ParseList(lines)
	if err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return proxy.Direct{}, nil
	}
	return proxy.NewRotating(urls, proxy.RotatingOptions{
		MaxFailures: cfg.MaxFailures,
		Cooldown:    cfg.Cooldown,
	})
}

func NewFetcher(cfg config.Config, log *slog.Logger) (*fetch.HTTPFetcher, error) {
	provider, err := NewProxyProvider(cfg.Proxy)
	if err != nil {
		return nil, err
	}
	if r, ok := provider.(*proxy.Rotating); ok {
		log.Info("proxy rotation enabled", "proxies", r.Len())
	}

	var ua fetch.UserAgentPicker
	switch len(cfg.HTTP.UserAgents) {
	case 0:
		ua = fetch.RandomUserAgent(fetch.DefaultUserAgents)
	case 1:
		ua = fetch.StaticUserAgent(cfg.HTTP.UserAgents[0])
	default:
		ua = fetch.RandomUserAgent(cfg.HTTP.UserAgents)
	}

	return fetch.NewHTTPFetcher(fetch.Options{
		Timeout:           cfg.HTTP.Timeout,
		MaxBodyBytes:      cfg.HTTP.MaxBodyBytes,
		Headers:           cfg.HTTP.Headers,
		UserAgents:        ua,
		Proxy:             provider,
		RequestsPerSecond: cfg.HTTP.RequestsPerSecond,
		Burst:             cfg.HTTP.Burst,
		RespectRobots:     cfg.HTTP.RespectRobots,
		RobotsAgent:       cfg.HTTP.RobotsAgent,
		Retry: fetch.RetryPolicy{
			MaxAttempts:   cfg.HTTP.Retry.MaxAttempts,
			BaseDelay:     cfg.HTTP.Retry.BaseDelay,
			MaxDelay:      cfg.HTTP.Retry.MaxDelay,
			RetryStatuses: cfg.HTTP.Retry.Statuses,
		},
		Logger: log,
	})
}

func NewEngine(cfg config.Config, log *slog.Logger) (*scraper.Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration:\n%w", err)
	}
	f, err := NewFetcher(cfg, log)
	if err != nil {
		return nil, err
	}
	pipeline, err := extract.NewPipeline(cfg.Profiles, cfg.Scraper.Follow)
	if err != nil {
		return nil, err
	}
	return scraper.New(f, pipeline, scraper.Options{
		Concurrency: cfg.Scraper.Concurrency,
		MaxDepth:    cfg.Scraper.MaxDepth,
		MaxPages:    cfg.Scraper.MaxPages,
		SameHost:    cfg.Scraper.SameHost,
	}, log), nil
}

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/0xTraceSentinel/MediaScraper/internal/extract"
	"github.com/0xTraceSentinel/MediaScraper/internal/fetch"
	"github.com/0xTraceSentinel/MediaScraper/internal/output"
)

type Config struct {
	Scraper  ScraperConfig     `yaml:"scraper"`
	HTTP     HTTPConfig        `yaml:"http"`
	Proxy    ProxyConfig       `yaml:"proxy"`
	Output   OutputConfig      `yaml:"output"`
	Log      LogConfig         `yaml:"log"`
	Profiles []extract.Profile `yaml:"profiles"`
}

type ScraperConfig struct {
	Concurrency int      `yaml:"concurrency"`
	MaxDepth    int      `yaml:"max_depth"`
	MaxPages    int      `yaml:"max_pages"`
	SameHost    bool     `yaml:"same_host"`
	Follow      []string `yaml:"follow"`
}

type HTTPConfig struct {
	Timeout           time.Duration     `yaml:"timeout"`
	MaxBodyBytes      int64             `yaml:"max_body_bytes"`
	RequestsPerSecond float64           `yaml:"requests_per_second"`
	Burst             int               `yaml:"burst"`
	UserAgents        []string          `yaml:"user_agents"`
	Headers           map[string]string `yaml:"headers"`
	RespectRobots     bool              `yaml:"respect_robots"`
	RobotsAgent       string            `yaml:"robots_agent"`
	Retry             RetryConfig       `yaml:"retry"`
}

type RetryConfig struct {
	MaxAttempts int           `yaml:"max_attempts"`
	BaseDelay   time.Duration `yaml:"base_delay"`
	MaxDelay    time.Duration `yaml:"max_delay"`
	Statuses    []int         `yaml:"statuses"`
}

type ProxyConfig struct {
	URLs        []string      `yaml:"urls"`
	File        string        `yaml:"file"`
	MaxFailures int           `yaml:"max_failures"`
	Cooldown    time.Duration `yaml:"cooldown"`
}

type OutputConfig struct {
	Format output.Format `yaml:"format"`
	Path   string        `yaml:"path"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func Default() Config {
	return Config{
		Scraper: ScraperConfig{
			Concurrency: 4,
			MaxDepth:    0,
			MaxPages:    0,
			SameHost:    true,
		},
		HTTP: HTTPConfig{
			Timeout:           30 * time.Second,
			MaxBodyBytes:      10 << 20,
			RequestsPerSecond: 1,
			Burst:             1,
			RespectRobots:     true,
			RobotsAgent:       "MediaScraper",
			Retry: RetryConfig{
				MaxAttempts: 3,
				BaseDelay:   500 * time.Millisecond,
				MaxDelay:    30 * time.Second,
				Statuses:    slices.Clone(fetch.DefaultRetryStatuses),
			},
		},
		Proxy: ProxyConfig{
			MaxFailures: 3,
			Cooldown:    time.Minute,
		},
		Output: OutputConfig{Format: output.FormatJSONL},
		Log:    LogConfig{Level: "info", Format: "text"},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	var errs []error
	if c.Scraper.Concurrency < 1 {
		errs = append(errs, errors.New("scraper.concurrency must be >= 1"))
	}
	if c.Scraper.MaxDepth < 0 {
		errs = append(errs, errors.New("scraper.max_depth must be >= 0"))
	}
	if c.Scraper.MaxPages < 0 {
		errs = append(errs, errors.New("scraper.max_pages must be >= 0"))
	}
	if c.HTTP.Timeout <= 0 {
		errs = append(errs, errors.New("http.timeout must be > 0"))
	}
	if c.HTTP.MaxBodyBytes <= 0 {
		errs = append(errs, errors.New("http.max_body_bytes must be > 0"))
	}
	if c.HTTP.RequestsPerSecond < 0 {
		errs = append(errs, errors.New("http.requests_per_second must be >= 0 (0 disables limiting)"))
	}
	if c.HTTP.Retry.MaxAttempts < 1 {
		errs = append(errs, errors.New("http.retry.max_attempts must be >= 1"))
	}
	if !slices.Contains(output.Formats, c.Output.Format) {
		errs = append(errs, fmt.Errorf("output.format must be one of %v", output.Formats))
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, c.Log.Level) {
		errs = append(errs, errors.New("log.level must be one of debug, info, warn, error"))
	}
	if !slices.Contains([]string{"text", "json"}, c.Log.Format) {
		errs = append(errs, errors.New("log.format must be text or json"))
	}
	for i := range c.Profiles {
		if err := c.Profiles[i].Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

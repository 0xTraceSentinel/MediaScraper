# MediaScraper

A configurable web scraper written in Go. It is built mainly for e-commerce
sites, and its parts are designed so other kinds of content and a web
frontend can be added later.

## Status
Still under active development...

- **Works on most stores with no setup.** It reads schema.org **JSON-LD**,
  **microdata** and **OpenGraph** product data automatically.
- **Site profiles for the rest.** You can add CSS-selector rules for a
  site in YAML, with no code changes. Profile values take priority, and
  structured data fills in any missing fields.
- **Crawling.** It can follow product links and pagination up to a set
  depth and page limit. It skips URLs it has already seen and can stay on
  the seed hosts.
- **Polite by default.** It follows robots.txt (including `Crawl-delay`),
  rate-limits each host, and retries with exponential backoff and jitter. It
  also honours `Retry-After`.
- **Proxy-ready.** It can rotate through HTTP, HTTPS or SOCKS5 proxies and
  bench a proxy for a while after repeated failures. The proxy layer is an
  interface, so smarter providers can be added later.
- **Output:** JSON Lines (streamed), JSON, or CSV.

## Quick start

```sh
make build                      # or: go build -o bin/mediascraper ./cmd/mediascraper

# One product page → JSON Lines on stdout
bin/mediascraper scrape https://scrapeme.live/shop/Bulbasaur/

# Crawl a category one hop deep, following product cards, to CSV
bin/mediascraper scrape https://store.example.com/c/shoes \
    --depth 1 --follow "a.product-card" -f csv -o shoes.csv

# Many URLs from a file, through rotating proxies, with a config file
bin/mediascraper scrape -c config.yaml -i urls.txt --proxy-file proxies.txt
```

Example output (one line per page):

```json
{"url":"https://scrapeme.live/shop/Bulbasaur/","status_code":200,"attempts":1,
 "products":[{"name":"Bulbasaur","sku":"4391","price":"63.00","currency":"GBP",
 "availability":"InStock","images":["https://scrapeme.live/.../001.png"],"sources":["jsonld"]}]}
```

Prices are kept as decimal strings (`"1299.00"`) so they never pick up
floating-point rounding errors. Availability uses the schema.org values
(`InStock`, `OutOfStock`, `PreOrder`, …).

## Commands

| Command | Purpose |
| --- | --- |
| `scrape [url...]` | Scrape URLs (see `scrape --help` for all flags) |
| `config validate -c file.yaml` | Check a config file without making network requests |
| `config defaults` | Print the default configuration as YAML |
| `version` | Print build information |

Settings are applied in this order, with later ones overriding earlier
ones: **built-in defaults → config file → CLI flags**. Unknown config keys
are rejected, so a typo can't silently fall back to a default. See
[`configs/example.yaml`](configs/example.yaml) for every option, including a
sample site profile.

## Architecture

```
cmd/mediascraper/      Entry point: signal handling, exit codes
internal/
  cli/                 Cobra commands; maps flags onto config
  app/                 Composition root: config → fetcher/proxy/pipeline/engine
  config/              Config schema, defaults, YAML loading, validation
  scraper/             Crawl engine: worker pool, frontier, dedupe, limits
  fetch/               HTTP fetcher: retries, rate limits, robots.txt, UA rotation
  proxy/               Provider interface + rotating pool with health tracking
  extract/             JSON-LD, microdata, OpenGraph, CSS profiles, merge pipeline
  output/              JSONL / JSON / CSV sinks
  model/               Normalized data types (Product)
  version/             Build metadata (set via -ldflags)
```

How a page moves through the system:

```
seeds ─▶ scraper.Engine ─▶ fetch.Fetcher ─▶ extract.Pipeline ─▶ <-chan Result ─▶ output.Sink
             ▲  (frontier)     │ proxy.Provider            │ links
             └─────────────────┴───────────────────────────┘
```

Design decisions:

- **The engine doesn't depend on any UI.** `Engine.Run` returns a channel
  of `Result`s. The CLI writes them to a sink. A future HTTP API for the
  frontend can stream the same results over SSE or WebSocket, or store them,
  and build everything through `internal/app` exactly as the CLI does.
- **Extension points are interfaces:** `fetch.Fetcher`, `proxy.Provider`,
  `extract.Extractor`, `output.Sink`. Each can be swapped without touching
  the engine.
- **Proxy selection happens once per attempt, in the fetcher.** It does not
  happen inside the transport. This lets every outcome (network error, 403,
  407, 429) be reported back to the provider that chose the proxy, so the
  provider can rotate away from blocked proxies. A retry gets a fresh
  proxy.
- **Scraped data is treated as untrusted.** Response bodies are size-capped,
  and CSV cells are protected against formula injection.

## Proxies and bot detection

Proxies are already supported:

```yaml
proxy:
  urls:
    - http://user:pass@proxy1.example.net:8080
    - socks5://proxy2.example.net:1080
  file: proxies.txt   # one per line, '#' comments allowed
  max_failures: 3     # consecutive failures before benching
  cooldown: 1m
```

Credentials are redacted in all logs and output. Keep real proxy lists in
files covered by `.gitignore` (such as `proxies.txt` or `configs/*.local.yaml`).

Planned work that the current design already has a place for:

| Capability | Where it plugs in |
| --- | --- |
| Commercial rotating-proxy APIs / residential pools | New `proxy.Provider` implementation |
| Sticky sessions (same proxy per host/session) | `Provider.Next` receives the target URL |
| Geo-targeted proxy selection | `proxy.Provider` + per-profile config |
| Browser-like TLS/HTTP2 fingerprints (e.g. uTLS) | `fetch.Options.Transport` |
| JavaScript-rendered sites (headless Chrome) | New `fetch.Fetcher` implementation |
| Per-proxy cookie jars | `fetch.HTTPFetcher` (currently one shared jar) |
| CAPTCHA/block-page detection | Fetcher response classification → `Provider.Report` |

## Development

```sh
make test        # unit + integration tests (httptest servers, no network)
make test-race   # with the race detector (needs cgo)
make cover       # coverage report
make lint        # golangci-lint (config in .golangci.yml)
```

CI (GitHub Actions) runs vet, tests with `-race`, golangci-lint and a build
on every push and PR.

## Responsible use

Only scrape sites you are allowed to. The defaults (respect robots.txt,
1 request/second per host) are deliberately conservative. Read each site's
terms of service before raising them. `--ignore-robots` exists for sites you
own or have permission to crawl.

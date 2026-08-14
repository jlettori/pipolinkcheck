# pipolinkcheck

[![CI](https://github.com/jlettori/pipolinkcheck/actions/workflows/ci.yml/badge.svg)](https://github.com/jlettori/pipolinkcheck/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/jlettori/pipolinkcheck.svg)](https://pkg.go.dev/github.com/jlettori/pipolinkcheck)
[![Release](https://img.shields.io/github/v/release/jlettori/pipolinkcheck)](https://github.com/jlettori/pipolinkcheck/releases/latest)
[![License](https://img.shields.io/github/license/jlettori/pipolinkcheck)](https://github.com/jlettori/pipolinkcheck/blob/main/LICENSE)
[![codecov](https://codecov.io/gh/jlettori/pipolinkcheck/graph/badge.svg?token=QZ74MRHLNY)](https://codecov.io/gh/jlettori/pipolinkcheck)

A concurrent website crawler that finds and reports broken links. It follows
`a`, `img`, `link`, `script`, `video` and `source` tags, then writes every
broken link it finds to a CSV report, while throttling requests and parallelizing
across multiple workers.

Works with any website — audit your site's links (see [example.com](https://example.com)).

## Installation

Build the binary (requires **Go 1.26+**):

```bash
go build .
```

The resulting `./pipolinkcheck` binary is ready to use. You can also build it
with `make build`.

## Usage

```bash
./pipolinkcheck [options]
```

Run `./pipolinkcheck` with no arguments to see the full list of options:

```
Usage: ./pipolinkcheck [options]

Options:
  -root <url>     Root URL to start crawling from (default "https://example.com/")
  -allowed <csv>  Comma-separated list of allowed URL prefixes
  -excluded <csv> Comma-separated list of excluded URL prefixes
  -maxreqs <n>    Maximum requests per second (default 20, clamped to 2-20)
  -maxlinks <n>   Maximum number of unique links to crawl (default 100000)
  -workers <n>    Number of parallel workers (default 0: auto-computed from CPUs, clamped to 2-16)
  -output <path>  Output CSV file path
  -user-agent <s> User-Agent header sent with every request
  -verbose        detailed logging output for debugging and monitoring (default true)
  -insecure-tls   skips TLS certificate verification (not recommended)
```

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `-root` | `https://example.com/` | URL to start crawling from |
| `-allowed` | *root URL* | Comma-separated URL prefixes the crawler is allowed to visit; root-relative entries (e.g. `/docs`) are resolved against `-root` |
| `-excluded` | *(none)* | Comma-separated URL prefixes to skip; root-relative entries (e.g. `/admin`) are resolved against `-root` |
| `-maxreqs` | `20` | Maximum requests per second (automatically clamped to 2–20) |
| `-maxlinks` | `100000` | Maximum number of unique links to crawl before discovery stops (bounds memory usage) |
| `-workers` | `0` | Number of parallel workers (`0` auto-computes from CPU count; clamped to 2–16) |
| `-output` | *derived from URL* | Output CSV file path (e.g. `pipolinkcheck-www.example.com.csv`) |
| `-user-agent` | `Mozilla/5.0 (compatible; PipoLinkCheckBot/1.0)` | User-Agent header sent with each request |
| `-verbose` | `true` | Log every crawled URL; use `-verbose=false` to silence |
| `-insecure-tls` | `false` | Skip TLS certificate verification (only for self-signed sites; not recommended) |

Any option listed under `Options` may also be set programmatically via
`NewConfigWithOptions` (see `config.go`) when embedding the crawler in your own
Go program.

## Examples

### Basic crawl

Crawl a site staying within its own domain (the root host is allowed by default):

```bash
./pipolinkcheck -root https://example.com
```

### Explicitly restrict the crawl to matching prefixes

Only follow URLs beginning with `https://example.com/`:

```bash
./pipolinkcheck -root https://example.com -allowed https://example.com
```

Allow a site *and* all its subdomains:

```bash
./pipolinkcheck -root https://example.com \
  -allowed https://example.com,https://www.example.com,https://blog.example.com
```

Stray beyond the root host, e.g. also crawl a CDN that hosts images:

```bash
./pipolinkcheck -root https://example.com \
  -allowed https://example.com,https://cdn.example.net
```

The root URL is always allowed; the `-allowed` flag adds extra prefixes on top
of it.

### Use root-relative prefixes

Prefixes may be written relative to the root URL — a leading `/` is resolved
against `-root`, so you don't have to repeat the full host:

```bash
./pipolinkcheck -root https://example.com \
  -allowed /docs,/api \
  -excluded /admin,/tmp
```

This is equivalent to `-allowed https://example.com/docs,https://example.com/api`
and `-excluded https://example.com/admin,https://example.com/tmp`. Absolute
URLs work exactly as before.

### Exclude parts of the site

Skip a section you know is heavy or has thousands of stale pages:

```bash
./pipolinkcheck -root https://example.com -excluded https://example.com/archive,https://example.com/tmp
```

Allowed and excluded prefixes can be combined — excluded wins over allowed:

```bash
./pipolinkcheck -root https://example.com \
  -allowed https://example.com,https://cdn.example.net \
  -excluded https://example.com/admin,https://cdn.example.net/private
```

### Control crawling speed

Slow things down to be polite to the server / avoid rate limiting (clamped to a
minimum of 2):

```bash
./pipolinkcheck -root https://example.com -maxreqs 2
```

Speed up for a large site:

```bash
./pipolinkcheck -root https://example.com -maxreqs 20
```

The value is automatically clamped to the 2–20 range, so out-of-range values
are safe to pass.

### Bound the number of crawled links

To keep memory usage bounded on very large sites, the crawler stops discovering
new links once it has queued `-maxlinks` unique URLs (100000 by default):

```bash
./pipolinkcheck -root https://example.com -maxlinks 50000
```

Pass a larger value (or rely on the default) for extensive sites.

### Choose the output file

```bash
./pipolinkcheck -root https://example.com -output /tmp/broken-links.csv
```

If `-output` is omitted, a filename is derived from the URL
(`pipolinkcheck-www.example.com.csv`) and the crawler also writes a matching
stats log (`pipolinkcheck-www.example.com.log`).

### Use a custom User-Agent

```bash
./pipolinkcheck -root https://example.com \
  -user-agent "Acme-SiteAudit/2.5 (+https://acme.example)"
```

### Silence verbose per-URL logging

```bash
./pipolinkcheck -root https://example.com -verbose=false
```

### Crawl a site with a self-signed certificate

```bash
./pipolinkcheck -root https://intranet.local -insecure-tls
```

> **Security note:** only use `-insecure-tls` for internal sites with
> self-signed / non-trusted certificates. Do not use it on public sites.

### A full example combining options

```bash
./pipolinkcheck \
  -root https://www.example.com \
  -allowed https://www.example.com,https://static.example.com \
  -excluded https://www.example.com/account,https://www.example.com/api \
  -maxreqs 8 \
  -user-agent "Acme-SiteAudit/2.5" \
  -output /tmp/example-report.csv \
  -verbose=false
```

## Output

The crawler produces **two** files:

### 1. CSV report (`<output>.csv`)

A semicolon-delimited (`;`) CSV with the following columns:

| Column | Description |
|--------|-------------|
| `Link Type` | `hyperlink`, `image`, `css`, `script`, or `video` |
| `Source Page` | The page the broken link was discovered on |
| `Link Name` | Visible link text (hyperlinks) / alt or title attribute |
| `Selector` | A short CSS-like path locating the element on the page, built from elements with an `id`/`class` and structural landmarks (e.g. `main#contents > section.block > p > a`). Boilerplate (`html`, `head`, `body`) and nested anonymous wrappers are dropped, and the path is capped in length |
| `Broken Link` | The URL that failed |
| `Status Code` | HTTP status returned (e.g. `404`), or one of the internal codes below when the link fails before an HTTP response |
| `Error message` | Textual error when no status code is available (e.g. DNS, TLS, timeout) |

Internal error codes written to the `Status Code` column when a request fails
before any HTTP response is received:

| Code | Meaning |
|------|---------|
| `1001` | Building the HTTP request failed (e.g. malformed URL) |
| `1002` | The HTTP request could not be completed (network/TLS/DNS error) |
| `1003` | Processing the link panicked |

Example report:

```csv
Link Type;Source Page;Link Name;Selector;Broken Link;Status Code;Error message
hyperlink;https://www.example.com/;;main#contents > section.block > p > a;https://www.example.com/old-page.html;404;
image;https://www.example.com/;banner;header > div.logo > img;https://www.example.com/img/banner.png;500;
hyperlink;https://www.example.com/contact;read more;footer > ul.footer-links > li > a;https://www.example.com/privacy;404;
```

The file is written with a UTF-8 BOM so it opens correctly in Excel/LibreOffice.
Cells are escaped to prevent spreadsheet formula injection.

### 2. Stats log (`<output>.log`)

A human-readable crawl summary: visited resources by MIME type, links enqueued
by type, and a breakdown of broken links by type and HTTP status code.

## Development

```bash
make test      # run tests
make fmt       # format code (requires gofumpt)
make coverage  # generate coverage report
make build     # build binary
make clean     # remove build artifacts
```
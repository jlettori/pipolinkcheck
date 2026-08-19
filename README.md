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

The resulting `pipolinkcheck` binary is ready to use. You can also build it
with `make build`.

## Antivirus false positives

Unsigned Go binaries are frequently flagged by antivirus software as
suspicious, even when they are completely clean. This is a known false
positive caused by heuristics matching statically-linked Go executables.

The most common detection is Microsoft Defender's
`Trojan:Win32/Wacatac.C!ml`. The `!ml` suffix means it comes from a
machine-learning heuristic, not a malware signature, and it is widely reported
against freshly built Go binaries (especially when built with a very recent Go
toolchain, before antivirus vendors update their models). It is a false
positive: this repository only crawls and checks the links of websites.

If you get a `Wacatac.C!ml` detection, the fastest way to clear it is to
report the file to Microsoft's security team as a false positive:

1. Rebuild the binary from this source so you can attest it is yours.
2. Submit it to <https://www.microsoft.com/wdsi/filesubmission> (sign in with
   a Microsoft account), selecting *"My submission was incorrectly detected as
   malware"* and noting the build revision.
3. Also add an exclusion in Defender for your local build directory so the
   binary is usable while Microsoft processes the report.

To confirm the binary you build is safe, rebuild from this source and compare
the SHA-256 hash — a reproducible build from the same revision always produces
the same bytes.

Release binaries are code-signed when a signing certificate is provided (see
below); signed binaries are trusted by most antivirus products. For locally
built development binaries, add the binary (or your build directory) to your
antivirus exclusion list.

### Code signing releases

Release binaries are signed when the corresponding secrets are configured in
GitHub. Signing runs via `scripts/sign.sh` and is skipped entirely when no
certificate is configured, so releases work without it.

| Platform | Secret(s) | Tool |
|----------|-----------|------|
| Windows | `WINDOWS_SIGNING_PFX`, `WINDOWS_SIGNING_PASSWORD` | `osslsigncode` (PKCS#12 `.pfx`) |
| macOS | `MACOS_SIGNING_IDENTITY` | `codesign` (must run on macOS) |

### How to produce a signed Windows binary

1. **Get a code-signing certificate.** Authenticode signing needs an
   organization-validation (OV) certificate — obtain one from your work's IT
   department, or buy one from a commercial CA (DigiCert, Sectigo,
   GlobalSign). A self-signed certificate does **not** help: it has no
   reputation and can *increase* detections.
2. **Export it as a PKCS#12 `.pfx`** that includes the private key, and note
   the password you set on it.
3. **Base64-encode the `.pfx`** so it can be stored as a GitHub secret:
   ```bash
   base64 -w0 your-cert.pfx > your-cert.pfx.b64
   ```
4. **Add two repository secrets** (Settings → Secrets and variables →
   Actions):
   - `WINDOWS_SIGNING_PFX` = the full contents of `your-cert.pfx.b64`
   - `WINDOWS_SIGNING_PASSWORD` = the `.pfx` password
5. **Push a `v*` tag.** The release workflow decodes the certificate, signs
   every `.exe` it builds, then verifies the signatures before publishing.
   If signing fails, the workflow fails so no unsigned binary ships.

Note that a freshly issued certificate still needs to build reputation — the
first signed releases may be flagged until AV vendors see the certificate used
by legitimate software over time. Submitting the signed release to VirusTotal
and to Microsoft's <https://www.microsoft.com/wdsi/filesubmission> speeds this
up.

### Which code-signing certificate to buy

Check with your organization's IT department first — many companies already own
a code-signing certificate you can use for internal tools at no cost. If you
must buy one, an OV certificate from a commercial CA is the right choice for a
project like this. Prices are per certificate, per year (reseller prices run
below list prices):

| CA | OV price (approx.) | Notes |
|----|--------------------|-------|
| Sectigo | ~$220/year | Cheapest of the major CAs |
| DigiCert | ~$409–439/year | Premium brand, enterprise focus |
| GlobalSign | ~$300–400/year | Quote at checkout, not public |

Two things drive the real cost up:

1. **Mandatory hardware** — since 2023 the private key must live on a FIPS
   140-2 L2 USB token (e.g. YubiKey) or HSM. Budget $50–130 extra for the token
   plus shipping, or a cloud-signing subscription (~$200+/year on top of the
   certificate).
2. **Shorter validity** — since 2026 the CA/Browser Forum caps code-signing
   certificates at 460 days, so multi-year plans are delivered as annual
   reissues.

For this project an OV certificate does what an EV certificate would (EV no
longer grants instant SmartScreen reputation either), so buy OV unless a driver
submission or procurement rule forces EV.

macOS signing additionally requires notarization to be fully trusted by
Gatekeeper; `codesign` alone covers AV heuristics but not notarization.

## Usage

```bash
pipolinkcheck [options]
```

Run `pipolinkcheck` with no arguments to see the full list of options:

```
Usage: pipolinkcheck [options]

Options:
  -base <url>     Base URL to start crawling from (default "https://example.com/")
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
| `-base` | `https://example.com/` | URL to start crawling from |
| `-allowed` | *root URL* | Comma-separated URL prefixes the crawler is allowed to visit; root-relative entries (e.g. `/docs`) are resolved against `-base` |
| `-excluded` | *(none)* | Comma-separated URL prefixes to skip; root-relative entries (e.g. `/admin`) are resolved against `-base` |
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
pipolinkcheck -base https://example.com
```

### Explicitly restrict the crawl to matching prefixes

Only follow URLs beginning with `https://example.com/`:

```bash
pipolinkcheck -base https://example.com -allowed https://example.com
```

Allow a site *and* all its subdomains:

```bash
pipolinkcheck -base https://example.com \
  -allowed https://example.com,https://www.example.com,https://blog.example.com
```

Stray beyond the root host, e.g. also crawl a CDN that hosts images:

```bash
pipolinkcheck -base https://example.com \
  -allowed https://example.com,https://cdn.example.net
```

The root URL is always allowed; the `-allowed` flag adds extra prefixes on top
of it.

### Use root-relative prefixes

Prefixes may be written relative to the root URL — a leading `/` is resolved
against `-base`, so you don't have to repeat the full host:

```bash
pipolinkcheck -base https://example.com \
  -allowed /docs,/api \
  -excluded /admin,/tmp
```

This is equivalent to `-allowed https://example.com/docs,https://example.com/api`
and `-excluded https://example.com/admin,https://example.com/tmp`. Absolute
URLs work exactly as before.

### Exclude parts of the site

Skip a section you know is heavy or has thousands of stale pages:

```bash
pipolinkcheck -base https://example.com -excluded https://example.com/archive,https://example.com/tmp
```

Allowed and excluded prefixes can be combined — excluded wins over allowed:

```bash
pipolinkcheck -base https://example.com \
  -allowed https://example.com,https://cdn.example.net \
  -excluded https://example.com/admin,https://cdn.example.net/private
```

### Control crawling speed

Slow things down to be polite to the server / avoid rate limiting (clamped to a
minimum of 2):

```bash
pipolinkcheck -base https://example.com -maxreqs 2
```

Speed up for a large site:

```bash
pipolinkcheck -base https://example.com -maxreqs 20
```

The value is automatically clamped to the 2–20 range, so out-of-range values
are safe to pass.

### Bound the number of crawled links

To keep memory usage bounded on very large sites, the crawler stops discovering
new links once it has queued `-maxlinks` unique URLs (100000 by default):

```bash
pipolinkcheck -base https://example.com -maxlinks 50000
```

Pass a larger value (or rely on the default) for extensive sites.

### Choose the output file

```bash
pipolinkcheck -base https://example.com -output /tmp/broken-links.csv
```

If `-output` is omitted, a filename is derived from the URL
(`pipolinkcheck-www.example.com.csv`) and the crawler also writes a matching
stats log (`pipolinkcheck-www.example.com.log`).

### Use a custom User-Agent

```bash
pipolinkcheck -base https://example.com \
  -user-agent "Acme-SiteAudit/2.5 (+https://acme.example)"
```

### Silence verbose per-URL logging

```bash
pipolinkcheck -base https://example.com -verbose=false
```

### Crawl a site with a self-signed certificate

```bash
pipolinkcheck -base https://intranet.local -insecure-tls
```

> **Security note:** only use `-insecure-tls` for internal sites with
> self-signed / non-trusted certificates. Do not use it on public sites.

### A full example combining options

```bash
pipolinkcheck \
  -base https://www.example.com \
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
| `Error message` | Human-readable description of the failure — the standard status description for HTTP errors (e.g. `Not Found`), or the textual error for internal failures (e.g. DNS, TLS, timeout) |

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
hyperlink;https://www.example.com/;;main#contents > section.block > p > a;https://www.example.com/old-page.html;404;Not Found
image;https://www.example.com/;banner;header > div.logo > img;https://www.example.com/img/banner.png;500;Internal Server Error
hyperlink;https://www.example.com/contact;read more;footer > ul.footer-links > li > a;https://www.example.com/privacy;404;Not Found
```

The file is written with a UTF-8 BOM so it opens correctly in Excel/LibreOffice.
Cells are escaped to prevent spreadsheet formula injection.

### 2. Stats log (`<output>.log`)

A human-readable crawl summary: visited resources by MIME type, links enqueued
by type, and a breakdown of broken links by type and HTTP status code.

## Development

```bash
make test      # run tests with coverage profile
make race      # run tests with the race detector enabled
make fmt       # format code (requires gofumpt)
make coverage  # generate coverage report
make build     # build binary
make clean     # remove build artifacts
```

The project includes unit tests for the crawler, config, result writer,
stats, HTTP layer, HTML parsing, and verification helpers. CI runs the test
suite (with coverage uploaded to Codecov) and the race detector on Linux, and
builds binaries for Linux, macOS, and Windows.
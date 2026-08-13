package main

import (
	"context"
	"crypto/tls"
	"errors"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// newHTTPClient builds an http.Client with a timeout, redirect validation, and env-based proxy.
// TLS certificates are verified by default; pass insecureTLS=true to skip verification
// (e.g. for internal sites with self-signed certificates).
func newHTTPClient(allowedURL, excludedURL []url.URL, insecureTLS bool) *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecureTLS, // #nosec G402 -- opt-in via -insecure-tls flag
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			for i := range excludedURL {
				if urlMatchesPrefix(req.URL, &excludedURL[i]) {
					return http.ErrUseLastResponse
				}
			}
			for i := range allowedURL {
				if urlMatchesPrefix(req.URL, &allowedURL[i]) {
					return nil
				}
			}
			return http.ErrUseLastResponse
		},
	}
}

// urlMatchesPrefix reports whether u is within the scope of the prefix URL.
// Matching is done on scheme, exact host, and path elements so that a
// look-alike host such as "example.com.evil.com" is never treated
// as "example.com".
func urlMatchesPrefix(u *url.URL, prefix *url.URL) bool {
	// Scheme is lowercased by url.Parse; host is case-insensitive but preserved
	// as typed, so it needs a case-insensitive comparison.
	if u.Scheme != prefix.Scheme {
		return false
	}

	if !strings.EqualFold(u.Host, prefix.Host) {
		return false
	}

	return pathHasPrefix(u.Path, prefix.Path)
}

// pathHasPrefix reports whether path begins with prefix, matching only on
// path-element boundaries so "/guide/2" matches "/guide" but
// "/guidet" does not.
func pathHasPrefix(path, prefix string) bool {
	if prefix == "" {
		return true
	}

	if !strings.HasPrefix(path, prefix) {
		return false
	}

	if strings.HasSuffix(prefix, "/") {
		return true
	}

	return len(path) == len(prefix) || strings.HasPrefix(path[len(prefix):], "/")
}

// do sends a single request for the link, reporting any failure via reportError.
// It returns nil when the request could not be built or performed.
func (c *Crawler) do(link Link, method string) *http.Response {
	req, err := http.NewRequest(method, link.URL, nil)
	if err != nil {
		c.reportError(link, errCodeRequestConstruction, err.Error())

		return nil
	}

	req.Header.Set("User-Agent", c.cfg.UserAgent)

	var resp *http.Response
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if resp, err = c.httpClient.Do(req); err == nil {
			return resp
		}

		if !isRetryable(err) || attempt == maxRetries {
			break
		}

		// Full-jitter exponential backoff: each delay is chosen uniformly in
		// [0, base*2^attempt), capped at backoffMax. This spreads retries for
		// many simultaneous timeouts and avoids thundering-herd synchronisation.
		maxDelay := backoffBase * (1 << attempt)
		if maxDelay > backoffMax {
			maxDelay = backoffMax
		}
		time.Sleep(time.Duration(rand.Int64N(int64(maxDelay))))
	}

	c.reportError(link, errCodeRequestFailed, err.Error())

	return nil
}

// isRetryable reports whether an HTTP transport error is transient enough to
// warrant a retry. Timeouts (including "context deadline exceeded awaiting
// headers"), connection resets, and temporary net errors are retried; permanent
// errors such as an unknown host are not.
func isRetryable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	return false
}

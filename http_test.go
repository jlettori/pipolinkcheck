package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// newRetryCrawler builds a minimal Crawler using the provided HTTP client so
// tests can exercise do() without going through newHTTPClient's fixed timeout.
func newRetryCrawler(t *testing.T, client *http.Client) *Crawler {
	t.Helper()
	return &Crawler{
		cfg:        &Config{UserAgent: "test-agent"},
		httpClient: client,
		stats:      NewStats(),
		resultCh:   make(chan BrokenLink, 4),
	}
}

func TestPathHasPrefixBoundaries(t *testing.T) {
	tests := []struct {
		path, prefix string
		want         bool
	}{
		{"/guide/", "/guide/", true},
		{"/guide/page", "/guide/", true},
		{"/guide/page", "/guide", true},
		{"/guide", "/guide", true},
		{"/guidet", "/guide", false},
		{"/guide2/", "/guide", false},
		{"/anything", "", true},
	}
	for _, tt := range tests {
		if got := pathHasPrefix(tt.path, tt.prefix); got != tt.want {
			t.Errorf("pathHasPrefix(%q, %q) = %v; want %v", tt.path, tt.prefix, got, tt.want)
		}
	}
}

func TestURLMatchesPrefixLookAlikeHosts(t *testing.T) {
	tests := []struct {
		url, prefix string
		want        bool
	}{
		{"https://example.com/guide/", "https://example.com/guide/", true},
		{"https://example.com.evil.com/guide/", "https://example.com/guide/", false},
		{"HTTPS://EXAMPLE.COM/guide/", "https://example.com/guide/", true},
		{"https://example.com:443/guide/", "https://example.com/guide/", false},
		{"https://example.com", "https://example.com", true},
	}
	for _, tt := range tests {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Fatal(err)
		}
		p, err := url.Parse(tt.prefix)
		if err != nil {
			t.Fatal(err)
		}
		if got := urlMatchesPrefix(u, p); got != tt.want {
			t.Errorf("urlMatchesPrefix(%q, %q) = %v; want %v", tt.url, tt.prefix, got, tt.want)
		}
	}
}

// timeoutError is a net.Error that reports Timeout true and Temporary false.
type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout awaiting headers" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

// temporaryError is a net.Error that reports both Timeout and Temporary false.
type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary failure" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"context deadline exceeded", context.DeadlineExceeded, true},
		{"deadline wrapped in url.Error", &url.Error{Err: context.DeadlineExceeded}, true},
		{"timeout net.Error", timeoutError{}, true},
		{"temporary net.Error", temporaryError{}, true},
		{"permanent net.Error", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, false},
		{"generic error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryable(tt.err); got != tt.want {
				t.Errorf("isRetryable() = %v; want %v", got, tt.want)
			}
		})
	}
}

// shortTimeoutClient returns an http.Client whose request timeout is short
// enough to exercise retries in tests without waiting for the real 15s.
func shortTimeoutClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func TestDoRetriesOnTimeoutThenSucceeds(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// Simulate a slow server: exceed the client timeout on the first try.
			time.Sleep(2 * time.Second)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newRetryCrawler(t, shortTimeoutClient(100*time.Millisecond))
	resp := c.do(Link{URL: srv.URL, Type: LinkTypeHyperlink}, "GET")
	if resp == nil {
		t.Fatalf("do() returned nil; want a successful response after retry")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d; want %d", resp.StatusCode, http.StatusOK)
	}
	if calls.Load() < 2 {
		t.Errorf("server hit %d times; want at least 2 (first attempt timed out and was retried)", calls.Load())
	}
}

func TestDoRetriesExhaustedThenReportsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always slower than the client timeout.
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	c := newRetryCrawler(t, shortTimeoutClient(50*time.Millisecond))
	resp := c.do(Link{URL: srv.URL, Type: LinkTypeHyperlink}, "GET")
	if resp != nil {
		resp.Body.Close()
		t.Fatalf("do() returned a response; want nil after retries are exhausted")
	}

	select {
	case res := <-c.resultCh:
		if res.statusCode != errCodeRequestFailed {
			t.Errorf("statusCode = %d; want %d", res.statusCode, errCodeRequestFailed)
		}
		if res.errorMsg == "" {
			t.Error("expected a non-empty error message")
		}
	default:
		t.Error("expected a broken-link result after retries were exhausted")
	}
}

func TestDoDoesNotRetryPermanentError(t *testing.T) {
	var dials atomic.Int64
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("no such host")
		},
	}}

	c := newRetryCrawler(t, client)
	resp := c.do(Link{URL: "http://nonexistent.invalid/", Type: LinkTypeHyperlink}, "GET")
	if resp != nil {
		resp.Body.Close()
		t.Fatalf("do() returned a response; want nil for a permanent error")
	}
	if got := dials.Load(); got != 1 {
		t.Errorf("dialed %d times; want exactly 1 (permanent errors must not be retried)", got)
	}
	select {
	case res := <-c.resultCh:
		if res.statusCode != errCodeRequestFailed {
			t.Errorf("statusCode = %d; want %d", res.statusCode, errCodeRequestFailed)
		}
	default:
		t.Error("expected a broken-link result for the permanent error")
	}
}

func TestDoSetsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newRetryCrawler(t, &http.Client{})
	resp := c.do(Link{URL: srv.URL, Type: LinkTypeHyperlink}, "GET")
	if resp == nil {
		t.Fatal("do() returned nil; want a response")
	}
	resp.Body.Close()

	// Re-run with a handler that captures the User-Agent, since the crawler
	// builds a fresh request per call.
	var gotUA string
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	})
	resp = c.do(Link{URL: srv.URL, Type: LinkTypeHyperlink}, "GET")
	if resp == nil {
		t.Fatal("do() returned nil; want a response")
	}
	resp.Body.Close()
	if gotUA != "test-agent" {
		t.Errorf("User-Agent = %q; want %q", gotUA, "test-agent")
	}
}

func TestDoReportsRequestConstructionError(t *testing.T) {
	c := newRetryCrawler(t, &http.Client{})
	resp := c.do(Link{URL: "http://[::1", Type: LinkTypeHyperlink}, "GET")
	if resp != nil {
		resp.Body.Close()
		t.Fatalf("do() returned a response; want nil for an invalid URL")
	}
	select {
	case res := <-c.resultCh:
		if res.statusCode != errCodeRequestConstruction {
			t.Errorf("statusCode = %d; want %d", res.statusCode, errCodeRequestConstruction)
		}
	default:
		t.Error("expected a broken-link result for the request construction error")
	}
}

// scopedClient builds an http.Client via newHTTPClient with the given allowed
// and excluded URL scope prefixes, so CheckRedirect decisions can be exercised.
func scopedClient(allowed, excluded string) *http.Client {
	a, _ := parseURLList(allowed, "", nil)
	e, _ := parseURLList(excluded, "", nil)
	return newHTTPClient(a, e, false)
}

// doGet follows client.Get and closes the body, returning the response.
func doGet(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Get(%q) unexpected error: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestCheckRedirectFollowsAllowedRedirect(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, srv.URL+"/target", http.StatusFound)
		case "/target":
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := scopedClient(srv.URL, "")
	resp := doGet(t, client, srv.URL+"/start")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d; want %d (redirect within allowed scope should be followed)", resp.StatusCode, http.StatusOK)
	}
	if resp.Request.URL.Path != "/target" {
		t.Errorf("final path = %q; want %q", resp.Request.URL.Path, "/target")
	}
}

func TestCheckRedirectDoesNotFollowExcluded(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/blocked", http.StatusFound)
	}))
	defer srv.Close()

	client := scopedClient(srv.URL, srv.URL+"/blocked")
	resp := doGet(t, client, srv.URL+"/")
	if resp.StatusCode != http.StatusFound {
		t.Errorf("Status = %d; want %d (redirect to excluded URL must stop with last response)", resp.StatusCode, http.StatusFound)
	}
}

func TestCheckRedirectDoesNotFollowOutOfScope(t *testing.T) {
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer external.Close()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL, http.StatusFound)
	}))
	defer srv.Close()

	client := scopedClient(srv.URL, "")
	resp := doGet(t, client, srv.URL+"/")
	if resp.StatusCode != http.StatusFound {
		t.Errorf("Status = %d; want %d (redirect outside allowed scope must stop with last response)", resp.StatusCode, http.StatusFound)
	}
	if resp.Request.URL.Host != srv.Listener.Addr().String() {
		t.Errorf("final host = %q; want %q (must not have followed external redirect)", resp.Request.URL.Host, srv.Listener.Addr().String())
	}
}

func TestCheckRedirectStopsAtMaxRedirects(t *testing.T) {
	var hops atomic.Int64
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		http.Redirect(w, r, srv.URL+"/hop", http.StatusFound)
	}))
	defer srv.Close()

	// Self-redirecting loop: the URL stays within the allowed scope, so the only
	// thing that can terminate the chain is the redirect cap in CheckRedirect.
	client := scopedClient(srv.URL, "")
	resp := doGet(t, client, srv.URL+"/hop")
	if resp.StatusCode != http.StatusFound {
		t.Errorf("Status = %d; want %d (redirect chain should stop at the redirect cap)", resp.StatusCode, http.StatusFound)
	}
	if got := hops.Load(); got != maxRedirects {
		t.Errorf("server hit %d times; want %d", got, maxRedirects)
	}
}

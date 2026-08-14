package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// mustNewCrawler creates a Crawler for tests, failing the test if construction fails.
func mustNewCrawler(t *testing.T, cfg *Config) *Crawler {
	t.Helper()
	rw, err := newResultWriter(cfg.OutputFile)
	if err != nil {
		t.Fatalf("newResultWriter() unexpected error: %v", err)
	}
	return NewCrawler(cfg, rw)
}

func TestNewCrawler(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     "https://example.com",
		AllowedURLs: "https://example.com",
		MaxReqs:     10,
		Workers:     5,
		UserAgent:   "test-agent",
		OutputFile:  t.TempDir() + "/test_output.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	if c.cfg.RootURL != cfg.RootURL {
		t.Errorf("RootURL = %q; want %q", c.cfg.RootURL, cfg.RootURL)
	}
	if c.cfg.Workers != cfg.Workers {
		t.Errorf("Workers = %d; want %d", c.cfg.Workers, cfg.Workers)
	}
	if c.rateLimiter == nil {
		t.Error("rateLimiter should not be nil")
	}
	if c.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
	if c.linkCh == nil {
		t.Error("linkChan should not be nil")
	}
	if c.resultCh == nil {
		t.Error("results should not be nil")
	}
	if c.resultW == nil {
		t.Error("resultW should not be nil")
	}
}

func TestProcess_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    server.URL,
		UserAgent:  "test",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: server.URL, URL: server.URL + "/broken", Type: LinkTypeHyperlink})

	select {
	case res := <-c.resultCh:
		if res.statusCode != 404 {
			t.Errorf("expected status 404, got %d", res.statusCode)
		}
		if res.brokenURL != server.URL+"/broken" {
			t.Errorf("expected URL %s, got %s", server.URL+"/broken", res.brokenURL)
		}
	default:
		t.Error("expected a broken link result for 404")
	}
}

func TestProcess_200HTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="/page1">link</a>`))
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL,
		AllowedURLs: server.URL,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: server.URL, URL: server.URL + "/", Type: LinkTypeHyperlink})

	if _, ok := c.visited.Load(server.URL + "/page1"); !ok {
		t.Error("expected /page1 to be visited")
	}
}

func TestProcess_NonHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL,
		AllowedURLs: server.URL,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: server.URL, URL: server.URL + "/", Type: LinkTypeHyperlink})

	count := 0
	c.visited.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 0 {
		t.Error("expected no visited URLs for non-HTML content")
	}
}

func TestProcessVerbose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="/ok">link</a>`))
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL,
		AllowedURLs: server.URL,
		UserAgent:   "test",
		Verbose:     true,
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: server.URL, URL: server.URL + "/", Type: LinkTypeHyperlink})

	if _, ok := c.visited.Load(server.URL + "/ok"); !ok {
		t.Error("expected /ok to be visited")
	}
}

func TestProcessExcluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:      server.URL,
		AllowedURLs:  server.URL,
		ExcludedURLs: server.URL + "/excluded",
		UserAgent:    "test",
		OutputFile:   t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: server.URL, URL: server.URL + "/excluded", Type: LinkTypeHyperlink})

	count := 0
	c.visited.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 0 {
		t.Error("expected no visited URLs for excluded link")
	}
}

func TestProcessNonHTMLInitial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL,
		AllowedURLs: server.URL,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: "", URL: server.URL + "/", Type: LinkTypeHyperlink})

	select {
	case res := <-c.resultCh:
		if res.errorMsg != "initial URL is not an HTML page" {
			t.Errorf("expected error about non-HTML initial URL, got %q", res.errorMsg)
		}
	default:
		t.Error("expected a result for non-HTML initial URL")
	}
}

func TestProcessNonHyperlink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "HEAD" {
			t.Errorf("expected HEAD request, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="/page1">link</a>`))
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL,
		AllowedURLs: server.URL,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: server.URL, URL: server.URL + "/", Type: LinkTypeImage})

	// Non-hyperlinks use HEAD, so extraction won't run. No visited URLs expected.
	count := 0
	c.visited.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 0 {
		t.Error("expected no visited URLs for non-hyperlink (HEAD only)")
	}
}

func TestProcessNonHyperlink_headMethodNotAllowedFallsBackToGET(t *testing.T) {
	var headReqs, getReqs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			headReqs.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		getReqs.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL,
		AllowedURLs: server.URL,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: server.URL, URL: server.URL + "/img.png", Type: LinkTypeImage})

	select {
	case res := <-c.resultCh:
		t.Errorf("expected no broken result, got %+v", res)
	default:
	}

	if headReqs.Load() != 1 {
		t.Errorf("expected exactly 1 HEAD request, got %d", headReqs.Load())
	}
	if getReqs.Load() != 1 {
		t.Errorf("expected exactly 1 GET fallback request, got %d", getReqs.Load())
	}
}

func TestProcessNonHyperlinkBroken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL,
		AllowedURLs: server.URL,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: server.URL, URL: server.URL + "/img.png", Type: LinkTypeImage})

	select {
	case res := <-c.resultCh:
		if res.statusCode != 404 {
			t.Errorf("expected 404, got %d", res.statusCode)
		}
	default:
		t.Error("expected a broken link result")
	}
}

func TestCrawlerClose(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     "https://example.com",
		AllowedURLs: "https://example.com",
		MaxReqs:     10,
		Workers:     2,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	c.Close()

	_, err = os.Stat(cfg.OutputFile)
	if os.IsNotExist(err) {
		t.Fatal("output file should exist after Close")
	}
}

func TestRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="/page1">Page 1</a><a href="/page2">Page 2</a>`))
	}))
	defer server.Close()

	outputFile := t.TempDir() + "/test_output.csv"
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL + "/",
		AllowedURLs: server.URL,
		MaxReqs:     1000,
		Workers:     2,
		UserAgent:   "test",
		OutputFile:  outputFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	c.Run()
	c.Close()

	records, err := readCSV(outputFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 1 {
		t.Fatal("expected at least header row")
	}
	if records[0][0] != "Link Type" {
		t.Errorf("header[0] = %q; want %q", records[0][0], "Link Type")
	}
}

func TestRunWithBrokenLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="/broken">Broken</a><a href="/ok">OK</a>`))
	}))
	defer server.Close()

	outputFile := t.TempDir() + "/test_output.csv"
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL + "/",
		AllowedURLs: server.URL,
		MaxReqs:     1000,
		Workers:     2,
		UserAgent:   "test",
		OutputFile:  outputFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	c.Run()
	c.Close()

	records, err := readCSV(outputFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 rows (header + 1 broken), got %d", len(records))
	}
	if records[0][0] != "Link Type" {
		t.Errorf("header[0] = %q; want %q", records[0][0], "Link Type")
	}
	if records[1][5] != "404" {
		t.Errorf("expected status 404, got %q", records[1][5])
	}
	if records[1][4] != server.URL+"/broken" {
		t.Errorf("broken URL = %q; want %q", records[1][4], server.URL+"/broken")
	}
}

func TestProcessLinkOutsideAllowedStillChecked(t *testing.T) {
	// The broken link points to a server that is NOT part of the allowed URLs.
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer external.Close()

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
	}))
	defer site.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     site.URL,
		AllowedURLs: site.URL,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	linkURL := external.URL + "/broken"
	c.process(Link{SourcePage: site.URL, URL: linkURL, Type: LinkTypeHyperlink})

	select {
	case res := <-c.resultCh:
		if res.statusCode != 404 {
			t.Errorf("status = %d; want 404", res.statusCode)
		}
		if res.brokenURL != linkURL {
			t.Errorf("brokenURL = %q; want %q", res.brokenURL, linkURL)
		}
	default:
		t.Error("expected a broken link result even though the URL is outside the allowed prefixes")
	}
}

func TestProcessLinkOutsideAllowedNotCrawled(t *testing.T) {
	// The external page is outside the allowed prefixes: it should still be
	// fetched and recorded, but its links must not be crawled.
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="/child">Child</a>`))
	}))
	defer external.Close()

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
	}))
	defer site.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     site.URL,
		AllowedURLs: site.URL,
		UserAgent:   "test",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.process(Link{SourcePage: site.URL, URL: external.URL + "/", Type: LinkTypeHyperlink})

	if _, ok := c.visited.Load(external.URL + "/child"); ok {
		t.Error("expected links on a page outside the allowed prefixes NOT to be crawled")
	}

	var mimeCount int64
	c.stats.mu.Lock()
	mimeCount = c.stats.resourcesByMime["text/html"]
	c.stats.mu.Unlock()
	if mimeCount != 1 {
		t.Errorf("expected the fetched external page to be recorded as visited text/html, got %d", mimeCount)
	}
}

func TestSanitizeLog(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"plain text", "plain text"},
		{"a\rb\nc\td\x00e", "a_b_c_d_e"},
		{"tab\there", "tab_here"},
	}
	for _, tt := range tests {
		if got := sanitizeLog(tt.in); got != tt.want {
			t.Errorf("sanitizeLog(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestAtLinkLimit(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		MaxLinks:   1,
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	if c.atLinkLimit() {
		t.Fatal("atLinkLimit() = true before reaching the limit")
	}
	if c.capLogged.Load() {
		t.Error("capLogged should not be set before reaching the limit")
	}

	c.visitedCount.Add(1)
	if !c.atLinkLimit() {
		t.Error("atLinkLimit() = false after reaching the limit")
	}
	if !c.capLogged.Load() {
		t.Error("capLogged should be set once the limit is reached")
	}
	if !c.atLinkLimit() {
		t.Error("atLinkLimit() = false when still at the limit")
	}
}

func TestSafeProcessRecoversPanic(t *testing.T) {
	c := &Crawler{
		cfg:      nil, // process() dereferences c.cfg.Verbose, forcing a panic.
		stats:    NewStats(),
		resultCh: make(chan BrokenLink, 1),
	}

	c.safeProcess(Link{URL: "https://example.com/", Type: LinkTypeHyperlink})

	select {
	case res := <-c.resultCh:
		if res.statusCode != errCodeProcessingPanic {
			t.Errorf("statusCode = %d; want %d", res.statusCode, errCodeProcessingPanic)
		}
		if res.errorMsg == "" {
			t.Error("expected a non-empty error message describing the panic")
		}
	default:
		t.Error("expected a broken-link result after panic recovery")
	}
}

func TestProcessTooManyRequestsDecreasesRate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    server.URL,
		UserAgent:  "test",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Build the crawler manually so the auto-increase goroutine is not started,
	// keeping the rate assertions deterministic.
	c := &Crawler{
		cfg:        cfg,
		httpClient: &http.Client{},
		rateLimiter: &RateLimiter{
			rate:        maxReqsPerSecond,
			initialRate: maxReqsPerSecond,
			stopCh:      make(chan struct{}),
		},
		stats:    NewStats(),
		resultCh: make(chan BrokenLink, 1),
		visited:  sync.Map{},
	}

	c.process(Link{URL: server.URL, Type: LinkTypeHyperlink})

	if got := c.rateLimiter.rate; got != maxReqsPerSecond-1 {
		t.Errorf("rate after 429 = %d; want %d", got, maxReqsPerSecond-1)
	}
}

func TestMimeType(t *testing.T) {
	c := &Crawler{}
	tests := []struct{ in, want string }{
		{"text/html", "text/html"},
		{"text/html; charset=utf-8", "text/html"},
		{"  application/json ; charset=utf-8 ", "application/json"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := c.mimeType(tt.in); got != tt.want {
			t.Errorf("mimeType(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestStatusMessage(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want string
	}{
		{ErrorCode(http.StatusNotFound), "Not Found"},
		{errCodeNotFound, "Not Found"},
		{ErrorCode(http.StatusTeapot), "I'm a teapot"},
		{ErrorCode(9999), "HTTP status 9999"},
	}
	for _, tt := range tests {
		if got := statusMessage(tt.code); got != tt.want {
			t.Errorf("statusMessage(%d) = %q; want %q", tt.code, got, tt.want)
		}
	}
}

func TestProcessRequestConstructionFails(t *testing.T) {
	// A URL that http.NewRequest rejects makes do() report the construction
	// error and return nil, so process() must bail out without crashing.
	c := &Crawler{
		cfg:        &Config{UserAgent: "test"},
		httpClient: &http.Client{},
		stats:      NewStats(),
		resultCh:   make(chan BrokenLink, 1),
	}

	c.process(Link{URL: "http://[::1", Type: LinkTypeHyperlink})

	select {
	case res := <-c.resultCh:
		if res.statusCode != errCodeRequestConstruction {
			t.Errorf("statusCode = %d; want %d", res.statusCode, errCodeRequestConstruction)
		}
	default:
		t.Error("expected a broken-link result for the request construction error")
	}
}

// headThenGETFails is a RoundTripper that answers HEAD with 405 and fails GET,
// exercising the "HEAD unsupported, GET fallback also fails" branch of process.
type headThenGETFails struct {
	head, get atomic.Int32
}

func (t *headThenGETFails) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == "HEAD" {
		t.head.Add(1)
		return &http.Response{
			StatusCode: http.StatusMethodNotAllowed,
			Status:     "405 Method Not Allowed",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}
	t.get.Add(1)
	return nil, errors.New("get failed")
}

func TestProcessHeadFallbackGETFails(t *testing.T) {
	transport := &headThenGETFails{}
	c := &Crawler{
		cfg:        &Config{UserAgent: "test"},
		httpClient: &http.Client{Transport: transport},
		stats:      NewStats(),
		resultCh:   make(chan BrokenLink, 2),
	}

	c.process(Link{URL: "https://example.com/img.png", Type: LinkTypeImage})

	if transport.head.Load() != 1 {
		t.Errorf("HEAD requests = %d; want 1", transport.head.Load())
	}
	if transport.get.Load() != 1 {
		t.Errorf("GET fallback requests = %d; want 1", transport.get.Load())
	}

	select {
	case res := <-c.resultCh:
		if res.statusCode != errCodeRequestFailed {
			t.Errorf("statusCode = %d; want %d", res.statusCode, errCodeRequestFailed)
		}
	default:
		t.Error("expected a broken-link result when the GET fallback also fails")
	}
}

func TestEnqueueLinkDeduplicates(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	l := Link{URL: "https://example.com/page", Type: LinkTypeHyperlink}
	c.enqueueLink(l)
	c.enqueueLink(l)

	if got := c.visitedCount.Load(); got != 1 {
		t.Errorf("visitedCount = %d; want 1 (duplicate must not be re-enqueued)", got)
	}
	<-c.linkCh
	select {
	case <-c.linkCh:
		t.Error("expected only one link on the channel")
	default:
	}
}

func TestEnqueueLinkStopsAtLimit(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		MaxLinks:   1,
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	c.enqueueLink(Link{URL: "https://example.com/one", Type: LinkTypeHyperlink})
	c.enqueueLink(Link{URL: "https://example.com/two", Type: LinkTypeHyperlink})

	if got := c.visitedCount.Load(); got != 1 {
		t.Errorf("visitedCount = %d; want 1 (second link must be dropped at the limit)", got)
	}
	<-c.linkCh
	select {
	case <-c.linkCh:
		t.Error("expected only one link on the channel")
	default:
	}
}

func TestRunWithUnwritableLogFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<a href="/page1">Page 1</a>`))
	}))
	defer server.Close()

	outputFile := t.TempDir() + "/test_output.csv"
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     server.URL + "/",
		AllowedURLs: server.URL,
		MaxReqs:     1000,
		Workers:     2,
		UserAgent:   "test",
		OutputFile:  outputFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Point the stats log at an unwritable location so Run falls back to
	// printing the summary on stdout.
	cfg.LogFile = t.TempDir() + "/no/such/dir/stats.log"

	c := mustNewCrawler(t, cfg)
	c.Run()
	c.Close()

	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Error("output CSV should still be written even when the stats log fails")
	}
}

func TestCrawlerCloseWithResultWriterError(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	rw, err := newResultWriter(cfg.OutputFile)
	if err != nil {
		t.Fatal(err)
	}

	c := &Crawler{
		cfg:         cfg,
		rateLimiter: NewRateLimiter(cfg.MaxReqs),
		httpClient:  newHTTPClient(cfg.allowedURL, cfg.excludedURL, cfg.InsecureTLS),
		resultW:     rw,
		stats:       NewStats(),
	}

	rw.mu.Lock()
	rw.err = errors.New("boom")
	rw.mu.Unlock()

	c.Close()
}

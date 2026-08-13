package main

import (
	"net/http"
	"net/http/httptest"
	"os"
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

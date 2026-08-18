package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
)

// Crawler manages the state, concurrency, and rate limiting of the crawl.
type Crawler struct {
	cfg         *Config       // holds the crawl configuration.
	httpClient  *http.Client  // performs HTTP requests with scope and TLS settings applied.
	rateLimiter *RateLimiter  // throttles requests to the configured rate.
	resultW     *resultWriter // writes broken-link records to the CSV output.
	stats       *Stats        // accumulates crawl counters.

	visited      sync.Map        // tracks URLs that have already been enqueued.
	visitedCount atomic.Int64    // number of unique links enqueued.
	capLogged    atomic.Bool     // records whether the link limit warning was logged.
	wg           sync.WaitGroup  // tracks in-flight link processing.
	consumerWg   sync.WaitGroup  // tracks the result consumer goroutine.
	linkCh       chan Link       // delivers discovered links to workers.
	resultCh     chan BrokenLink // delivers broken links to the consumer.
}

// Link represents a discovered URL and its context within the crawled site.
type Link struct {
	SourcePage string   // URL of the page where this link was found.
	URL        string   // resolved absolute URL.
	Type       LinkType // category of the resource.
	LinkName   string   // visible link text (only for hyperlinks).
	Selector   string   // CSS-like path identifying the element's location on the page.
}

// NewCrawler creates a fully initialised Crawler from a Config, writing broken
// link results to the provided result writer. The caller owns resultW and is
// responsible for closing it (Crawler.Close closes it alongside the crawler).
func NewCrawler(cfg *Config, resultW *resultWriter) *Crawler {
	return &Crawler{
		cfg:         cfg,
		httpClient:  newHTTPClient(cfg.allowedURL, cfg.excludedURL, cfg.InsecureTLS),
		rateLimiter: NewRateLimiter(cfg.MaxReqs),
		resultW:     resultW,
		stats:       NewStats(),
		linkCh:      make(chan Link, linkQueueSize),
		resultCh:    make(chan BrokenLink, resultsQueueSize),
	}
}

// Close releases the crawler's underlying resources (rate limiter, HTTP connections, result writer).
func (c *Crawler) Close() {
	c.rateLimiter.Stop()
	c.httpClient.CloseIdleConnections()
	if err := c.resultW.Close(); err != nil {
		log.Printf("failed to close result writer: %v", err)
	}
}

// Run starts the worker goroutines, seeds the crawl, and blocks until all links are processed.
// After Run returns the link and result channels are closed.
func (c *Crawler) Run() {
	c.consumerWg.Go(func() {
		for res := range c.resultCh {
			c.resultW.writeBrokenLink(res)

			log.Printf("broken %s - %s in page %s - Status %d - error - %s", res.linkType, sanitizeLog(res.brokenURL), sanitizeLog(res.sourcePage), res.statusCode, sanitizeLog(res.errorMsg))
		}
	})

	for range c.cfg.Workers {
		go c.worker()
	}

	log.Printf("Starting crawl on %s...", c.cfg.BaseURL)
	c.enqueueLink(Link{SourcePage: "", URL: c.cfg.BaseURL, Type: LinkTypeHyperlink})
	c.wg.Wait()
	close(c.linkCh)
	close(c.resultCh)
	c.consumerWg.Wait()

	if err := c.resultW.Err(); err != nil {
		log.Printf("result writer error: %v", err)
	}

	logFile, err := os.Create(c.cfg.LogFile)
	if err != nil {
		log.Printf("Failed to create stats log file: %v", err)
		c.stats.Print(os.Stdout)

		return
	}

	c.stats.Print(io.MultiWriter(os.Stdout, logFile))
	logFile.Close()
}

// sanitizeLog neutralises control characters in remote-controlled strings
// (URLs, server status text, error messages) before they reach a log line, so a
// hostile page cannot forge or corrupt log records with embedded newlines/CRs.
func sanitizeLog(s string) string {
	if s == "" {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r < 0x20 {
			return '_'
		}
		return r
	}, s)
}

// worker is the main loop of a crawl goroutine: it consumes links and spawns a
// processing goroutine for each, so the worker immediately returns to draining
// linkCh. Processing enqueues newly discovered links on the same channel, so
// when the buffer is full the processing goroutines block on send while the
// worker pool keeps receiving; processing inline could instead leave every
// worker blocked on a send with a full buffer and no receiver left to drain it.
func (c *Crawler) worker() {
	for link := range c.linkCh {
		c.rateLimiter.Wait()
		go c.processAndDone(link)
	}
}

// processAndDone runs safeProcess for a link and accounts for its completion.
// Each link is counted by enqueueLink with wg.Add(1), balanced by the Done here.
func (c *Crawler) processAndDone(link Link) {
	defer c.wg.Done()
	c.safeProcess(link)
}

// safeProcess runs process, converting any panic into a recorded broken-link
// error so a single bad page neither crashes the crawl nor deadlocks wg.Wait.
func (c *Crawler) safeProcess(link Link) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic while processing %s: %v\n%s", link.URL, r, debug.Stack())
			c.reportError(link, errCodeProcessingPanic, fmt.Sprintf("panic: %v", r))
		}
	}()

	c.process(link)
}

// process fetches a single link, checks its status, and optionally parses its HTML for more links.
func (c *Crawler) process(link Link) {
	if c.cfg.Verbose {
		log.Printf("Crawling: %s", sanitizeLog(link.URL))
	}
	method := "GET"
	if link.Type != LinkTypeHyperlink {
		method = "HEAD"
	}

	// With -no-external, out-of-scope links are never requested, so pages on a
	// sensitive network cannot be made to probe hosts outside the allowed
	// prefixes merely by referencing them in crawled HTML.
	if c.cfg.NoExternal && !c.isAllowed(link.URL) {
		return
	}

	resp := c.do(link, method)
	if resp == nil {
		return
	}

	// Servers that don't implement HEAD commonly answer 405/501 even though the
	// resource itself is fine, so verify asset links with GET before declaring them broken.
	if link.Type != LinkTypeHyperlink && (resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented) {
		resp.Body.Close()
		resp = c.do(link, "GET")
		if resp == nil {
			return
		}
	}

	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		c.rateLimiter.Decrease()

	default:
		if resp.StatusCode >= 400 {
			c.reportError(link, ErrorCode(resp.StatusCode), statusMessage(ErrorCode(resp.StatusCode)))

			return
		}
	}

	mimeType := c.mimeType(contentType)
	c.stats.RecordMime(mimeType)

	if link.Type != LinkTypeHyperlink {
		return
	}

	if !c.isAllowed(link.URL) {
		return
	}

	if strings.Contains(contentType, "text/html") {
		body := io.LimitReader(resp.Body, maxBodySize)
		page := NewHTMLPage(c, body, link.URL)
		page.ExtractLinks()
	} else if link.SourcePage == "" {
		c.reportError(link, ErrorCode(resp.StatusCode), "initial URL is not an HTML page")
	}
}

// statusMessage returns a human-readable description for an error code. Defined
// codes use their ErrorCode label; any other real HTTP status falls back to the
// standard reason phrase so an undefined code never surfaces as "ErrorCode(N)".
func statusMessage(code ErrorCode) string {
	if label := code.String(); !strings.HasPrefix(label, "ErrorCode(") {
		return label
	}
	if text := http.StatusText(int(code)); text != "" {
		return text
	}
	return fmt.Sprintf("HTTP status %d", code)
}

// mimeType returns the MIME type of a Content-Type header, dropping any
// parameters (e.g. "text/html; charset=utf-8" -> "text/html").
func (c *Crawler) mimeType(contentType string) string {
	before, _, ok := strings.Cut(contentType, ";")
	if ok {
		return strings.TrimSpace(before)
	}

	return strings.TrimSpace(contentType)
}

// enqueueLink sends a link to the channel unless it has already been visited.
func (c *Crawler) enqueueLink(l Link) {
	if c.atLinkLimit() {
		return
	}

	if _, loaded := c.visited.LoadOrStore(l.URL, true); !loaded {
		c.visitedCount.Add(1)
		c.stats.RecordLink(l.Type)
		c.wg.Add(1)
		c.linkCh <- l
	}
}

// atLinkLimit reports whether the number of unique links already queued has
// reached the configured maximum, bounding memory use on very large sites. It
// logs a one-time warning when the limit is first reached.
func (c *Crawler) atLinkLimit() bool {
	if c.visitedCount.Load() >= int64(c.cfg.MaxLinks) {
		if c.capLogged.CompareAndSwap(false, true) {
			log.Printf("reached the maximum of %d unique links; stopping discovery of new links", c.cfg.MaxLinks)
		}
		return true
	}

	return false
}

// reportError records a broken-link in the stats and sends it to the resultCh channel.
func (c *Crawler) reportError(link Link, statusCode ErrorCode, errMsg string) {
	c.stats.RecordError(link.Type, statusCode)

	c.resultCh <- BrokenLink{
		sourcePage: link.SourcePage,
		linkType:   link.Type,
		brokenURL:  link.URL,
		statusCode: statusCode,
		errorMsg:   errMsg,
		linkName:   link.LinkName,
		selector:   link.Selector,
	}
}

// isAllowed determines if a given URL is permitted based on allowed and excluded URL prefix lists.
func (c *Crawler) isAllowed(targetURL string) bool {
	target, err := url.Parse(targetURL)
	if err != nil {
		return false
	}

	for i := range c.cfg.excludedURL {
		if urlMatchesPrefix(target, &c.cfg.excludedURL[i]) {
			return false
		}
	}

	for i := range c.cfg.allowedURL {
		if urlMatchesPrefix(target, &c.cfg.allowedURL[i]) {
			return true
		}
	}

	return false
}

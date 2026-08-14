package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const (
	appPrefix        = "pipolinkcheck"                                  // appPrefix is the application name, used in default filenames.
	defaultRootURL   = "https://example.com/"                           // defaultRootURL is the root URL used when none is provided.
	defaultUserAgent = "Mozilla/5.0 (compatible; PipoLinkCheckBot/1.0)" // defaultUserAgent is sent with every HTTP request.
	linkQueueSize    = 10000                                            // linkQueueSize is the buffered size of the discovered-link channel.
	resultsQueueSize = 100                                              // resultsQueueSize is the buffered size of the broken-link channel.
	requestTimeout   = 15 * time.Second                                 // requestTimeout bounds each individual HTTP request.
	maxRetries       = 3                                                // maxRetries is the number of retry attempts after a transient request failure.
	backoffBase      = 200 * time.Millisecond                           // backoffBase is the initial delay before the first retry.
	backoffMax       = 2 * time.Second                                  // backoffMax caps the delay between retries.
	maxRedirects     = 10                                               // maxRedirects caps the number of redirects followed per request.
	maxBodySize      = 10 * 1024 * 1024                                 // maxBodySize caps the number of HTML bytes parsed for links.
	minWorkers       = 2                                                // minWorkers is the minimum number of crawl worker goroutines.
	maxWorkers       = 16                                               // maxWorkers is the maximum number of crawl worker goroutines.
	minReqsPerSecond = 2                                                // minReqsPerSecond is the lowest permitted requests-per-second rate.
	maxReqsPerSecond = 20                                               // maxReqsPerSecond is the highest permitted requests-per-second rate.
	defaultMaxLinks  = 100000                                           // defaultMaxLinks bounds the number of unique links crawled.
	flushInterval    = 500 * time.Millisecond                           // flushInterval is how often buffered CSV results are flushed.
	csvSeparator     = ';'                                              // csvSeparator separates fields in the output CSV file.
)

var (
	errInvalidRootURL = fmt.Errorf("invalid root URL")
)

var tagMap = map[string]struct {
	linkType LinkType // linkType is the kind of resource the element points to.
	urlAttr  string   // urlAttr is the attribute holding the element's URL.
	nameAttr string   // nameAttr is the attribute holding the element's link text.
}{
	"a":      {linkType: LinkTypeHyperlink, urlAttr: "href", nameAttr: "title"},
	"img":    {linkType: LinkTypeImage, urlAttr: "src", nameAttr: "alt"},
	"link":   {linkType: LinkTypeCSS, urlAttr: "href", nameAttr: "title"},
	"script": {linkType: LinkTypeScript, urlAttr: "src", nameAttr: "title"},
	"video":  {linkType: LinkTypeVideo, urlAttr: "src", nameAttr: "title"},
	"source": {linkType: LinkTypeVideo, urlAttr: "src", nameAttr: "title"},
}

// Config holds all user-configurable settings for the crawl.
type Config struct {
	RootURL      string // RootURL is the starting URL of the crawl.
	AllowedURLs  string // AllowedURLs is a comma-separated list of allowed URL prefixes.
	ExcludedURLs string // ExcludedURLs is a comma-separated list of excluded URL prefixes.
	UserAgent    string // UserAgent is sent with every HTTP request.
	MaxReqs      int    // MaxReqs is the maximum requests per second.
	Workers      int    // Workers is the number of parallel workers (0 for auto-compute).
	MaxLinks     int    // MaxLinks bounds the number of unique links crawled.
	OutputFile   string // OutputFile is the path of the output CSV file.
	LogFile      string // LogFile is the path of the stats log file.
	Verbose      bool   // Verbose enables detailed logging output.
	InsecureTLS  bool   // InsecureTLS skips TLS certificate verification.

	allowedURL  []url.URL // allowedURL holds the parsed allowed URL prefixes.
	excludedURL []url.URL // excludedURL holds the parsed excluded URL prefixes.
}

// NewConfig parses command-line flags and returns a populated Config.
func NewConfig() (*Config, error) {
	cfg := &Config{}
	fs := newFlagSet(cfg)
	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	if err := cfg.finalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// newFlagSet registers the command-line flags on a local FlagSet, leaving the
// process-global flag.CommandLine untouched so tests can parse independently.
func newFlagSet(cfg *Config) *flag.FlagSet {
	fs := flag.NewFlagSet("pipolinkcheck", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [options]\n\nOptions:\n", os.Args[0])
		fs.PrintDefaults()
	}

	fs.StringVar(&cfg.RootURL, "root", defaultRootURL, "Root URL to start crawling from")
	fs.StringVar(&cfg.AllowedURLs, "allowed", "", "Comma-separated list of allowed URL prefixes")
	fs.StringVar(&cfg.ExcludedURLs, "excluded", "", "Comma-separated list of excluded URL prefixes")
	fs.IntVar(&cfg.MaxReqs, "maxreqs", maxReqsPerSecond, "Maximum requests per second")
	fs.IntVar(&cfg.Workers, "workers", 0, "Number of parallel workers (0 for auto-compute based on CPUs)")
	fs.IntVar(&cfg.MaxLinks, "maxlinks", defaultMaxLinks, "Maximum number of unique links to crawl (0 for default)")
	fs.StringVar(&cfg.OutputFile, "output", "", "Output CSV file path")
	fs.StringVar(&cfg.UserAgent, "user-agent", defaultUserAgent, "User-Agent header sent with every request")
	fs.BoolVar(&cfg.Verbose, "verbose", true, "detailed logging output for debugging and monitoring purposes")
	fs.BoolVar(&cfg.InsecureTLS, "insecure-tls", false, "skips TLS certificate verification (not recommended)")

	return fs
}

// printUsage prints flag help to standard error.
func printUsage() {
	newFlagSet(&Config{}).Usage()
}

// NewConfigWithOptions creates and returns a new Config initialized with the provided values.
func NewConfigWithOptions(opts *Config) (*Config, error) {
	c := *opts
	if err := c.finalize(); err != nil {
		return nil, err
	}
	return &c, nil
}

// finalize parses URL scopes and applies defaults and bounds to the Config.
func (c *Config) finalize() error {
	if _, err := url.Parse(c.RootURL); err != nil {
		return fmt.Errorf("%w: %v", errInvalidRootURL, err)
	}

	baseURL, err := url.Parse(c.RootURL)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidRootURL, err)
	}

	allowedURL, err := parseURLList(c.AllowedURLs, c.RootURL, baseURL)
	if err != nil {
		return fmt.Errorf("invalid allowed URL: %w", err)
	}

	excludedURL, err := parseURLList(c.ExcludedURLs, "", baseURL)
	if err != nil {
		return fmt.Errorf("invalid excluded URL: %w", err)
	}

	c.allowedURL = allowedURL
	c.excludedURL = excludedURL
	c.MaxReqs = clampReqsPerSecond(c.MaxReqs)

	n := runtime.NumCPU() - 1
	if c.Workers > 0 {
		n = c.Workers
	}
	c.Workers = max(minWorkers, min(maxWorkers, n))

	if c.MaxLinks <= 0 {
		c.MaxLinks = defaultMaxLinks
	}
	if c.UserAgent == "" {
		c.UserAgent = defaultUserAgent
	}
	if c.OutputFile == "" {
		c.OutputFile = defaultOutputFileFromURL(c.RootURL)
	}
	c.LogFile = logPathForOutput(c.OutputFile)

	return nil
}

// parseURLList parses a comma-separated list of URL prefixes, falling back to
// def when rawList is empty. It parses and validates each entry up front so the
// crawl never reparses them, and normalises hosts to lowercase so look-alike
// casing can never leak into the stored scopes. Entries that are relative to
// the root (e.g. "/test") are resolved against baseURL so they become absolute
// prefixes.
func parseURLList(rawList, def string, baseURL *url.URL) ([]url.URL, error) {
	rawList = strings.TrimSpace(rawList)
	if rawList == "" {
		rawList = def
	}
	if rawList == "" {
		return nil, nil
	}

	list := make([]url.URL, 0, 4)
	for item := range strings.SplitSeq(rawList, ",") {
		u, err := url.Parse(item)
		if err != nil {
			return nil, err
		}
		if u.Scheme == "" && baseURL != nil {
			u = baseURL.ResolveReference(u)
		}
		u.Host = strings.ToLower(u.Host)
		list = append(list, *u)
	}

	return list, nil
}

// defaultOutputFileFromURL generates a default CSV filename from a URL.
// It strips the scheme and replaces characters unsafe for filenames with '_'.
func defaultOutputFileFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	base := u.Host + u.Path

	base = strings.TrimRight(base, "/")
	base = sanitizeFilename(base)

	return appPrefix + "-" + base + ".csv"
}

// logPathForOutput returns a stats log path for the given output file by
// replacing its extension with ".log" (e.g. "out.csv" -> "out.log",
// "out.txt" -> "out.log", "out" -> "out.log").
func logPathForOutput(outputFile string) string {
	if i := strings.LastIndexByte(outputFile, '.'); i != -1 {
		return outputFile[:i] + ".log"
	}

	return outputFile + ".log"
}

// sanitizeFilename replaces any character that isn't alphanumeric, dash, dot, or underscore with '-'.
func sanitizeFilename(s string) string {
	var safe strings.Builder
	safe.Grow(len(s))

	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_') {
			r = '-'
		}
		safe.WriteRune(r)
	}

	return safe.String()
}

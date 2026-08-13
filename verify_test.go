package main

import "testing"

func TestIsAllowed(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     "https://example.com",
		AllowedURLs: "https://example.com,https://sub.example.com",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/page", true},
		{"https://example.com/", true},
		{"https://sub.example.com/path", true},
		{"https://other.com/", false},
	}
	for _, tt := range tests {
		got := c.isAllowed(tt.url)
		if got != tt.want {
			t.Errorf("isAllowed(%q) = %v; want %v", tt.url, got, tt.want)
		}
	}
}

func TestIsAllowedWithExcluded(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:      "https://example.com",
		AllowedURLs:  "https://example.com",
		ExcludedURLs: "https://example.com/excluded",
		OutputFile:   t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/page", true},
		{"https://example.com/excluded/foo", false},
		{"https://example.com/excluded", false},
		{"https://other.com/page", false},
	}
	for _, tt := range tests {
		got := c.isAllowed(tt.url)
		if got != tt.want {
			t.Errorf("isAllowed(%q) = %v; want %v", tt.url, got, tt.want)
		}
	}
}

func TestIsAllowedRejectsLookAlikeHost(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:      "https://example.com/guide/",
		AllowedURLs:  "https://example.com/guide/",
		ExcludedURLs: "https://example.com/guide/private/",
		OutputFile:   t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/guide/", true},
		{"https://example.com/guide/page", true},
		{"https://example.com/guide2/", false},
		{"https://example.com.evil.com/guide/", false},
		{"http://example.com/guide/", false},
		{"https://sub.example.com/guide/", false},
		{"https://example.com/guide/private/", false},
		{"https://example.com/guide/privatex", true},
	}
	for _, tt := range tests {
		got := c.isAllowed(tt.url)
		if got != tt.want {
			t.Errorf("isAllowed(%q) = %v; want %v", tt.url, got, tt.want)
		}
	}
}

func TestReportError(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	link := Link{SourcePage: "src", URL: "https://example.com/broken", Type: LinkTypeHyperlink}

	c.reportError(link, 404, "")
	select {
	case res := <-c.resultCh:
		if res.sourcePage != "src" || res.brokenURL != "https://example.com/broken" || res.statusCode != 404 {
			t.Errorf("unexpected result: %+v", res)
		}
	default:
		t.Error("expected a result on the channel")
	}

	c.reportError(link, 0, "connection refused")
	select {
	case res := <-c.resultCh:
		if res.errorMsg != "connection refused" || res.statusCode != 0 {
			t.Errorf("unexpected result: %+v", res)
		}
	default:
		t.Error("expected a result on the channel")
	}
}

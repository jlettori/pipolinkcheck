package main

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestNewConfigDefaults(t *testing.T) {
	saveArgs := os.Args
	defer func() { os.Args = saveArgs }()

	os.Args = []string{"linkcheck"}

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() unexpected error: %v", err)
	}

	if cfg.BaseURL != defaultBaseURL {
		t.Errorf("BaseURL = %q; want %q", cfg.BaseURL, defaultBaseURL)
	}
	if len(cfg.allowedURL) != 1 || cfg.allowedURL[0].String() != defaultBaseURL {
		t.Errorf("AllowedPrefix = %v; want [%q]", cfg.allowedURL, defaultBaseURL)
	}
	if cfg.MaxReqs != maxReqsPerSecond {
		t.Errorf("RateLimit = %d; want %d", cfg.MaxReqs, maxReqsPerSecond)
	}
	wantWorkers := max(minWorkers, min(maxWorkers, runtime.NumCPU()-1))
	if cfg.Workers != wantWorkers {
		t.Errorf("Workers = %d; want %d", cfg.Workers, wantWorkers)
	}
	if cfg.UserAgent != defaultUserAgent {
		t.Errorf("UserAgent = %q; want %q", cfg.UserAgent, defaultUserAgent)
	}
	wantOutput := appPrefix + "-example.com.csv"
	if cfg.OutputFile != wantOutput {
		t.Errorf("OutputFile = %q; want %q", cfg.OutputFile, wantOutput)
	}
}

func TestNewConfigCustom(t *testing.T) {
	saveArgs := os.Args
	defer func() { os.Args = saveArgs }()

	os.Args = []string{
		"linkcheck",
		"-base", "https://example.com",
		"-allowed", "https://example.com,https://sub.example.com",
		"-maxreqs", "20",
		"-user-agent", "MyBot/1.0",
		"-output", "/tmp/results.csv",
	}

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() unexpected error: %v", err)
	}

	if cfg.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q; want %q", cfg.BaseURL, "https://example.com")
	}
	wantAllowed := []string{"https://example.com", "https://sub.example.com"}
	if len(cfg.allowedURL) != len(wantAllowed) {
		t.Errorf("AllowedPrefix length = %d; want %d", len(cfg.allowedURL), len(wantAllowed))
	}
	for i := range wantAllowed {
		if cfg.allowedURL[i].String() != wantAllowed[i] {
			t.Errorf("AllowedPrefix[%d] = %q; want %q", i, cfg.allowedURL[i].String(), wantAllowed[i])
		}
	}
	if cfg.MaxReqs != 20 {
		t.Errorf("RateLimit = %d; want 20", cfg.MaxReqs)
	}
	wantWorkers := max(minWorkers, min(maxWorkers, runtime.NumCPU()-1))
	if cfg.Workers != wantWorkers {
		t.Errorf("Workers = %d; want %d", cfg.Workers, wantWorkers)
	}
	if cfg.UserAgent != "MyBot/1.0" {
		t.Errorf("UserAgent = %q; want %q", cfg.UserAgent, "MyBot/1.0")
	}
	if cfg.OutputFile != "/tmp/results.csv" {
		t.Errorf("OutputFile = %q; want %q", cfg.OutputFile, "/tmp/results.csv")
	}
}

func TestNewConfigAllowedDefaultIsRoot(t *testing.T) {
	saveArgs := os.Args
	defer func() { os.Args = saveArgs }()

	os.Args = []string{"linkcheck", "-base", "https://custom-base.com"}

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() unexpected error: %v", err)
	}

	if cfg.BaseURL != "https://custom-base.com" {
		t.Errorf("BaseURL = %q; want %q", cfg.BaseURL, "https://custom-base.com")
	}
	if len(cfg.allowedURL) != 1 || cfg.allowedURL[0].String() != "https://custom-base.com" {
		t.Errorf("AllowedPrefix = %v; want [%q]", cfg.allowedURL, "https://custom-base.com")
	}
}

func TestNewConfigUnexpectedArgs(t *testing.T) {
	saveArgs := os.Args
	defer func() { os.Args = saveArgs }()

	os.Args = []string{"linkcheck", "extra-arg"}

	_, err := NewConfig()
	if err == nil {
		t.Fatal("NewConfig() expected error for unexpected arguments, got nil")
	}
}

func TestNewConfigFlagParseError(t *testing.T) {
	saveArgs := os.Args
	defer func() { os.Args = saveArgs }()

	os.Args = []string{"linkcheck", "-unknown-flag"}

	_, err := NewConfig()
	if err == nil {
		t.Fatal("NewConfig() expected error for unknown flag, got nil")
	}
}

func TestNewConfigWithOptionsInvalidExcludedURL(t *testing.T) {
	opts := &Config{
		BaseURL:      "https://example.com",
		ExcludedURLs: "://invalid",
	}
	_, err := NewConfigWithOptions(opts)
	if err == nil {
		t.Fatal("expected error for invalid excluded URL, got nil")
	}
}

func TestNewConfigWithOptionsEmptyUserAgentDefaults(t *testing.T) {
	opts := &Config{
		BaseURL: "https://example.com",
	}
	cfg, err := NewConfigWithOptions(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UserAgent != defaultUserAgent {
		t.Errorf("userAgent = %q; want %q", cfg.UserAgent, defaultUserAgent)
	}
}

func TestNewConfigWithOptionsEmptyOutputFileDefaults(t *testing.T) {
	opts := &Config{
		BaseURL: "https://example.com",
	}
	cfg, err := NewConfigWithOptions(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantOutput := appPrefix + "-example.com.csv"
	if cfg.OutputFile != wantOutput {
		t.Errorf("outputFile = %q; want %q", cfg.OutputFile, wantOutput)
	}
}

func TestNewConfigWithOptionsMaxReqsZero(t *testing.T) {
	opts := &Config{
		BaseURL: "https://example.com",
		MaxReqs: 0,
	}
	cfg, err := NewConfigWithOptions(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxReqs != minReqsPerSecond {
		t.Errorf("maxReqs = %d; want %d", cfg.MaxReqs, minReqsPerSecond)
	}
}

func TestNewConfigWithOptionsExcludedURL(t *testing.T) {
	opts := &Config{
		BaseURL:      "https://example.com",
		ExcludedURLs: "https://example.com/excluded",
	}
	cfg, err := NewConfigWithOptions(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.excludedURL) != 1 || cfg.excludedURL[0].String() != "https://example.com/excluded" {
		t.Errorf("excludedURL = %v; want [%q]", cfg.excludedURL, "https://example.com/excluded")
	}
}

func TestNewConfigWithOptionsRelativeURLsResolvedToRoot(t *testing.T) {
	opts := &Config{
		BaseURL:      "https://example.com/",
		AllowedURLs:  "/test,/api",
		ExcludedURLs: "/private",
	}
	cfg, err := NewConfigWithOptions(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAllowed := []string{"https://example.com/test", "https://example.com/api"}
	if len(cfg.allowedURL) != len(wantAllowed) {
		t.Fatalf("allowedURL length = %d; want %d", len(cfg.allowedURL), len(wantAllowed))
	}
	for i := range wantAllowed {
		if cfg.allowedURL[i].String() != wantAllowed[i] {
			t.Errorf("allowedURL[%d] = %q; want %q", i, cfg.allowedURL[i].String(), wantAllowed[i])
		}
	}

	if len(cfg.excludedURL) != 1 || cfg.excludedURL[0].String() != "https://example.com/private" {
		t.Errorf("excludedURL = %v; want [%q]", cfg.excludedURL, "https://example.com/private")
	}
}

func TestNewConfigWithOptionsInvalidBaseURL(t *testing.T) {
	opts := &Config{
		BaseURL: "://invalid",
	}
	_, err := NewConfigWithOptions(opts)
	if !errors.Is(err, errInvalidBaseURL) {
		t.Errorf("expected %v, got %v", errInvalidBaseURL, err)
	}
}

func TestNewConfigWithOptionsStoredHostsLowercase(t *testing.T) {
	opts := &Config{
		BaseURL:      "https://WWW.FRANCETRAVAIL.FR/Region/Corse/",
		AllowedURLs:  "https://WWW.FRANCETRAVAIL.FR,https://Sub.Example.com",
		ExcludedURLs: "https://Sub.Example.com/Prive",
	}
	cfg, err := NewConfigWithOptions(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := range cfg.allowedURL {
		if got := cfg.allowedURL[i].Host; got != strings.ToLower(got) {
			t.Errorf("allowedURL[%d].Host = %q; want lowercase", i, got)
		}
	}
	for i := range cfg.excludedURL {
		if got := cfg.excludedURL[i].Host; got != strings.ToLower(got) {
			t.Errorf("excludedURL[%d].Host = %q; want lowercase", i, got)
		}
	}
}

func TestNewConfigWithOptionsInvalidAllowedURL(t *testing.T) {
	opts := &Config{
		BaseURL:     "https://example.com",
		AllowedURLs: "://bad",
	}
	_, err := NewConfigWithOptions(opts)
	if err == nil {
		t.Fatal("expected error for invalid allowed URL, got nil")
	}
}

func TestNewConfigInvalidBaseURL(t *testing.T) {
	saveArgs := os.Args
	defer func() { os.Args = saveArgs }()

	os.Args = []string{"linkcheck", "-base", "://invalid"}
	_, err := NewConfig()
	if err == nil {
		t.Fatal("NewConfig() expected error for invalid base URL, got nil")
	}
}

func TestPrintUsage(t *testing.T) {
	saveStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = saveStderr }()

	printUsage()
	w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("usage output missing 'Usage:', got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "-base") {
		t.Errorf("usage output missing '-base' flag, got %q", buf.String())
	}
}

func TestLogPathForOutput(t *testing.T) {
	tests := []struct{ in, want string }{
		{"out.csv", "out.log"},
		{"out.txt", "out.log"},
		{"path/to/result.CSV", "path/to/result.log"},
		{"out", "out.log"},
		{"", ".log"},
	}
	for _, tt := range tests {
		if got := logPathForOutput(tt.in); got != tt.want {
			t.Errorf("logPathForOutput(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestDefaultOutputFileFromURLInvalid(t *testing.T) {
	if got := defaultOutputFileFromURL("://invalid"); got != "" {
		t.Errorf("defaultOutputFileFromURL(invalid) = %q; want empty", got)
	}
}

func TestNewConfigWithOptionsMaxLinksZeroDefaults(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		BaseURL:  "https://example.com",
		MaxLinks: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxLinks != defaultMaxLinks {
		t.Errorf("MaxLinks = %d; want default %d", cfg.MaxLinks, defaultMaxLinks)
	}
}

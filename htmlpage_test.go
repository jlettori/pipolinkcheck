package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestGetAttr(t *testing.T) {
	token := html.Token{
		Type: html.StartTagToken,
		Data: "a",
		Attr: []html.Attribute{
			{Key: "href", Val: "https://example.com"},
			{Key: "class", Val: "link"},
		},
	}
	if getAttr(token, "href") != "https://example.com" {
		t.Error("getAttr(href) failed")
	}
	if getAttr(token, "class") != "link" {
		t.Error("getAttr(class) failed")
	}
	if getAttr(token, "missing") != "" {
		t.Error("getAttr(missing) should be empty")
	}
}

func TestResolveURL(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	base, _ := url.Parse("https://example.com/foo/")

	p := &HTMLPage{baseURL: base}

	tests := []struct {
		raw  string
		want string
	}{
		{"https://example.com/page", "https://example.com/page"},
		{"/absolute/path", "https://example.com/absolute/path"},
		{"relative/path", "https://example.com/foo/relative/path"},
		{"https://other.com/", "https://other.com/"},
		{"mailto:user@example.com", ""},
		{"javascript:void(0)", ""},
		{"tel:+1234567890", ""},
	}
	for _, tt := range tests {
		got := p.resolveURL(tt.raw)
		if got != tt.want {
			t.Errorf("resolveURL(%q) = %q; want %q", tt.raw, got, tt.want)
		}
	}

	got := p.resolveURL("/page#section")
	if strings.HasSuffix(got, "#section") {
		t.Errorf("resolveURL should strip fragment, got %q", got)
	}

	if got := p.resolveURL("://invalid"); got != "" {
		t.Errorf("resolveURL with invalid URL should return empty, got %q", got)
	}
}

func TestResolveURLWithParentheses(t *testing.T) {
	base, _ := url.Parse("https://example.com/")

	if got := (&HTMLPage{baseURL: base}).resolveURL("https://example.com/(parens)"); got != "" {
		t.Errorf("resolveURL with parentheses should return empty, got %q", got)
	}
}

func TestResolveURLEmptyAndHash(t *testing.T) {
	base, _ := url.Parse("https://example.com/")

	if got := (&HTMLPage{baseURL: base}).resolveURL(""); got != "" {
		t.Errorf("resolveURL with empty string should return empty, got %q", got)
	}
	if got := (&HTMLPage{baseURL: base}).resolveURL("#"); got != "" {
		t.Errorf("resolveURL with '#' should return empty, got %q", got)
	}
	if got := (&HTMLPage{baseURL: base}).resolveURL("#section"); got != "" {
		t.Errorf("resolveURL with '#section' should return empty, got %q", got)
	}
}

func TestExtractLinks(t *testing.T) {
	htmlContent := `<html><body>
		<a href="/page1">Link 1</a>
		<img src="/img.png">
		<link href="/style.css" rel="stylesheet">
		<script src="/app.js"></script>
		<video src="/video.mp4"></video>
		<a href="https://external.com/">External</a>
		<a href="mailto:test@example.com">Email</a>
		<a href="/page1">Duplicate</a>
		<a href="/page2?foo=bar&baz=1">With query</a>
		<a href="/page3#section">With fragment</a>
		<a href='/page4'>Single quotes</a>
		<a href="/page5?q=a%20b">Encoded space</a>
		<a href="/page'6">Path with single quote</a>
	</body></html>`

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     "https://example.com",
		AllowedURLs: "https://example.com",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(htmlContent)),
	}

	NewHTMLPage(c, resp.Body, "https://example.com/base/").ExtractLinks()

	expected := []string{
		"https://example.com/page1",
		"https://example.com/img.png",
		"https://example.com/style.css",
		"https://example.com/app.js",
		"https://example.com/video.mp4",
		"https://external.com/",
		"https://example.com/page2?foo=bar&baz=1",
		"https://example.com/page3",
		"https://example.com/page4",
		"https://example.com/page5?q=a%20b",
		"https://example.com/page'6",
	}
	for _, u := range expected {
		if _, ok := c.visited.Load(u); !ok {
			t.Errorf("expected %q to be visited", u)
		}
	}

	count := 0
	c.visited.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != len(expected) {
		t.Errorf("expected %d visited URLs, got %d", len(expected), count)
	}

	if _, ok := c.visited.Load("mailto:test@example.com"); ok {
		t.Error("mailto: should not be visited")
	}

loop:
	for {
		select {
		case <-c.linkCh:
		default:
			break loop
		}
	}
}

func TestExtractLinksAnchorNameFromContent(t *testing.T) {
	htmlContent := `<html><body>
		<a href="/page1">Click me</a>
		<a href="/page2" title="Titled link">Ignored text</a>
		<a href="/page3">  Spaced   text  </a>
		<a href="/page4"><span>Nested</span> content</a>
		<a href="/page5"></a>
		<a href="/page6"><img src="/img.png"></a>
	</body></html>`

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:     "https://example.com",
		AllowedURLs: "https://example.com",
		OutputFile:  t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(htmlContent)),
	}

	NewHTMLPage(c, resp.Body, "https://example.com/").ExtractLinks()

	names := map[string]string{}
	for {
		select {
		case l := <-c.linkCh:
			if l.Type == LinkTypeHyperlink {
				names[l.URL] = l.LinkName
			}
		default:
			goto done
		}
	}
done:

	expected := map[string]string{
		"https://example.com/page1": "Click me",
		"https://example.com/page2": "Titled link",
		"https://example.com/page3": "Spaced   text",
		"https://example.com/page4": "Nested content",
		"https://example.com/page5": "",
		"https://example.com/page6": "",
	}
	for u, want := range expected {
		if names[u] != want {
			t.Errorf("link %s: LinkName = %q; want %q", u, names[u], want)
		}
	}
}

func TestExtractLinksSelector(t *testing.T) {
	htmlContent := `<html><body>
		<div id="main" class="content">
			<nav><ul><li class="item active"><a href="/broken">Link</a></li></ul></nav>
			<img src="/img.png" alt="Banner">
			<script src="/app.js"></script>
		</div>
	</body></html>`

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(htmlContent)),
	}

	NewHTMLPage(c, resp.Body, "https://example.com/").ExtractLinks()

	selectors := map[string]string{}
	for {
		select {
		case l := <-c.linkCh:
			selectors[l.URL] = l.Selector
		default:
			goto done
		}
	}
done:

	expected := map[string]string{
		"https://example.com/broken":  "div#main.content > nav > ul > li.item.active > a",
		"https://example.com/img.png": "div#main.content > img",
		"https://example.com/app.js":  "div#main.content > script",
	}
	for u, want := range expected {
		if selectors[u] != want {
			t.Errorf("link %s: Selector = %q; want %q", u, selectors[u], want)
		}
	}
}

func TestExtractLinksSelectorBoundsLengthAndClasses(t *testing.T) {
	htmlContent := `<html><body><div id="wrap">
		<div class="one two three four five six seven eight"><ul class="u v w x"><li class="a b c d"><a href="/x">deep</a></li></ul></div>
	</div></body></html>`

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(htmlContent)),
	}

	NewHTMLPage(c, resp.Body, "https://example.com/").ExtractLinks()

	var got string
	for {
		select {
		case l := <-c.linkCh:
			got = l.Selector
		default:
			goto done
		}
	}
done:

	// Class lists are trimmed to maxSelectorClasses, but the path stays under
	// maxSelectorAncestors here so no depth cap is applied yet.
	want := "div#wrap > div.one.two.three > ul.u.v.w > li.a.b.c > a"
	if got != want {
		t.Errorf("Selector = %q; want %q", got, want)
	}
}

func TestExtractLinksSelectorFullDepth(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<html><body>`)
	const depth = 10
	for i := 0; i < depth; i++ {
		b.WriteString(`<div class="a"><section class="b"><ul class="c"><li class="d">`)
	}
	b.WriteString(`<a href="/deep">deep</a>`)
	for i := 0; i < depth; i++ {
		b.WriteString(`</li></ul></section></div>`)
	}
	b.WriteString(`</body></html>`)

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(b.String())),
	}

	NewHTMLPage(c, resp.Body, "https://example.com/").ExtractLinks()

	var got string
	for {
		select {
		case l := <-c.linkCh:
			got = l.Selector
		default:
			goto done
		}
	}
done:

	// The full path after <body> is preserved without truncation or filtering.
	if n := strings.Count(got, " > "); n != depth*4 {
		t.Errorf("Selector has %d ancestor links; want %d: %q", n, depth*4, got)
	}
	if strings.HasPrefix(got, "html") || strings.HasPrefix(got, "head") || strings.HasPrefix(got, "body") {
		t.Errorf("Selector should start after <body>, got prefix from %q", got)
	}
}

func TestExtractLinkWithSource(t *testing.T) {
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	token := html.Token{
		Type: html.StartTagToken,
		Data: "source",
		Attr: []html.Attribute{
			{Key: "src", Val: "/video.mp4"},
			{Key: "title", Val: "My Video"},
		},
	}
	base, _ := url.Parse("https://example.com/")

	l := (&HTMLPage{baseURL: base, c: c}).extractLink(token, nil)
	if l == nil {
		t.Fatal("extractLink returned nil for source tag")
	}
	if l.URL != "https://example.com/video.mp4" {
		t.Errorf("URL = %q; want %q", l.URL, "https://example.com/video.mp4")
	}
	if l.Type != LinkTypeVideo {
		t.Errorf("Type = %v; want LinkTypeVideo", l.Type)
	}
	if l.LinkName != "My Video" {
		t.Errorf("LinkName = %q; want %q", l.LinkName, "My Video")
	}
}

func TestExtractLinkMissingAttr(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	token := html.Token{
		Type: html.StartTagToken,
		Data: "a",
		Attr: []html.Attribute{},
	}
	if l := (&HTMLPage{baseURL: base, c: c}).extractLink(token, nil); l != nil {
		t.Error("expected nil for anchor without href")
	}

	token = html.Token{
		Type: html.StartTagToken,
		Data: "img",
		Attr: []html.Attribute{},
	}
	if l := (&HTMLPage{baseURL: base, c: c}).extractLink(token, nil); l != nil {
		t.Error("expected nil for img without src")
	}
}

func TestNewHTMLPageInvalidSourceURL(t *testing.T) {
	p := NewHTMLPage(nil, strings.NewReader(`<a href="/x">x</a>`), "://invalid")
	if p.baseURL != nil {
		t.Error("expected nil baseURL for an invalid source URL")
	}
	// With no base URL, link extraction must be a safe no-op.
	p.ExtractLinks()
}

func TestElemSelectorString(t *testing.T) {
	if got := (elemSelector{}).String(); got != "" {
		t.Errorf("empty selector String() = %q; want empty", got)
	}
	if got := (elemSelector{tag: "a", id: "#main", class: ".btn.active"}).String(); got != "a#main.btn.active" {
		t.Errorf("full selector String() = %q; want %q", got, "a#main.btn.active")
	}
	if got := (elemSelector{tag: "li", class: ".item"}).String(); got != "li.item" {
		t.Errorf("selector without id String() = %q; want %q", got, "li.item")
	}
}

func TestExtractLinksSelfClosingAndVoidElements(t *testing.T) {
	htmlContent := `<html><body>
		<img src="/s.png" />
		<a href="/l">Link</a>
		<br />
		<input type="hidden">
		<meta charset="utf-8">
	</body></html>`

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	NewHTMLPage(c, strings.NewReader(htmlContent), "https://example.com/").ExtractLinks()

	for _, u := range []string{"https://example.com/s.png", "https://example.com/l"} {
		if _, ok := c.visited.Load(u); !ok {
			t.Errorf("expected %q to be visited", u)
		}
	}

	// br, input and meta are not link elements and must not be enqueued.
	count := 0
	c.visited.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 2 {
		t.Errorf("expected 2 visited URLs, got %d", count)
	}
}

func TestExtractLinksNestedAnchorSkipped(t *testing.T) {
	// Nested <a> inside an outer <a> is invalid HTML; the inner anchor must be
	// skipped so it does not clobber the pending outer anchor's link text.
	htmlContent := `<html><body><a href="/outer">Outer <a href="/inner">inner</a> text</a></body></html>`

	cfg, err := NewConfigWithOptions(&Config{
		RootURL:    "https://example.com",
		OutputFile: t.TempDir() + "/test.csv",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := mustNewCrawler(t, cfg)
	defer c.Close()

	NewHTMLPage(c, strings.NewReader(htmlContent), "https://example.com/").ExtractLinks()

	if _, ok := c.visited.Load("https://example.com/inner"); ok {
		t.Error("nested anchor should not be enqueued")
	}
	if _, ok := c.visited.Load("https://example.com/outer"); !ok {
		t.Error("outer anchor should be enqueued")
	}
}

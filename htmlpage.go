package main

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// HTMLPage stores the body of a downloaded HTML page along with the context
// needed to tokenise it and turn the discovered references into absolute URLs.
type HTMLPage struct {
	c       *Crawler  // c provides link enqueueing and crawl scope.
	body    io.Reader // body is the HTML source to tokenise.
	baseURL *url.URL  // baseURL resolves relative URLs found in the page.
}

// NewHTMLPage builds an HTMLPage from the given body and source URL. The base
// URL is derived from sourceURL; links are only extractable when it parses.
func NewHTMLPage(c *Crawler, body io.Reader, sourceURL string) *HTMLPage {
	baseURL, err := url.Parse(sourceURL)
	if err != nil {
		baseURL = nil
	}

	return &HTMLPage{c: c, body: body, baseURL: baseURL}
}

// elemSelector is the CSS-like fragment of a single element used to build a
// context path: its tag name plus any id and class attributes.
type elemSelector struct {
	tag   string // tag is the element's tag name.
	id    string // id is the element's id attribute.
	class string // class is the element's class attribute(s), dotted-prefixed.
}

// String renders the selector fragment, e.g. "a", "li#item", "div.card.active".
func (e elemSelector) String() string {
	if e.tag == "" {
		return ""
	}
	return e.tag + e.id + e.class
}

// maxSelectorClasses bounds how many classes an element fragment keeps.
const maxSelectorClasses = 3

// selectorFromToken builds the selector fragment for an HTML token.
func selectorFromToken(token html.Token) elemSelector {
	es := elemSelector{tag: token.Data}
	for _, attr := range token.Attr {
		switch attr.Key {
		case "id":
			if id := strings.TrimSpace(attr.Val); id != "" {
				es.id = "#" + id
			}
		case "class":
			if classes := strings.Fields(attr.Val); len(classes) > 0 {
				if len(classes) > maxSelectorClasses {
					classes = classes[:maxSelectorClasses]
				}
				es.class = "." + strings.Join(classes, ".")
			}
		}
	}
	return es
}

// buildSelector renders the element's complete CSS-like context path, from the
// first element after the <body> tag down to the element itself. Everything
// before <body> (html, head and head content) is outside the body context and
// omitted. The path is not filtered or truncated, e.g.
// "main#contents > div > section.block-article-link > p > a.text-link".
func buildSelector(ancestors []elemSelector, self elemSelector) string {
	// Start the path right after <body>; elements pushed before it (html, head
	// and their descendants) are not part of the in-body context.
	for i, es := range ancestors {
		if es.tag == "body" {
			ancestors = ancestors[i+1:]
			break
		}
	}

	var b strings.Builder
	for _, es := range ancestors {
		b.WriteString(es.String())
		b.WriteString(" > ")
	}
	b.WriteString(self.String())
	return b.String()
}

// voidElements are HTML elements that never have a closing tag, so they must
// not be pushed onto the selector ancestor stack.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// ExtractLinks tokenises the HTML body and enqueues every discovered URL for processing.
func (p *HTMLPage) ExtractLinks() {
	if p.baseURL == nil {
		return
	}

	tokenizer := html.NewTokenizer(p.body)

	var pending *Link
	var pendingText strings.Builder
	var stack []elemSelector
	var skipDepth int

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}

		switch tokenType {
		case html.StartTagToken:
			token := tokenizer.Token()

			// script/style bodies are raw text, not visible link text; track a
			// nesting depth so their content never leaks into anchor names.
			if token.Data == "script" || token.Data == "style" {
				skipDepth++
			}

			if pending != nil && pending.Type == LinkTypeHyperlink && token.Data == "a" {
				continue
			}

			l := p.extractLink(token, stack)
			if !voidElements[token.Data] {
				stack = append(stack, selectorFromToken(token))
			}
			if l == nil {
				continue
			}

			if l.Type == LinkTypeHyperlink && l.LinkName == "" {
				pending = l
				pendingText.Reset()
				continue
			}

			p.c.enqueueLink(*l)

		case html.SelfClosingTagToken:
			token := tokenizer.Token()
			l := p.extractLink(token, stack)
			if l == nil {
				continue
			}
			p.c.enqueueLink(*l)

		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			if pending != nil {
				pendingText.WriteString(tokenizer.Token().Data)
			}

		case html.EndTagToken:
			token := tokenizer.Token()
			if (token.Data == "script" || token.Data == "style") && skipDepth > 0 {
				skipDepth--
			}
			if pending != nil && token.Data == "a" {
				if name := strings.TrimSpace(pendingText.String()); name != "" {
					pending.LinkName = name
				}
				p.c.enqueueLink(*pending)
				pending = nil
			}

			if n := len(stack); n > 0 && stack[n-1].tag == token.Data {
				stack = stack[:n-1]
			}
		}
	}
}

func (p *HTMLPage) extractLink(token html.Token, ancestors []elemSelector) *Link {
	info, ok := tagMap[token.Data]
	if !ok {
		return nil
	}

	rawURL := getAttr(token, info.urlAttr)
	if rawURL == "" {
		return nil
	}

	resolvedURL := p.resolveURL(rawURL)
	if resolvedURL == "" {
		return nil
	}

	name := getAttr(token, info.nameAttr)

	return &Link{
		URL:        resolvedURL,
		Type:       info.linkType,
		SourcePage: p.baseURL.String(),
		LinkName:   name,
		Selector:   buildSelector(ancestors, selectorFromToken(token)),
	}
}

// resolveURL converts a possibly-relative URL to absolute, stripping fragments and
// discarding non-http protocols (mailto, javascript, tel).
func (p *HTMLPage) resolveURL(raw string) string {
	if raw == "" || raw[0] == '#' {
		return ""
	}

	parsedRaw, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	// Only http/https (and relative URLs, which have an empty scheme) are
	// allowed; other schemes such as mailto, javascript, tel, ftp, data are
	// rejected.
	if parsedRaw.Scheme != "" && parsedRaw.Scheme != "http" && parsedRaw.Scheme != "https" {
		return ""
	}

	// Remove old JavaScript functions
	if strings.ContainsAny(parsedRaw.Path, "()") {
		return ""
	}

	resolved := p.baseURL.ResolveReference(parsedRaw)
	resolved.Fragment = ""

	return resolved.String()
}

// getAttr returns the value of a named attribute from an HTML token, or "" if absent.
func getAttr(token html.Token, attrName string) string {
	for _, attr := range token.Attr {
		if attr.Key == attrName {
			return attr.Val
		}
	}

	return ""
}

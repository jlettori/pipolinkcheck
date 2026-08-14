package main

import "net/url"

// reportError sends a broken-link record to the resultCh channel.
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

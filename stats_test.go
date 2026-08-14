package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestStatsPrintEmpty(t *testing.T) {
	st := NewStats()
	var buf bytes.Buffer
	st.Print(&buf)
	out := buf.String()

	for _, want := range []string{"CRAWL STATISTICS", "0 broken link found.", "Total visited:", "Total links:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestStatsPrintWithErrors(t *testing.T) {
	st := NewStats()
	st.RecordMime("text/html")
	st.RecordMime("text/html")
	st.RecordLink(LinkTypeHyperlink)
	st.RecordLink(LinkTypeImage)
	st.RecordError(LinkTypeHyperlink, ErrorCode(http.StatusNotFound))
	st.RecordError(LinkTypeImage, ErrorCode(errCodeRequestFailed))

	var buf bytes.Buffer
	st.Print(&buf)
	out := buf.String()

	for _, want := range []string{
		"CRAWL STATISTICS",
		"text/html",
		"hyperlink",
		"image",
		"404",
		"Not Found",
		"Total errors:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "0 broken link found.") {
		t.Errorf("output claims zero broken links despite recorded errors:\n%s", out)
	}
}

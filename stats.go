package main

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
)

const statsRule = "============================================================"

// linkTypes is the complete set of LinkType values in declaration order. The
// stats output iterates it wherever the whole enum must be covered, so adding a
// new link type only requires extending this list.
var linkTypes = []LinkType{LinkTypeHyperlink, LinkTypeImage, LinkTypeCSS, LinkTypeScript, LinkTypeVideo}

// Stats holds cumulative counters for the entire crawl.
type Stats struct {
	mu              sync.Mutex          // guards all the counters below against concurrent access.
	resourcesByMime map[string]int64    // visited resources per MIME type.
	linksByType     map[LinkType]int64  // enqueued links per link type.
	linksEnqueued   int64               // total number of links enqueued.
	errorsByType    map[LinkType]int64  // broken links per link type.
	errorsByStatus  map[ErrorCode]int64 // broken links per HTTP status code.
	totalErrors     int64               // total number of broken links found.
}

// NewStats creates and returns a new Stats instance.
func NewStats() *Stats {
	return &Stats{
		resourcesByMime: make(map[string]int64),
		linksByType:     make(map[LinkType]int64),
		errorsByType:    make(map[LinkType]int64),
		errorsByStatus:  make(map[ErrorCode]int64),
	}
}

// RecordMime increments the counter for the given MIME type (a visited resource).
func (st *Stats) RecordMime(mime string) {
	if mime == "" {
		mime = "unknown"
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.resourcesByMime[mime]++
}

// RecordLink increments the count of enqueued links of the given type.
func (st *Stats) RecordLink(linkType LinkType) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.linksEnqueued++
	st.linksByType[linkType]++
}

// RecordError increments the total error count, and tracks by link type and status code.
func (st *Stats) RecordError(linkType LinkType, statusCode ErrorCode) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.totalErrors++
	st.errorsByType[linkType]++
	st.errorsByStatus[statusCode]++
}

// Print writes a summary of the crawl to the given writer.
func (st *Stats) Print(w io.Writer) {
	st.mu.Lock()
	resources := st.resourcesByMime
	links := st.linksByType
	errorsByType := st.errorsByType
	errorsByStatus := st.errorsByStatus
	totalErrors := st.totalErrors
	st.mu.Unlock()

	fmt.Fprintln(w)
	fmt.Fprintln(w, statsRule)
	fmt.Fprintln(w, "CRAWL STATISTICS")
	fmt.Fprintln(w, statsRule)

	printResourcesByMime(w, resources)
	printLinksByType(w, links)
	printErrors(w, errorsByType, errorsByStatus, totalErrors)

	fmt.Fprintln(w, statsRule)
}

// printResourcesByMime prints the number of visited resources per MIME type.
func printResourcesByMime(w io.Writer, resources map[string]int64) {
	fmt.Fprintln(w, "\nVisited resources by MIME type")
	total, width := printSortedCounts(w, resources)
	fmt.Fprintf(w, "  %-*s %8d\n", labelWidth(width, "Total visited:"), "Total visited:", total)
}

// printLinksByType prints the number of enqueued links per link type.
func printLinksByType(w io.Writer, links map[LinkType]int64) {
	fmt.Fprintln(w, "\nLinks enqueued by type")
	printLinkTypeCounts(w, links, "Total links:")
}

// printLinkTypeCounts prints each non-zero count in counts as a row aligned to
// the widest label, followed by the totalLabel row. It returns the total count
// and the column width used.
func printLinkTypeCounts(w io.Writer, counts map[LinkType]int64, totalLabel string) (int64, int) {
	width := 0
	for _, lt := range linkTypes {
		if counts[lt] != 0 {
			width = labelWidth(width, lt.String())
		}
	}
	width = labelWidth(width, totalLabel)

	total := int64(0)
	for _, lt := range linkTypes {
		if n := counts[lt]; n != 0 {
			total += n
			fmt.Fprintf(w, "  %-*s %8d\n", width, lt.String(), n)
		}
	}
	fmt.Fprintf(w, "  %-*s %8d\n", width, totalLabel, total)

	return total, width
}

// printErrors prints the breakdown of broken links by type and by status code.
func printErrors(w io.Writer, errorsByType map[LinkType]int64, errorsByStatus map[ErrorCode]int64, totalErrors int64) {
	if totalErrors == 0 {
		fmt.Fprintln(w, "\n0 broken link found.")
		return
	}

	fmt.Fprintln(w, "\nErrors by link type")
	_, width := printLinkTypeCounts(w, errorsByType, "Total errors:")

	fmt.Fprintln(w, "\nErrors by status code")
	printErrorsByStatus(w, errorsByStatus)

	fmt.Fprintf(w, "\n  %-*s %8d\n", width, "Total errors:", totalErrors)
}

// printErrorsByStatus prints the number of broken links per HTTP status code,
// sorted by status code, with a human-readable label for each known code.
func printErrorsByStatus(w io.Writer, errorsByStatus map[ErrorCode]int64) {
	keys := make([]ErrorCode, 0, len(errorsByStatus))
	for k := range errorsByStatus {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	width := 0
	for _, code := range keys {
		if errorsByStatus[code] == 0 {
			continue
		}
		if label := code.String(); !strings.HasPrefix(label, "ErrorCode(") {
			width = labelWidth(width, label)
		}
	}

	for _, code := range keys {
		n := errorsByStatus[code]
		if n == 0 {
			continue
		}
		label := ""
		if l := code.String(); !strings.HasPrefix(l, "ErrorCode(") {
			label = l
		}
		fmt.Fprintf(w, "  %-6d %-*s %8d\n", code, width, label, n)
	}
}

// printSortedCounts prints each non-zero counter of m sorted by its typed key K
// and returns the total count and the width of the widest key. Callers pass
// K = string for MIME types, so keys are compared directly without string
// formatting.
func printSortedCounts[K cmp.Ordered](w io.Writer, m map[K]int64) (int64, int) {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	width := 0
	for _, k := range keys {
		if l := len(fmt.Sprint(k)); l > width {
			width = l
		}
	}

	total := int64(0)
	for _, k := range keys {
		n := m[k]
		if n == 0 {
			continue
		}
		total += n
		fmt.Fprintf(w, "  %-*v %8d\n", width, k, n)
	}
	return total, width
}

// labelWidth returns width widened as needed to accommodate the longest label,
// so that output columns expand to fit their widest entry.
func labelWidth(width int, labels ...string) int {
	for _, l := range labels {
		if len(l) > width {
			width = len(l)
		}
	}
	return width
}

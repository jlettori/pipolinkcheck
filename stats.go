package main

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"sync"
)

const statsRule = "============================================================"

// Stats holds cumulative counters for the entire crawl.
type Stats struct {
	mu              sync.Mutex         // mu guards all the counters below against concurrent access.
	resourcesByMime map[string]int64   // resourcesByMime counts visited resources per MIME type.
	linksByType     map[LinkType]int64 // linksByType counts enqueued links per link type.
	linksEnqueued   int64              // linksEnqueued is the total number of links enqueued.
	errorsByType    map[LinkType]int64 // errorsByType counts broken links per link type.
	errorsByStatus  map[int]int64      // errorsByStatus counts broken links per HTTP status code.
	totalErrors     int64              // totalErrors is the total number of broken links found.
}

// NewStats creates and returns a new Stats instance.
func NewStats() *Stats {
	return &Stats{
		resourcesByMime: make(map[string]int64),
		linksByType:     make(map[LinkType]int64),
		errorsByType:    make(map[LinkType]int64),
		errorsByStatus:  make(map[int]int64),
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
func (st *Stats) RecordError(linkType LinkType, statusCode int) {
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
	total := printSortedCounts(w, resources)
	fmt.Fprintf(w, "  %-34s %8d\n", "Total visited:", total)
}

// printLinksByType prints the number of enqueued links per link type.
func printLinksByType(w io.Writer, links map[LinkType]int64) {
	fmt.Fprintln(w, "\nLinks enqueued by type")
	total := int64(0)
	for lt := LinkType(0); lt <= LinkTypeVideo; lt++ {
		n := links[lt]
		if n == 0 {
			continue
		}
		total += n
		fmt.Fprintf(w, "  %-34s %8d\n", lt.String(), n)
	}
	fmt.Fprintf(w, "  %-34s %8d\n", "Total links:", total)
}

// printErrors prints the breakdown of broken links by type and by status code.
func printErrors(w io.Writer, errorsByType map[LinkType]int64, errorsByStatus map[int]int64, totalErrors int64) {
	if totalErrors == 0 {
		fmt.Fprintln(w, "\n0 broken link found.")
		return
	}

	fmt.Fprintln(w, "\nErrors by link type")
	for lt := LinkType(0); lt <= LinkTypeVideo; lt++ {
		n := errorsByType[lt]
		if n == 0 {
			continue
		}
		fmt.Fprintf(w, "  %-34s %8d\n", lt.String(), n)
	}

	fmt.Fprintln(w, "\nErrors by status code")
	printSortedCounts(w, errorsByStatus)

	fmt.Fprintf(w, "\n  %-34s %8d\n", "Total errors:", totalErrors)
}

// printSortedCounts prints each non-zero counter of m sorted by its typed key K and
// returns the total count. Callers pass K = string for MIME types or K = int for
// HTTP status codes, so keys are compared directly without string formatting.
func printSortedCounts[K cmp.Ordered](w io.Writer, m map[K]int64) int64 {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	total := int64(0)
	for _, k := range keys {
		n := m[k]
		if n == 0 {
			continue
		}
		total += n
		fmt.Fprintf(w, "  %-34v %8d\n", k, n)
	}
	return total
}

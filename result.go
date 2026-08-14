package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BrokenLink holds the data written to the CSV output for each broken resource.
type BrokenLink struct {
	linkType   LinkType  // linkType is the category of the resource pointed to by the broken link.
	statusCode ErrorCode // statusCode is the HTTP response code for the broken link, or 0 if no response was received.
	sourcePage string    // sourcePage is the page the broken link was found on.
	linkName   string    // linkName is the visible link text (only for hyperlinks).
	selector   string    // selector is a CSS-like path identifying the element's location on the page.
	brokenURL  string    // brokenURL is the URL that was found to be broken.
	errorMsg   string    // errorMsg is the error description when statusCode is 0.
}

// resultWriter manages the output CSV file and writes broken link records.
type resultWriter struct {
	file   *os.File      // file is the underlying output CSV file.
	writer *csv.Writer   // writer writes CSV rows to file.
	mu     sync.Mutex    // mu guards writer and err against concurrent access.
	done   chan struct{} // done closes to stop the periodic flush goroutine.
	err    error         // err holds the first write/flush/close error encountered.
}

// newResultWriter opens the CSV file, writes the header, and returns a resultWriter.
func newResultWriter(filename string) (*resultWriter, error) {
	// #nosec G304 -- CLI tool: the output file path comes from the user's own command line.
	file, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSV file: %w", err)
	}

	// Write UTF-8 BOM so Excel recognizes the encoding
	if _, err := file.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to write BOM: %w", err)
	}

	w := csv.NewWriter(file)
	w.Comma = csvSeparator

	err = w.Write([]string{"Link Type", "Source Page", "Link Name", "Selector", "Broken Link", "Status Code", "Error message"})
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}
	w.Flush()

	if err := w.Error(); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to flush CSV header: %w", err)
	}

	rw := &resultWriter{file: file, writer: w, done: make(chan struct{})}
	go rw.periodicFlush()

	return rw, nil
}

// writeBrokenLink formats a BrokenLink and buffers it as a CSV row.
func (rw *resultWriter) writeBrokenLink(bl BrokenLink) {
	status := strconv.Itoa(int(bl.statusCode))

	// Fields sourced from crawled HTML are attacker-controlled; escape
	// spreadsheet-formula cells to prevent CSV formula injection.
	fields := []string{
		bl.linkType.String(),
		sanitizeCSVCell(bl.sourcePage),
		sanitizeCSVCell(bl.linkName),
		sanitizeCSVCell(bl.selector),
		sanitizeCSVCell(bl.brokenURL),
		status,
		sanitizeCSVCell(bl.errorMsg),
	}

	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.err != nil {
		return
	}
	if err := rw.writer.Write(fields); err != nil {
		rw.err = err
	}
}

// sanitizeCSVCell neutralises spreadsheet formula injection by prefixing cells
// that begin with a formula character (= + - @) or line-start whitespace (> \t \r).
// Leading whitespace before a formula character is also caught, since many
// spreadsheet engines strip it before evaluating the rest as a formula.
func sanitizeCSVCell(s string) string {
	if s == "" {
		return ""
	}

	s = strings.TrimLeft(s, " \t\r\n")
	if s == "" {
		return ""
	}

	switch s[0] {
	case '=', '+', '-', '@':
		return "'" + s
	}

	return s
}

// periodicFlush flushes buffered CSV rows to disk at the configured interval.
func (rw *resultWriter) periodicFlush() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rw.flush()
		case <-rw.done:
			return
		}
	}
}

// flush writes any buffered rows to the underlying file.
func (rw *resultWriter) flush() {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.err != nil {
		return
	}
	rw.writer.Flush()
	if err := rw.writer.Error(); err != nil && rw.err == nil {
		rw.err = err
	}
}

// Err returns the first error encountered while writing, flushing, or closing.
func (rw *resultWriter) Err() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.err
}

// Close stops the flush goroutine, flushes remaining data, and closes the underlying file.
// It returns the first storage error encountered, if any.
func (rw *resultWriter) Close() error {
	close(rw.done)
	rw.flush()

	rw.mu.Lock()
	if err := rw.file.Close(); err != nil && rw.err == nil {
		rw.err = err
	}
	err := rw.err
	rw.mu.Unlock()

	return err
}

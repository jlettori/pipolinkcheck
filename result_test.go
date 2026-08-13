package main

import (
	"encoding/csv"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewResultWriterError(t *testing.T) {
	_, err := newResultWriter(t.TempDir() + "/no/such/dir/out.csv")
	if err == nil {
		t.Fatal("expected error from newResultWriter with invalid output path, got nil")
	}
}

func TestPeriodicFlush(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected only the header row, got %d", len(records))
	}

	bl := BrokenLink{
		sourcePage: "https://example.com/page",
		linkType:   LinkTypeHyperlink,
		brokenURL:  "https://example.com/broken",
		statusCode: 404,
	}
	rw.writeBrokenLink(bl)

	time.Sleep(2 * flushInterval)

	records, err = readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 rows (header + data) after periodic flush, got %d", len(records))
	}
	if records[1][4] != "https://example.com/broken" {
		t.Errorf("broken link = %q; want %q", records[1][4], "https://example.com/broken")
	}
}

func TestPeriodicFlushWritesMultipleRows(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	links := []BrokenLink{
		{sourcePage: "https://site1.com", linkType: LinkTypeHyperlink, brokenURL: "https://site1.com/a", statusCode: 404},
		{sourcePage: "https://site2.com", linkType: LinkTypeImage, brokenURL: "https://site2.com/b.png", statusCode: 0, errorMsg: "connection reset"},
	}
	for _, bl := range links {
		rw.writeBrokenLink(bl)
	}

	time.Sleep(2 * flushInterval)

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 rows after periodic flush, got %d", len(records))
	}
	if records[1][4] != "https://site1.com/a" || records[2][4] != "https://site2.com/b.png" {
		t.Errorf("unexpected rows after periodic flush: %v", records[1:])
	}
}

func TestNewResultWriter(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	if rw.file == nil {
		t.Fatal("file should not be nil")
	}
	if rw.writer == nil {
		t.Fatal("writer should not be nil")
	}

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 header row, got %d", len(records))
	}
	want := []string{"Link Type", "Source Page", "Link Name", "Selector", "Broken Link", "Status Code", "Error message"}
	for i, v := range want {
		if records[0][i] != v {
			t.Errorf("header[%d] = %q; want %q", i, records[0][i], v)
		}
	}
}

func TestWriteBrokenLink(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	bl := BrokenLink{
		sourcePage: "https://example.com/page",
		linkType:   LinkTypeHyperlink,
		brokenURL:  "https://example.com/broken",
		statusCode: 404,
	}
	rw.writeBrokenLink(bl)
	rw.Close()

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 rows (header + data), got %d", len(records))
	}
	want := []string{"hyperlink", "https://example.com/page", "", "", "https://example.com/broken", "404", ""}
	for i, v := range want {
		if records[1][i] != v {
			t.Errorf("row[%d] = %q; want %q", i, records[1][i], v)
		}
	}
}

func TestWriteBrokenLinkWithError(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	bl := BrokenLink{
		sourcePage: "https://example.com/page",
		linkType:   LinkTypeImage,
		brokenURL:  "https://example.com/img.png",
		statusCode: 0,
		errorMsg:   "connection refused",
	}
	rw.writeBrokenLink(bl)
	rw.Close()

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 rows (header + data), got %d", len(records))
	}
	want := []string{"image", "https://example.com/page", "", "", "https://example.com/img.png", "0", "connection refused"}
	for i, v := range want {
		if records[1][i] != v {
			t.Errorf("row[%d] = %q; want %q", i, records[1][i], v)
		}
	}
}

func TestResultWriterClose(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	rw.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("file should exist after close")
	}

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 header row, got %d", len(records))
	}
}

func TestWriteBrokenLinkMultiple(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	links := []BrokenLink{
		{sourcePage: "https://site1.com", linkType: LinkTypeHyperlink, brokenURL: "https://site1.com/a", statusCode: 404},
		{sourcePage: "https://site2.com", linkType: LinkTypeImage, brokenURL: "https://site2.com/b.png", statusCode: 0, errorMsg: "connection reset"},
	}
	for _, bl := range links {
		rw.writeBrokenLink(bl)
	}
	rw.Close()

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(records))
	}
	if records[1][5] != "404" {
		t.Errorf("first link status = %q; want %q", records[1][5], "404")
	}
	if records[2][6] != "connection reset" {
		t.Errorf("second link status = %q; want %q", records[2][6], "connection reset")
	}
}

func TestWriteBrokenLinkWithLinkName(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	bl := BrokenLink{
		sourcePage: "https://example.com/page",
		linkType:   LinkTypeHyperlink,
		brokenURL:  "https://example.com/broken",
		linkName:   "Click Here",
		statusCode: 404,
	}
	rw.writeBrokenLink(bl)
	rw.Close()

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(records))
	}
	if records[1][0] != "hyperlink" {
		t.Errorf("LinkType = %q; want %q", records[1][0], "hyperlink")
	}
	if records[1][5] != "404" {
		t.Errorf("Status = %q; want %q", records[1][5], "404")
	}
	if records[1][2] != "Click Here" {
		t.Errorf("LinkName = %q; want %q", records[1][2], "Click Here")
	}
	if records[1][4] != "https://example.com/broken" {
		t.Errorf("BrokenLink = %q; want %q", records[1][4], "https://example.com/broken")
	}
}

func TestWriteBrokenLinkSpecialChars(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	bl := BrokenLink{
		sourcePage: "https://example.com/page,1",
		linkType:   LinkTypeHyperlink,
		brokenURL:  "https://example.com/broken?q=a,b",
		statusCode: 0,
		errorMsg:   `timeout: "connection"`,
	}
	rw.writeBrokenLink(bl)
	rw.Close()

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(records))
	}
	if !strings.Contains(records[1][1], "page") {
		t.Errorf("expected source page with comma to be quoted, got %q", records[1][1])
	}
	if !strings.Contains(records[1][4], "q=a,b") {
		t.Errorf("expected URL with comma to be quoted, got %q", records[1][4])
	}
}

func TestWriteBrokenLinkCSVFormulaInjection(t *testing.T) {
	path := t.TempDir() + "/test_output.csv"
	rw, err := newResultWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	injected := []string{"=cmd|'/C calc'!A0", "+SUM(A1:A9)", "@SUM(A1:A9)", "-2+3", "\t=1+1", " =1+1", "  @SUM(A1)"}
	for _, payload := range injected {
		bl := BrokenLink{
			sourcePage: payload,
			linkName:   payload,
			selector:   payload,
			brokenURL:  payload,
			statusCode: 0,
			errorMsg:   payload,
		}
		rw.writeBrokenLink(bl)
	}
	rw.Close()

	records, err := readCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != len(injected)+1 {
		t.Fatalf("expected %d rows, got %d", len(injected)+1, len(records))
	}
	for i, payload := range injected {
		want := sanitizeCSVCell(payload)
		for col := 1; col <= 6; col++ {
			if col == 5 { // Status Code column is never user-controlled
				continue
			}
			got := records[i+1][col]
			if got != want {
				t.Errorf("row %d col %d = %q; want formula-neutralised %q", i+1, col, got, want)
			}
		}
	}
}

func TestSanitizeCSVCellSafeValuesUntouched(t *testing.T) {
	safe := []string{"", "text", "http://example.com?a=b", "not-stripped", "123"}
	for _, s := range safe {
		if got := sanitizeCSVCell(s); got != s {
			t.Errorf("sanitizeCSVCell(%q) = %q; want unchanged", s, got)
		}
	}
}

func readCSV(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	r := csv.NewReader(strings.NewReader(string(data)))
	r.Comma = csvSeparator
	return r.ReadAll()
}

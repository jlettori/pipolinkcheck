package main

import (
	"fmt"
	"os"
)

// main parses flags, initialises the crawler, and orchestrates the crawl lifecycle.
func main() {
	cfg, err := NewConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n", err)
		printUsage()
		os.Exit(2)
	}

	resultW, err := newResultWriter(cfg.OutputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	crawler := NewCrawler(cfg, resultW)
	defer crawler.Close()

	crawler.Run()
}

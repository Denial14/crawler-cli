package main

import (
	"context"
	"crawler-api/internal/crawler"
	"crawler-api/internal/models"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	urlsFlag := flag.String("urls", "", "input urls")
	depthFlag := flag.Int("depth", 3, "max depth")
	timeoutFlag := flag.Duration("timeout", 30*time.Second, "global timeout")
	reqTimeoutFlag := flag.Duration("request-timeout", 5*time.Second, "per-request timeout")
	outputFlag := flag.String("output", "result.json", "output JSON file")
	logFlag := flag.String("log", "crawler.log", "log file name")
	flag.Parse()

	if *urlsFlag == "" {
		fmt.Fprintf(os.Stderr, "error: urls is empty\n")
		flag.Usage()
		os.Exit(1)
	}

	urls := make([]string, 0)
	for _, u := range strings.Split(*urlsFlag, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		fmt.Fprintf(os.Stderr, "error: no valid URLs\n")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeoutFlag)
	defer cancel()

	logFile, err := os.OpenFile(*logFlag, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open log file %s: %v\n", *logFlag, err)
		os.Exit(1)
	}
	defer logFile.Close()

	logger := log.New(io.MultiWriter(os.Stdout, logFile), "", log.LstdFlags|log.Lshortfile)
	logger.Printf("[INFO] start crawler, urls=%v, depth=%d, timeout=%s, req_timeout=%s", urls, *depthFlag, *timeoutFlag, *reqTimeoutFlag)

	c := crawler.New(logger, *depthFlag, *reqTimeoutFlag)
	roots := c.Run(ctx, urls)
	logger.Printf("[INFO] crawler finished, pages: %d", len(roots))

	orderedRoots := make([]*models.Node, 0, len(roots))
	for _, u := range urls {
		if root, ok := roots[u]; ok {
			orderedRoots = append(orderedRoots, root)
		}
	}

	data, err := json.MarshalIndent(orderedRoots, "", "  ")
	if err != nil {
		logger.Printf("[ERROR] json marshal: %v", err)
		fmt.Fprintf(os.Stderr, "error: json marshal: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputFlag, data, 0644); err != nil {
		logger.Printf("[ERROR] write output: %v", err)
		fmt.Fprintf(os.Stderr, "error: write output: %v\n", err)
		os.Exit(1)
	}

	logger.Printf("[INFO] result saved to %s", *outputFlag)
	fmt.Printf("Crawler finished, pages: %d, saved to: %s\n", len(orderedRoots), *outputFlag)
}

// ABOUTME: Entry point for the OTLP Mock Receiver.
// ABOUTME: Starts gRPC and HTTP servers to receive logs from TAS OTel Collector.

package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"otlp-mock-receiver/allowlist"
	"otlp-mock-receiver/metrics"
	"otlp-mock-receiver/output"
	"otlp-mock-receiver/receiver"
	"otlp-mock-receiver/transform"
)

func main() {
	grpcPort := flag.Int("grpc-port", 4317, "gRPC server port")
	httpPort := flag.Int("http-port", 4318, "HTTP server port")
	verbose := flag.Bool("verbose", false, "Show verbose output including transformed logs")
	sampleRate := flag.Int("sample-rate", 1, "Keep 1 in N logs (1 = keep all, 10 = keep 10%)")
	sampleDebugOnly := flag.Bool("sample-debug-only", true, "Only sample DEBUG logs (INFO+ always kept)")
	allowlistFile := flag.String("allowlist", "", "Path to allowlist file (one app per line)")
	enableMetrics := flag.Bool("metrics", true, "Enable Prometheus metrics endpoint at /metrics")
	outputFile := flag.String("output-file", "", "Path to JSON output file")
	outputFormat := flag.String("output-format", "jsonl", "Output format: jsonl (default) or json")
	outputBufferSize := flag.Int("output-buffer-size", 100, "Number of logs to buffer before flushing")
	outputFlushInterval := flag.Duration("output-flush-interval", 5*time.Second, "Flush interval for buffered logs")
	flag.Parse()

	// Cloud Foundry provides PORT env var - override HTTP port if set
	if portEnv := os.Getenv("PORT"); portEnv != "" {
		if port, err := strconv.Atoi(portEnv); err == nil {
			*httpPort = port
		}
	}

	// Configure sampling
	if *sampleRate > 1 {
		receiver.SetSamplingConfig(&transform.SamplingConfig{
			SampleRate:      *sampleRate,
			SampleDebugOnly: *sampleDebugOnly,
		})
	}

	// Configure allowlist
	var appAllowlist *allowlist.Allowlist
	if *allowlistFile != "" {
		var err error
		appAllowlist, err = allowlist.LoadFromFile(*allowlistFile)
		if err != nil {
			log.Fatalf("Failed to load allowlist: %v", err)
		}
		receiver.SetAllowlist(appAllowlist)
	}

	// Configure metrics
	if *enableMetrics {
		receiver.SetMetrics(metrics.New())
	}

	// Configure JSON output
	var jsonWriter *output.JSONWriter
	if *outputFile != "" {
		format := output.FormatJSONL
		if *outputFormat == "json" {
			format = output.FormatJSON
		}
		var err error
		jsonWriter, err = output.NewJSONWriter(*outputFile, format, *outputBufferSize, *outputFlushInterval, 100*1024*1024)
		if err != nil {
			log.Fatalf("Failed to create JSON writer: %v", err)
		}
		receiver.SetJSONWriter(jsonWriter)
	}

	log.SetFlags(log.Ltime | log.Lmicroseconds)

	log.Println("========================================")
	log.Println("  OTLP Mock Receiver")
	log.Println("  Practice environment for TAS logging")
	log.Println("========================================")
	log.Printf("  gRPC endpoint: localhost:%d", *grpcPort)
	log.Printf("  HTTP endpoint: localhost:%d/v1/logs", *httpPort)
	log.Printf("  Health check:  localhost:%d/health", *httpPort)
	if *enableMetrics {
		log.Printf("  Metrics:       localhost:%d/metrics", *httpPort)
	}
	if *sampleRate > 1 {
		log.Printf("  Sampling:      1-in-%d (debug-only: %v)", *sampleRate, *sampleDebugOnly)
	}
	if appAllowlist != nil {
		log.Printf("  Allowlist:     %s (%d apps)", *allowlistFile, len(appAllowlist.Apps()))
	}
	if jsonWriter != nil {
		log.Printf("  Output:        %s (%s format)", *outputFile, *outputFormat)
	}
	log.Println("========================================")
	log.Println("")

	// Start gRPC server
	grpcServer, err := receiver.StartGRPC(*grpcPort, *verbose)
	if err != nil {
		log.Fatalf("Failed to start gRPC server: %v", err)
	}

	// Start HTTP server
	httpServer, err := receiver.StartHTTP(*httpPort, *verbose)
	if err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}

	// Start allowlist hot-reload watcher
	stopWatcher := make(chan struct{})
	if appAllowlist != nil && *allowlistFile != "" {
		go appAllowlist.WatchFile(*allowlistFile, stopWatcher, nil, nil)
		log.Printf("Watching %s for changes (hot-reload enabled)", *allowlistFile)
	}

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("\nShutting down...")
	close(stopWatcher)
	if jsonWriter != nil {
		jsonWriter.Close()
	}
	grpcServer.GracefulStop()
	httpServer.Close()

	received, transformed, dropped := receiver.GetStats()
	log.Printf("Final stats: received=%d transformed=%d dropped=%d", received, transformed, dropped)
}

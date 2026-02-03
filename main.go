// ABOUTME: Entry point for the OTLP Mock Receiver.
// ABOUTME: Starts gRPC and HTTP servers to receive logs from TAS OTel Collector.

package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"otlp-mock-receiver/receiver"
	"otlp-mock-receiver/transform"
)

func main() {
	grpcPort := flag.Int("grpc-port", 4317, "gRPC server port")
	httpPort := flag.Int("http-port", 4318, "HTTP server port")
	verbose := flag.Bool("verbose", false, "Show verbose output including transformed logs")
	sampleRate := flag.Int("sample-rate", 1, "Keep 1 in N logs (1 = keep all, 10 = keep 10%)")
	sampleDebugOnly := flag.Bool("sample-debug-only", true, "Only sample DEBUG logs (INFO+ always kept)")
	flag.Parse()

	// Configure sampling
	if *sampleRate > 1 {
		receiver.SetSamplingConfig(&transform.SamplingConfig{
			SampleRate:      *sampleRate,
			SampleDebugOnly: *sampleDebugOnly,
		})
	}

	log.SetFlags(log.Ltime | log.Lmicroseconds)

	log.Println("========================================")
	log.Println("  OTLP Mock Receiver")
	log.Println("  Practice environment for TAS logging")
	log.Println("========================================")
	log.Printf("  gRPC endpoint: localhost:%d", *grpcPort)
	log.Printf("  HTTP endpoint: localhost:%d/v1/logs", *httpPort)
	log.Printf("  Health check:  localhost:%d/health", *httpPort)
	if *sampleRate > 1 {
		log.Printf("  Sampling:      1-in-%d (debug-only: %v)", *sampleRate, *sampleDebugOnly)
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

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("\nShutting down...")
	grpcServer.GracefulStop()
	httpServer.Close()

	received, transformed, dropped := receiver.GetStats()
	log.Printf("Final stats: received=%d transformed=%d dropped=%d", received, transformed, dropped)
}

// ABOUTME: OTLP gRPC and HTTP receivers for log ingestion.
// ABOUTME: Implements the OTLP LogsService to receive logs from TAS OTel Collector.

package receiver

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"otlp-mock-receiver/routing"
	"otlp-mock-receiver/transform"
)

// Stats tracks receiver metrics
type Stats struct {
	LogsReceived    atomic.Int64
	LogsTransformed atomic.Int64
	LogsDropped     atomic.Int64
}

var stats Stats
var samplingConfig *transform.SamplingConfig
var router = routing.DefaultRouter()

// SetSamplingConfig configures sampling for the receiver
func SetSamplingConfig(cfg *transform.SamplingConfig) {
	samplingConfig = cfg
}

// LogsService implements the OTLP Logs gRPC service
type LogsService struct {
	collogspb.UnimplementedLogsServiceServer
	verbose bool
}

// Export handles incoming OTLP log export requests
func (s *LogsService) Export(ctx context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
	for _, resourceLogs := range req.GetResourceLogs() {
		resource := resourceLogs.GetResource()

		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			scope := scopeLogs.GetScope()

			for _, logRecord := range scopeLogs.GetLogRecords() {
				stats.LogsReceived.Add(1)
				processLogRecord(resource, scope, logRecord, s.verbose)
			}
		}
	}

	return &collogspb.ExportLogsServiceResponse{}, nil
}

func processLogRecord(resource *resourcepb.Resource, scope *commonpb.InstrumentationScope, lr *logspb.LogRecord, verbose bool) {
	// Check sampling before processing
	if !transform.ShouldSample(lr, samplingConfig) {
		stats.LogsDropped.Add(1)
		if verbose {
			log.Printf("│ [SAMPLED OUT] Log dropped by sampling (severity: %s)", lr.GetSeverityText())
		}
		return
	}

	log.Println("┌─────────────────────────────────────────")
	log.Printf("│ LOG #%d", stats.LogsReceived.Load())
	log.Println("├─────────────────────────────────────────")

	// Print resource attributes (app metadata from TAS)
	if resource != nil && len(resource.GetAttributes()) > 0 {
		log.Println("│ Resource Attributes:")
		for _, attr := range resource.GetAttributes() {
			log.Printf("│   %s = %s", attr.GetKey(), formatValue(attr.GetValue()))
		}
	}

	// Print scope (instrumentation library info)
	if scope != nil && scope.GetName() != "" {
		log.Printf("│ Scope: %s (v%s)", scope.GetName(), scope.GetVersion())
	}

	// Print log details
	log.Println("│")
	log.Printf("│ Severity: %s (%d)", lr.GetSeverityText(), lr.GetSeverityNumber())
	log.Printf("│ Timestamp: %d", lr.GetTimeUnixNano())

	// Print body
	body := lr.GetBody()
	if body != nil {
		bodyStr := formatValue(body)
		if len(bodyStr) > 200 && !verbose {
			bodyStr = bodyStr[:200] + "..."
		}
		log.Printf("│ Body: %s", bodyStr)
	}

	// Print log attributes
	if len(lr.GetAttributes()) > 0 {
		log.Println("│ Attributes:")
		for _, attr := range lr.GetAttributes() {
			log.Printf("│   %s = %s", attr.GetKey(), formatValue(attr.GetValue()))
		}
	}

	// Apply transformations
	log.Println("│")
	log.Println("│ ─── Applying Transforms ───")

	transformed, actions := transform.Apply(lr)
	for _, action := range actions {
		log.Printf("│   ✓ %s", action)
	}

	// Apply routing
	index, ruleName := router.Route(transformed)
	transform.SetAttribute(transformed, "index", index)
	log.Printf("│   ✓ Routed to: %s (rule: %s)", index, ruleName)

	stats.LogsTransformed.Add(1)

	// Show transformed result
	if verbose {
		log.Println("│")
		log.Println("│ ─── After Transform ───")
		if transformed.GetBody() != nil {
			log.Printf("│ Body: %s", formatValue(transformed.GetBody()))
		}
		if len(transformed.GetAttributes()) > 0 {
			log.Println("│ Attributes:")
			for _, attr := range transformed.GetAttributes() {
				log.Printf("│   %s = %s", attr.GetKey(), formatValue(attr.GetValue()))
			}
		}
	}

	log.Println("└─────────────────────────────────────────")
	log.Println("")
}

func formatValue(v *commonpb.AnyValue) string {
	if v == nil {
		return "<nil>"
	}
	switch val := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_IntValue:
		return fmt.Sprintf("%d", val.IntValue)
	case *commonpb.AnyValue_DoubleValue:
		return fmt.Sprintf("%f", val.DoubleValue)
	case *commonpb.AnyValue_BoolValue:
		return fmt.Sprintf("%t", val.BoolValue)
	case *commonpb.AnyValue_BytesValue:
		return fmt.Sprintf("[%d bytes]", len(val.BytesValue))
	case *commonpb.AnyValue_ArrayValue:
		return fmt.Sprintf("[array: %d items]", len(val.ArrayValue.GetValues()))
	case *commonpb.AnyValue_KvlistValue:
		return fmt.Sprintf("[kvlist: %d items]", len(val.KvlistValue.GetValues()))
	default:
		return fmt.Sprintf("%v", v)
	}
}

// StartGRPC starts the gRPC server for OTLP log ingestion
func StartGRPC(port int, verbose bool) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	server := grpc.NewServer()
	collogspb.RegisterLogsServiceServer(server, &LogsService{verbose: verbose})

	go func() {
		log.Printf("gRPC server listening on :%d", port)
		if err := server.Serve(lis); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	return server, nil
}

// StartHTTP starts the HTTP server for OTLP/HTTP log ingestion
func StartHTTP(port int, verbose bool) (*http.Server, error) {
	mux := http.NewServeMux()

	handler := &httpHandler{verbose: verbose}
	mux.HandleFunc("/v1/logs", handler.handleLogs)
	mux.HandleFunc("/health", handleHealth)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		log.Printf("HTTP server listening on :%d", port)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	return server, nil
}

type httpHandler struct {
	verbose bool
}

func (h *httpHandler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse as protobuf
	req := &collogspb.ExportLogsServiceRequest{}
	if err := proto.Unmarshal(body, req); err != nil {
		log.Printf("Failed to unmarshal OTLP request: %v", err)
		http.Error(w, "Failed to parse OTLP", http.StatusBadRequest)
		return
	}

	// Process logs
	for _, resourceLogs := range req.GetResourceLogs() {
		resource := resourceLogs.GetResource()
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			scope := scopeLogs.GetScope()
			for _, logRecord := range scopeLogs.GetLogRecords() {
				stats.LogsReceived.Add(1)
				processLogRecord(resource, scope, logRecord, h.verbose)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK\nLogs received: %d\nLogs transformed: %d\nLogs dropped: %d\n",
		stats.LogsReceived.Load(),
		stats.LogsTransformed.Load(),
		stats.LogsDropped.Load())
}

// GetStats returns current receiver statistics
func GetStats() (received, transformed, dropped int64) {
	return stats.LogsReceived.Load(), stats.LogsTransformed.Load(), stats.LogsDropped.Load()
}

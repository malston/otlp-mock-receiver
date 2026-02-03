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
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/soheilhy/cmux"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"otlp-mock-receiver/allowlist"
	"otlp-mock-receiver/metrics"
	"otlp-mock-receiver/output"
	"otlp-mock-receiver/routing"
	"otlp-mock-receiver/transform"
)

// Stats tracks receiver metrics
type Stats struct {
	LogsReceived    atomic.Int64
	LogsTransformed atomic.Int64
	LogsDropped     atomic.Int64
	LogsFiltered    atomic.Int64
}

var stats Stats
var samplingConfig *transform.SamplingConfig
var router = routing.DefaultRouter()
var appAllowlist *allowlist.Allowlist
var metricsInstance *metrics.Metrics
var jsonWriter *output.JSONWriter

// SetMetrics configures Prometheus metrics for the receiver
func SetMetrics(m *metrics.Metrics) {
	metricsInstance = m
}

// SetJSONWriter configures the JSON file output writer
func SetJSONWriter(w *output.JSONWriter) {
	jsonWriter = w
}

// SetSamplingConfig configures sampling for the receiver
func SetSamplingConfig(cfg *transform.SamplingConfig) {
	samplingConfig = cfg
}

// SetAllowlist configures the app allowlist for filtering
func SetAllowlist(al *allowlist.Allowlist) {
	appAllowlist = al
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
				if metricsInstance != nil {
					metricsInstance.LogsReceived.Inc()
				}
				processLogRecord(resource, scope, logRecord, s.verbose)
			}
		}
	}

	return &collogspb.ExportLogsServiceResponse{}, nil
}

func processLogRecord(resource *resourcepb.Resource, scope *commonpb.InstrumentationScope, lr *logspb.LogRecord, verbose bool) {
	// Record severity metric
	if metricsInstance != nil {
		severity := lr.GetSeverityText()
		if severity == "" {
			severity = "UNSPECIFIED"
		}
		metricsInstance.LogsBySeverity.WithLabelValues(severity).Inc()
	}

	// Check sampling before processing
	if !transform.ShouldSample(lr, samplingConfig) {
		stats.LogsDropped.Add(1)
		if metricsInstance != nil {
			metricsInstance.LogsDropped.WithLabelValues("sampled").Inc()
		}
		if verbose {
			log.Printf("│ [SAMPLED OUT] Log dropped by sampling (severity: %s)", lr.GetSeverityText())
		}
		return
	}

	// Check allowlist before processing
	if appAllowlist != nil && !appAllowlist.IsAllowed(lr) {
		stats.LogsFiltered.Add(1)
		if metricsInstance != nil {
			metricsInstance.LogsDropped.WithLabelValues("filtered").Inc()
		}
		if verbose {
			appName := getAppName(lr)
			log.Printf("│ [FILTERED] %s (not in allowlist)", appName)
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

	var timer *prometheus.Timer
	if metricsInstance != nil {
		timer = metricsInstance.NewTransformTimer()
	}

	transformed, actions := transform.Apply(lr)
	for _, action := range actions {
		log.Printf("│   ✓ %s", action)
		// Track specific transform actions in metrics
		if metricsInstance != nil {
			if strings.HasPrefix(action, "Redacted PCI") {
				metricsInstance.PCIRedactions.Inc()
			} else if strings.HasPrefix(action, "Truncated body") {
				metricsInstance.BodyTruncations.Inc()
			}
		}
	}

	// Apply routing
	index, ruleName := router.Route(transformed)
	transform.SetAttribute(transformed, "index", index)
	log.Printf("│   ✓ Routed to: %s (rule: %s)", index, ruleName)

	if timer != nil {
		timer.ObserveDuration()
	}

	stats.LogsTransformed.Add(1)
	if metricsInstance != nil {
		metricsInstance.LogsTransformed.Inc()
		metricsInstance.LogsByIndex.WithLabelValues(index).Inc()
	}

	// Write to JSON file if configured
	if jsonWriter != nil {
		entry := buildLogEntry(resource, transformed, index, ruleName, actions)
		jsonWriter.Write(entry)
	}

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

// buildLogEntry creates a LogEntry from a transformed log record
func buildLogEntry(resource *resourcepb.Resource, lr *logspb.LogRecord, index, ruleName string, actions []string) *output.LogEntry {
	// Convert timestamp from nanoseconds to ISO8601
	ts := time.Unix(0, int64(lr.GetTimeUnixNano())).UTC().Format(time.RFC3339Nano)

	// Extract attributes
	attrs := make(map[string]string)
	for _, attr := range lr.GetAttributes() {
		attrs[attr.GetKey()] = formatValue(attr.GetValue())
	}

	// Extract resource attributes
	resourceAttrs := make(map[string]string)
	if resource != nil {
		for _, attr := range resource.GetAttributes() {
			resourceAttrs[attr.GetKey()] = formatValue(attr.GetValue())
		}
	}

	// Get body
	body := ""
	if lr.GetBody() != nil {
		body = formatValue(lr.GetBody())
	}

	return &output.LogEntry{
		Timestamp:      ts,
		Severity:       lr.GetSeverityText(),
		SeverityNumber: int32(lr.GetSeverityNumber()),
		Body:           body,
		Attributes:     attrs,
		ResourceAttrs:  resourceAttrs,
		Routing:        output.RoutingInfo{Index: index, Rule: ruleName},
		Transforms:     actions,
	}
}

// getAppName extracts the application name from log attributes
func getAppName(lr *logspb.LogRecord) string {
	for _, attr := range lr.GetAttributes() {
		key := attr.GetKey()
		if key == "cf_app_name" || key == "application_name" {
			return attr.GetValue().GetStringValue()
		}
	}
	return ""
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

// StartMultiplexed starts both gRPC and HTTP servers on the same port using cmux.
// This is useful for Cloud Foundry deployments where only one port is available.
func StartMultiplexed(port int, verbose bool) (*grpc.Server, *http.Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	// Create cmux multiplexer
	m := cmux.New(lis)

	// Match gRPC (HTTP/2 with content-type application/grpc)
	grpcL := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	// Match HTTP (everything else)
	httpL := m.Match(cmux.Any())

	// Create gRPC server
	grpcServer := grpc.NewServer()
	collogspb.RegisterLogsServiceServer(grpcServer, &LogsService{verbose: verbose})

	// Create HTTP server with h2c support for HTTP/2 cleartext
	mux := http.NewServeMux()
	handler := &httpHandler{verbose: verbose}
	mux.HandleFunc("/v1/logs", handler.handleLogs)
	mux.HandleFunc("/health", handleHealth)
	if metricsInstance != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(metricsInstance.Registry(), promhttp.HandlerOpts{}))
	}

	h2s := &http2.Server{}
	httpServer := &http.Server{
		Handler: h2c.NewHandler(mux, h2s),
	}

	// Start servers
	go func() {
		if err := grpcServer.Serve(grpcL); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	go func() {
		if err := httpServer.Serve(httpL); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	go func() {
		log.Printf("Multiplexed gRPC+HTTP server listening on :%d", port)
		if err := m.Serve(); err != nil {
			log.Printf("cmux error: %v", err)
		}
	}()

	return grpcServer, httpServer, nil
}

// StartHTTP starts the HTTP server for OTLP/HTTP log ingestion
func StartHTTP(port int, verbose bool) (*http.Server, error) {
	mux := http.NewServeMux()

	handler := &httpHandler{verbose: verbose}
	mux.HandleFunc("/v1/logs", handler.handleLogs)
	mux.HandleFunc("/health", handleHealth)

	// Add Prometheus metrics endpoint if metrics are configured
	if metricsInstance != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(metricsInstance.Registry(), promhttp.HandlerOpts{}))
	}

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
				if metricsInstance != nil {
					metricsInstance.LogsReceived.Inc()
				}
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

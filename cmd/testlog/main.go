// ABOUTME: Test utility to send sample OTLP logs to the receiver.
// ABOUTME: Useful for testing transformations without a real TAS environment.

package main

import (
	"context"
	"flag"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func main() {
	endpoint := flag.String("endpoint", "localhost:4317", "OTLP gRPC endpoint")
	flag.Parse()

	conn, err := grpc.Dial(*endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := collogspb.NewLogsServiceClient(conn)

	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "application_name", Value: strVal("payment-service")},
						{Key: "organization_name", Value: strVal("acme-prod")},
						{Key: "space_name", Value: strVal("production")},
						{Key: "instance_id", Value: strVal("0")},
						{Key: "diego_cell_ip", Value: strVal("10.0.1.42")},
						{Key: "process_id", Value: strVal("web")},
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{
						Scope: &commonpb.InstrumentationScope{
							Name:    "cf.loggregator",
							Version: "1.0.0",
						},
						LogRecords: []*logspb.LogRecord{
							{
								TimeUnixNano:   uint64(time.Now().UnixNano()),
								SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_INFO,
								SeverityText:   "INFO",
								Body:           strVal("Payment processed for order #12345. Card: 4111-1111-1111-1111"),
								Attributes: []*commonpb.KeyValue{
									{Key: "source_type", Value: strVal("APP/PROC/WEB")},
								},
							},
						},
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Export(ctx, req)
	if err != nil {
		log.Fatalf("Failed to export: %v", err)
	}

	log.Println("Successfully sent test log")
}

func strVal(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{
		Value: &commonpb.AnyValue_StringValue{StringValue: s},
	}
}

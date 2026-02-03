# OTLP Mock Receiver

A hands-on learning tool for understanding OpenTelemetry log ingestion and transformation, designed to simulate Cribl-like processing for TAS logs.

## Quick Start

```bash
# Run with defaults (gRPC:4317, HTTP:4318)
./otlp-mock-receiver

# Run with verbose output
./otlp-mock-receiver -verbose

# Custom ports
./otlp-mock-receiver -grpc-port 4317 -http-port 4318

# Enable log sampling (keep 1 in 10 debug logs)
./otlp-mock-receiver -sample-rate 10 -sample-debug-only

# Filter to only allowed apps (with hot-reload)
./otlp-mock-receiver -allowlist /path/to/apps.txt

# Disable Prometheus metrics endpoint
./otlp-mock-receiver -metrics=false

# Write logs to JSON file
./otlp-mock-receiver -output-file /tmp/logs.jsonl

# Custom buffer size and flush interval
./otlp-mock-receiver -output-file /tmp/logs.jsonl -output-buffer-size 50 -output-flush-interval 10s
```

## Deploy to TAS/Cloud Foundry

### Prerequisites

- CF CLI installed and authenticated
- TAS 2.12+ (HTTP/2 enabled by default in gorouter)
- Go buildpack with Go 1.24+ available

### Step 1: Deploy the App

```bash
# Target your org and space
cf target -o YOUR_ORG -s YOUR_SPACE

# Push the app (no route by default)
cf push
```

### Step 2: Map HTTP/2 Route for gRPC Support

```bash
# Map route with HTTP/2 protocol
cf map-route otlp-mock-receiver apps.YOUR_DOMAIN --hostname otlp-mock-receiver --app-protocol http2
```

### Step 3: Verify Deployment

```bash
# Check app status
cf app otlp-mock-receiver

# Verify health endpoint
curl -sk https://otlp-mock-receiver.apps.YOUR_DOMAIN/health

# Check Prometheus metrics
curl -sk https://otlp-mock-receiver.apps.YOUR_DOMAIN/metrics

# View app logs (should show "Cloud Foundry (multiplexed)" mode)
cf logs otlp-mock-receiver --recent | grep -A10 "OTLP Mock Receiver"
```

Expected startup output:

```
Mode:          Cloud Foundry (multiplexed)
Endpoint:      :8080 (gRPC + HTTP)
Multiplexed gRPC+HTTP server listening on :8080
```

### Step 4: Send a Test Log

Create a test script to send an OTLP log:

```go
// test_log.go - run with: go run test_log.go
package main

import (
    "bytes"
    "crypto/tls"
    "fmt"
    "net/http"
    "time"

    "google.golang.org/protobuf/proto"
    collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
    commonpb "go.opentelemetry.io/proto/otlp/common/v1"
    logspb "go.opentelemetry.io/proto/otlp/logs/v1"
    resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func main() {
    req := &collogspb.ExportLogsServiceRequest{
        ResourceLogs: []*logspb.ResourceLogs{{
            Resource: &resourcepb.Resource{
                Attributes: []*commonpb.KeyValue{
                    {Key: "application_name", Value: &commonpb.AnyValue{
                        Value: &commonpb.AnyValue_StringValue{StringValue: "test-app"}}},
                },
            },
            ScopeLogs: []*logspb.ScopeLogs{{
                LogRecords: []*logspb.LogRecord{{
                    TimeUnixNano:   uint64(time.Now().UnixNano()),
                    SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_INFO,
                    SeverityText:   "INFO",
                    Body: &commonpb.AnyValue{
                        Value: &commonpb.AnyValue_StringValue{StringValue: "Test log message"}},
                }},
            }},
        }},
    }

    data, _ := proto.Marshal(req)
    client := &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }
    resp, err := client.Post(
        "https://otlp-mock-receiver.apps.YOUR_DOMAIN/v1/logs",
        "application/x-protobuf",
        bytes.NewReader(data),
    )
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }
    defer resp.Body.Close()
    fmt.Printf("Response: %s\n", resp.Status)
}
```

### Step 5: Verify Log Was Received

```bash
# Check metrics (should show logs_received_total = 1)
curl -sk https://otlp-mock-receiver.apps.YOUR_DOMAIN/metrics | grep logs_received

# View processed log in app logs
cf logs otlp-mock-receiver --recent | grep -A20 "LOG #"
```

### How It Works on Cloud Foundry

The app detects the `PORT` environment variable and automatically switches to **multiplexed mode**, serving both gRPC and HTTP on a single port using cmux. The HTTP/2 route (`--app-protocol http2`) enables gRPC clients to connect through the gorouter.

When you set `protocol: http2` on the route, Gorouter sends all traffic to that app over HTTP/2 -- even if the client connected via HTTP/1.1. This is critical for gRPC since it requires HTTP/2 end-to-end.

### Alternative: Manifest-Based Route Configuration

Instead of using the CLI, you can configure HTTP/2 routes in your manifest:

```yaml
applications:
  - name: otlp-mock-receiver
    routes:
      - route: otlp-mock-receiver.apps.YOUR_DOMAIN
        protocol: http2
```

### Testing gRPC Connectivity

Use [grpcurl](https://github.com/fullstorydev/grpcurl#installation) to test the OTLP LogsService endpoint. Since reflection is not enabled, you need to provide the proto definition:

```bash
# Create minimal proto file
cat > /tmp/otlp.proto <<'EOF'
syntax = "proto3";
package opentelemetry.proto.collector.logs.v1;
message ExportLogsServiceRequest {}
message ExportLogsServiceResponse {}
service LogsService {
  rpc Export(ExportLogsServiceRequest) returns (ExportLogsServiceResponse);
}
EOF

# Test gRPC connectivity (sends empty request)
grpcurl -insecure -import-path /tmp -proto otlp.proto \
  otlp-mock-receiver.apps.YOUR_DOMAIN:443 \
  opentelemetry.proto.collector.logs.v1.LogsService/Export
```

Expected response: `{}` (empty ExportLogsServiceResponse)

### Requirements for gRPC

Every network hop must use HTTP/2 for gRPC to work:

- Load balancer → Gorouter: HTTP/2
- Gorouter → App container: HTTP/2
- Route mapped with `--app-protocol http2`

**Load Balancer Requirements:**

Your LB must either:

- Pass through HTTP/2 (Layer 4 / TCP mode), or
- Terminate TLS and re-establish HTTP/2 to Gorouter with ALPN negotiation

If your LB terminates TLS and only speaks HTTP/1.1 to Gorouter, gRPC will fail.

### Limitations

| Limitation          | Impact                                                |
| ------------------- | ----------------------------------------------------- |
| Windows Diego Cells | No HTTP/2 egress -- gRPC won't work                   |
| WebSockets          | Don't work over HTTP/2 -- use separate HTTP/1.1 route |
| Route Services      | May break end-to-end HTTP/2 depending on the service  |

### References

- [TAS HTTP/2 Protocol Support](https://techdocs.broadcom.com/us/en/vmware-tanzu/platform/elastic-application-runtime/10-2/eart/supporting-http2.html)
- [Cloud Foundry HTTP/2 Protocol](https://docs.cloudfoundry.org/devguide/http2-protocol.html)

### Configure TAS OTel Collector

To send TAS platform logs to this receiver:

```yaml
# Using gRPC (recommended)
exporters:
  otlp:
    endpoint: "otlp-mock-receiver.apps.YOUR_DOMAIN:443"
    tls:
      insecure: false

# Using HTTP
exporters:
  otlphttp:
    endpoint: "https://otlp-mock-receiver.apps.YOUR_DOMAIN"
```

## Endpoints

| Protocol | Port | Path       |
| -------- | ---- | ---------- |
| gRPC     | 4317 | -          |
| HTTP     | 4318 | `/v1/logs` |
| Health   | 4318 | `/health`  |
| Metrics  | 4318 | `/metrics` |

## Configure TAS to Send Logs Here

In OpsManager, add this OTel config to **TAS Tile → System Logging → OpenTelemetry Collector Configuration**:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
      http:

processors:
  batch:
    timeout: 1s

exporters:
  otlp:
    endpoint: "OTLP_MOCK_RECEIVER_IP_ADDRESS:4317"
    tls:
      insecure: true

service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp]
```

## What It Does

1. **Receives** OTLP logs via gRPC or HTTP
2. **Displays** raw log structure (resource attributes, scope, body, attributes)
3. **Transforms** logs using configurable rules:
   - Field renaming (e.g., `application_name` → `cf_app_name`)
   - Field deletion (high-cardinality fields)
   - PCI redaction (credit card, SSN patterns)
   - Body truncation
4. **Shows** transformation actions taken

## Default Transformations

Configured in `transform/transform.go`:

| Transform | Details                                    |
| --------- | ------------------------------------------ |
| Rename    | `application_name` → `cf_app_name`         |
| Rename    | `organization_name` → `cf_org_name`        |
| Rename    | `space_name` → `cf_space_name`             |
| Rename    | `instance_id` → `cf_instance_id`           |
| Rename    | `app_id` → `cf_app_guid`                   |
| Rename    | `organization_id` → `cf_org_guid`          |
| Rename    | `space_id` → `cf_space_guid`               |
| Rename    | `source_type` → `cf_source_type`           |
| Rename    | `log_type` → `cf_log_type`                 |
| Delete    | `diego_cell_ip`, `process_id`, `source_id` |
| Redact    | Credit card patterns                       |
| Redact    | SSN patterns                               |
| Truncate  | Body > 32KB                                |

## Exercises

See [EXERCISES.md](EXERCISES.md) for detailed acceptance criteria.

1. [Add more field renames for CF/TAS standardization](EXERCISES.md#exercise-1-add-more-field-renames-for-cftas-standardization)
2. [Implement log sampling (keep 1-in-N debug logs)](EXERCISES.md#exercise-2-implement-log-sampling-keep-1-in-n-debug-logs)
3. [Add routing logic to determine Splunk index](EXERCISES.md#exercise-3-add-routing-logic-to-determine-splunk-index)
4. [Implement app allowlist filtering](EXERCISES.md#exercise-4-implement-app-allowlist-filtering)
5. [Add metrics/counters for monitoring](EXERCISES.md#exercise-5-add-metricscounters-for-monitoring)
6. [Write transformed logs to a JSON file](EXERCISES.md#exercise-6-write-transformed-logs-to-a-json-file)

## Project Structure

```text
otlp-mock-receiver/
├── main.go              # Entry point, CLI flags
├── allowlist/
│   └── allowlist.go     # App allowlist with hot-reload
├── metrics/
│   └── metrics.go       # Prometheus metrics
├── output/
│   └── jsonfile.go      # JSON file output with buffering
├── receiver/
│   └── receiver.go      # gRPC + HTTP OTLP servers
├── routing/
│   └── routing.go       # Index routing rules
└── transform/
    └── transform.go     # Transformation logic
```

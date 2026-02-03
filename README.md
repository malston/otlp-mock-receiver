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
```

## Endpoints

| Protocol | Port | Path       |
| -------- | ---- | ---------- |
| gRPC     | 4317 | -          |
| HTTP     | 4318 | `/v1/logs` |
| Health   | 4318 | `/health`  |

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
├── receiver/
│   └── receiver.go      # gRPC + HTTP OTLP servers
├── routing/
│   └── routing.go       # Index routing rules
└── transform/
    └── transform.go     # Transformation logic
```

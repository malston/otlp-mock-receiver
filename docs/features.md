# OTLP Mock Receiver Features

This document describes the features available in the OTLP Mock Receiver and how to use them.

## Table of Contents

- [Field Renames](#field-renames)
- [Log Sampling](#log-sampling)
- [Index Routing](#index-routing)
- [App Allowlist Filtering](#app-allowlist-filtering)
- [Prometheus Metrics](#prometheus-metrics)
- [JSON File Output](#json-file-output)

---

## Field Renames

Automatically renames TAS/Cloud Foundry log attributes to a standardized format for downstream systems like Splunk.

### Default Renames

| Original Field      | Renamed To       |
| ------------------- | ---------------- |
| `application_name`  | `cf_app_name`    |
| `organization_name` | `cf_org_name`    |
| `space_name`        | `cf_space_name`  |
| `app_id`            | `cf_app_guid`    |
| `organization_id`   | `cf_org_guid`    |
| `space_id`          | `cf_space_guid`  |
| `source_type`       | `cf_source_type` |
| `log_type`          | `cf_log_type`    |

### How It Works

Field renames are applied automatically to all incoming logs. The original field is removed and replaced with the renamed field, preserving the value.

### Usage

Field renames are enabled by default with no configuration required:

```bash
./otlp-mock-receiver
```

---

## Log Sampling

Reduces log volume by keeping only a fraction of debug logs while preserving all error and warning logs.

### How It Works

- Sampling uses a deterministic hash of log content for reproducibility
- ERROR and above severity logs are never sampled (always kept)
- When `sample-debug-only` is enabled, only DEBUG severity logs are subject to sampling
- Sampled-out logs increment the `logs_dropped_total{reason="sampled"}` metric

### CLI Flags

| Flag                 | Default | Description                                                                             |
| -------------------- | ------- | --------------------------------------------------------------------------------------- |
| `-sample-rate N`     | `1`     | Keep 1 in N logs. Value of 1 means no sampling (keep all). Value of 10 means keep ~10%. |
| `-sample-debug-only` | `true`  | When true, only apply sampling to DEBUG severity logs.                                  |

### Usage

```bash
# Keep 1 in 10 debug logs (drop ~90%)
./otlp-mock-receiver -sample-rate 10

# Sample all log levels (not just debug)
./otlp-mock-receiver -sample-rate 10 -sample-debug-only=false
```

---

## Index Routing

Automatically routes logs to different Splunk indexes based on configurable rules.

### Default Routing Rules

Rules are evaluated in priority order (first match wins):

| Priority | Condition                       | Target Index   |
| -------- | ------------------------------- | -------------- |
| 1        | Severity >= ERROR               | `tas_errors`   |
| 2        | App name matches `^security-`   | `tas_security` |
| 3        | App name matches `^audit-`      | `tas_audit`    |
| 4        | Space name matches `production` | `tas_prod`     |
| 5        | Default (no match)              | `tas_logs`     |

### How It Works

- The router evaluates each log against the rules in priority order
- The first matching rule determines the target index
- An `index` attribute is added to the log record
- In verbose mode, the matched rule is logged

### Usage

Routing is enabled by default. Use verbose mode to see routing decisions:

```bash
./otlp-mock-receiver -verbose
# Output: "Routed to: tas_errors via rule: error-severity"
```

---

## App Allowlist Filtering

Filter logs to only allow specific applications, useful for reducing noise or focusing on specific services.

### How It Works

- When an allowlist file is provided, only logs from listed apps are processed
- Apps not in the allowlist are dropped and counted in `logs_dropped_total{reason="filtered"}`
- The allowlist file supports comments (lines starting with `#`)
- Matching is case-insensitive
- Hot-reload: changes to the allowlist file are detected and applied without restart

### File Format

```text
# This is a comment
my-app
payment-service
auth-service
```

### CLI Flags

| Flag              | Default | Description                                                    |
| ----------------- | ------- | -------------------------------------------------------------- |
| `-allowlist path` | (none)  | Path to allowlist file. Empty or missing means allow all apps. |

### Usage

```bash
# Create an allowlist file
cat > /tmp/allowlist.txt << EOF
# Production apps only
payment-service
order-service
auth-service
EOF

# Start with allowlist filtering
./otlp-mock-receiver -allowlist /tmp/allowlist.txt -verbose

# Modify the file while running - changes are auto-reloaded
echo "new-app" >> /tmp/allowlist.txt
```

---

## Prometheus Metrics

Exposes operational metrics in Prometheus format for monitoring and alerting.

### Available Metrics

All metrics use the `otlp_receiver_` prefix.

| Metric                       | Type      | Labels     | Description                        |
| ---------------------------- | --------- | ---------- | ---------------------------------- |
| `logs_received_total`        | Counter   | -          | Total logs received                |
| `logs_transformed_total`     | Counter   | -          | Logs after transformation          |
| `logs_dropped_total`         | Counter   | `reason`   | Logs dropped (sampled or filtered) |
| `logs_by_severity_total`     | Counter   | `severity` | Log count by severity level        |
| `logs_by_index_total`        | Counter   | `index`    | Log count by routing destination   |
| `transform_duration_seconds` | Histogram | -          | Time spent transforming logs       |
| `pci_redactions_total`       | Counter   | -          | PCI patterns redacted              |
| `body_truncations_total`     | Counter   | -          | Log bodies truncated               |

### CLI Flags

| Flag       | Default | Description                         |
| ---------- | ------- | ----------------------------------- |
| `-metrics` | `true`  | Enable/disable the metrics endpoint |

### Usage

```bash
# Metrics enabled by default
./otlp-mock-receiver

# Query metrics
curl http://localhost:4318/metrics | grep otlp_receiver

# Disable metrics
./otlp-mock-receiver -metrics=false
```

### Example Output

```text
otlp_receiver_logs_received_total 1542
otlp_receiver_logs_by_severity_total{severity="INFO"} 1200
otlp_receiver_logs_by_severity_total{severity="ERROR"} 342
otlp_receiver_logs_by_index_total{index="tas_logs"} 1100
otlp_receiver_logs_by_index_total{index="tas_errors"} 342
otlp_receiver_logs_dropped_total{reason="sampled"} 450
```

---

## JSON File Output

Writes transformed logs to a JSON file for offline analysis, debugging, or integration with other systems.

### Output Format

Each log is written as a single line of JSON (JSONL format by default):

```json
{
  "timestamp": "2024-01-15T10:30:00.000Z",
  "severity": "INFO",
  "severity_number": 9,
  "body": "Payment processed for order #12345",
  "attributes": {
    "cf_source_type": "APP/PROC/WEB"
  },
  "resource_attributes": {
    "cf_app_name": "payment-service",
    "cf_org_name": "production"
  },
  "routing": {
    "index": "tas_logs",
    "rule": "default"
  },
  "transforms_applied": ["Renamed: application_name -> cf_app_name"]
}
```

### CLI Flags

| Flag                     | Default | Description                                            |
| ------------------------ | ------- | ------------------------------------------------------ |
| `-output-file path`      | (none)  | Path to output file. No file output if not specified.  |
| `-output-format`         | `jsonl` | Output format: `json` or `jsonl` (line-delimited JSON) |
| `-output-buffer-size N`  | `100`   | Number of logs to buffer before writing                |
| `-output-flush-interval` | `5s`    | Maximum time between flushes                           |

### Features

- **Buffered writes**: Logs are buffered for performance and flushed based on buffer size or time interval
- **File rotation**: When the file exceeds 100MB, it's rotated to `filename.1`, `filename.2`, etc.
- **Graceful shutdown**: Buffer is flushed on shutdown to prevent data loss

### Usage

```bash
# Basic file output
./otlp-mock-receiver -output-file /var/log/otlp/logs.jsonl

# Custom buffer settings for high-throughput environments
./otlp-mock-receiver \
  -output-file /var/log/otlp/logs.jsonl \
  -output-buffer-size 500 \
  -output-flush-interval 10s

# Verify output
tail -f /var/log/otlp/logs.jsonl | jq .
```

---

## Combining Features

All features can be used together:

```bash
./otlp-mock-receiver \
  -verbose \
  -sample-rate 10 \
  -sample-debug-only \
  -allowlist /etc/otlp/allowlist.txt \
  -output-file /var/log/otlp/logs.jsonl \
  -output-buffer-size 200
```

This configuration:

1. Renames TAS fields to standardized names
2. Samples debug logs (keeps ~10%)
3. Filters to only allowed apps
4. Routes logs to appropriate Splunk indexes
5. Exposes Prometheus metrics
6. Writes transformed logs to a JSON file

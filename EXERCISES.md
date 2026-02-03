# OTLP Mock Receiver Exercises

Hands-on exercises for learning log transformation patterns. Each exercise builds on the existing codebase.

---

## Exercise 1: Add More Field Renames for CF/TAS Standardization

Extend the field rename configuration to handle additional TAS/CF log attributes that need standardization for downstream systems.

### Acceptance Criteria

- [x] Add renames for at least 5 additional CF/TAS fields to `DefaultConfig()` in `transform/transform.go`
- [x] Required renames to add:
  - `app_id` → `cf_app_guid`
  - `organization_id` → `cf_org_guid`
  - `space_id` → `cf_space_guid`
  - `source_type` → `cf_source_type`
  - `log_type` → `cf_log_type`
- [x] Existing renames must continue to work (no regressions)
- [x] Update README.md "Default Transformations" table with new renames
- [x] Write tests that verify each new rename works correctly
- [x] Tests must cover the case where the original field doesn't exist (should be a no-op)

### Verification

```bash
go test ./transform/... -v -run TestFieldRenames
```

---

## Exercise 2: Implement Log Sampling (Keep 1-in-N Debug Logs)

Add configurable sampling to reduce debug log volume while keeping all error/warning logs.

### Acceptance Criteria

- [ ] Add `SamplingConfig` struct to `transform/transform.go` with fields:
  - `SampleRate int` -- keep 1 in N logs (e.g., 10 = keep 10%)
  - `SampleDebugOnly bool` -- when true, only sample DEBUG severity logs
- [ ] Add `SamplingConfig` field to the main `Config` struct
- [ ] Implement `ShouldSample(lr *logspb.LogRecord, cfg *SamplingConfig) bool` function
- [ ] Sampling logic:
  - If `SampleDebugOnly` is true, only apply sampling to DEBUG logs (severity < INFO)
  - ERROR and above logs are never sampled (always kept)
  - Use deterministic sampling based on log content hash for reproducibility
- [ ] Integrate sampling into `receiver/receiver.go` processing pipeline
- [ ] Increment `stats.LogsDropped` when logs are sampled out
- [ ] Add CLI flag `-sample-rate N` to main.go (default: 1 = no sampling)
- [ ] Add CLI flag `-sample-debug-only` to main.go (default: true)
- [ ] Write tests covering:
  - Sample rate of 1 keeps all logs
  - Sample rate of 10 drops approximately 90% of eligible logs
  - ERROR logs are never dropped regardless of sample rate
  - DEBUG logs are dropped when `SampleDebugOnly=true`

### Verification

```bash
go test ./transform/... -v -run TestSampling
./otlp-mock-receiver -sample-rate 10 -sample-debug-only
```

---

## Exercise 3: Add Routing Logic to Determine Splunk Index

Extend the existing `DetermineIndex` function to support configurable routing rules.

### Acceptance Criteria

- [ ] Create `routing/routing.go` with routing logic (move from transform.go)
- [ ] Define `RoutingRule` struct:

  ```go
  type RoutingRule struct {
      Name       string            // Rule name for logging
      Conditions map[string]string // Attribute name → regex pattern
      Index      string            // Target Splunk index
      Priority   int               // Lower = higher priority
  }
  ```

- [ ] Implement `Router` struct that holds ordered rules
- [ ] Implement `Router.Route(lr *logspb.LogRecord) string` method
- [ ] Default routing rules (in priority order):
  1. Severity >= ERROR → `tas_errors`
  2. App name matches `^security-` → `tas_security`
  3. App name matches `^audit-` → `tas_audit`
  4. Space name matches `production` → `tas_prod`
  5. Default → `tas_logs`
- [ ] Add `index` attribute to transformed log records
- [ ] Log which routing rule matched in verbose mode
- [ ] Write tests for each routing rule
- [ ] Write test for rule priority (first match wins)
- [ ] Write test for default fallback when no rules match

### Verification

```bash
go test ./routing/... -v
./otlp-mock-receiver -verbose  # Should show "Routed to: tas_xxx via rule: yyy"
```

---

## Exercise 4: Implement App Allowlist Filtering

Make the existing `ShouldAllow` function configurable via CLI and add metrics for filtered apps.

### Acceptance Criteria

- [ ] Add CLI flag `-allowlist path/to/apps.txt` to main.go
- [ ] Allowlist file format: one app name per line, lines starting with `#` are comments
- [ ] Empty allowlist file or no `-allowlist` flag means allow all apps
- [ ] Implement hot-reload: watch allowlist file for changes and reload without restart
- [ ] Add `LogsFiltered` counter to `Stats` struct (separate from `LogsDropped` which is for sampling)
- [ ] Log filtered app names in verbose mode: "Filtered: app_name (not in allowlist)"
- [ ] Write tests for:
  - Allowlist with specific apps only allows those apps
  - Empty allowlist allows all apps
  - Case-insensitive matching works
  - Comments in allowlist file are ignored
  - Hot-reload updates the allowlist

### Verification

```bash
# Create allowlist
echo -e "my-app\nother-app" > /tmp/allowlist.txt
./otlp-mock-receiver -allowlist /tmp/allowlist.txt -verbose
# In another terminal, modify allowlist and verify reload
```

---

## Exercise 5: Add Metrics/Counters for Monitoring

Expose Prometheus-compatible metrics for operational monitoring.

### Acceptance Criteria

- [ ] Create `metrics/metrics.go` with Prometheus client integration
- [ ] Add dependency: `go get github.com/prometheus/client_golang/prometheus`
- [ ] Expose metrics endpoint at `/metrics` on the HTTP port
- [ ] Required metrics (all with `otlp_receiver_` prefix):
  - `logs_received_total` (counter) -- total logs received
  - `logs_transformed_total` (counter) -- logs after transformation
  - `logs_dropped_total` (counter, labels: `reason=[sampled|filtered]`)
  - `logs_by_severity_total` (counter, label: `severity`)
  - `logs_by_index_total` (counter, label: `index`)
  - `transform_duration_seconds` (histogram) -- time spent transforming
  - `pci_redactions_total` (counter) -- PCI patterns redacted
  - `body_truncations_total` (counter) -- bodies truncated
- [ ] Replace atomic counters in `receiver.go` with Prometheus counters
- [ ] Write tests verifying metrics are incremented correctly
- [ ] Add `-metrics` CLI flag to enable/disable metrics (default: enabled)

### Verification

```bash
./otlp-mock-receiver
curl http://localhost:4318/metrics | grep otlp_receiver
```

Expected output includes:

```text
otlp_receiver_logs_received_total 42
otlp_receiver_logs_by_severity_total{severity="INFO"} 30
otlp_receiver_logs_by_severity_total{severity="ERROR"} 12
```

---

## Exercise 6: Write Transformed Logs to a JSON File

Add file output for transformed logs in JSON format for offline analysis.

### Acceptance Criteria

- [ ] Create `output/jsonfile.go` with file writer implementation
- [ ] Add CLI flag `-output-file path/to/logs.json` to main.go
- [ ] Add CLI flag `-output-format json|jsonl` (default: jsonl for line-delimited JSON)
- [ ] JSON schema for each log entry:

  ```json
  {
    "timestamp": "2024-01-15T10:30:00.000Z",
    "severity": "INFO",
    "severity_number": 9,
    "body": "log message here",
    "attributes": { "key": "value" },
    "resource_attributes": { "app_name": "my-app" },
    "routing": { "index": "tas_logs", "rule": "default" },
    "transforms_applied": ["Renamed: application_name -> cf_app_name"]
  }
  ```

- [ ] Implement buffered writes with configurable flush interval
- [ ] Add CLI flag `-output-buffer-size N` (default: 100 logs)
- [ ] Add CLI flag `-output-flush-interval 5s` (default: 5 seconds)
- [ ] Handle file rotation: when file exceeds 100MB, rotate to `logs.json.1`, etc.
- [ ] Graceful shutdown: flush buffer before exit
- [ ] Write tests for:
  - JSON output is valid and parseable
  - Buffer flushes at configured size
  - Buffer flushes at configured interval
  - File rotation works at size threshold
  - Graceful shutdown flushes remaining buffer

### Verification

```bash
./otlp-mock-receiver -output-file /tmp/logs.jsonl -output-format jsonl
# Send some logs, then:
cat /tmp/logs.jsonl | jq .  # Should be valid JSON per line
```

---

## General Requirements for All Exercises

1. **Testing**: Every exercise must include unit tests with >80% coverage for new code
2. **Documentation**: Update README.md to document new features and CLI flags
3. **Backwards Compatibility**: Existing behavior must not change unless explicitly modified
4. **Error Handling**: All errors must be handled gracefully with meaningful messages
5. **Code Style**: Follow existing code patterns and the Google Go Style Guide

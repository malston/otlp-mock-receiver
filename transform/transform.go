// ABOUTME: Log transformation logic simulating Cribl-like processing.
// ABOUTME: Provides field renaming, PCI redaction, truncation, and filtering.

package transform

import (
	"regexp"
	"strings"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// Config holds transformation configuration
type Config struct {
	// Field renames: old name -> new name
	FieldRenames map[string]string

	// Fields to delete
	FieldsToDelete []string

	// Max body length (0 = no limit)
	MaxBodyLength int

	// PCI patterns to redact
	PCIPatterns []*regexp.Regexp

	// App allowlist (empty = allow all)
	AllowedApps []string
}

// DefaultConfig returns the default transformation config for CF/TAS field standardization
func DefaultConfig() *Config {
	return &Config{
		FieldRenames: map[string]string{
			"application_name":  "cf_app_name",
			"organization_name": "cf_org_name",
			"space_name":        "cf_space_name",
			"instance_id":       "cf_instance_id",
			"app_id":            "cf_app_guid",
			"organization_id":   "cf_org_guid",
			"space_id":          "cf_space_guid",
			"source_type":       "cf_source_type",
			"log_type":          "cf_log_type",
		},
		FieldsToDelete: []string{
			"diego_cell_ip",
			"process_id",
			"source_id",
		},
		MaxBodyLength: 32768, // 32KB
		PCIPatterns: []*regexp.Regexp{
			// Credit card patterns
			regexp.MustCompile(`\b\d{4}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}\b`),
			// SSN pattern
			regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		},
		AllowedApps: []string{}, // Empty = allow all
	}
}

var defaultConfig = DefaultConfig()

// Apply runs the transformation pipeline on a log record.
// Returns the transformed log and a list of actions taken.
func Apply(lr *logspb.LogRecord) (*logspb.LogRecord, []string) {
	return ApplyWithConfig(lr, defaultConfig)
}

// ApplyWithConfig runs transformations with a custom config
func ApplyWithConfig(lr *logspb.LogRecord, cfg *Config) (*logspb.LogRecord, []string) {
	var actions []string

	// 1. Rename fields
	for oldKey, newKey := range cfg.FieldRenames {
		if renameAttribute(lr, oldKey, newKey) {
			actions = append(actions, "Renamed: "+oldKey+" -> "+newKey)
		}
	}

	// 2. Delete fields
	for _, key := range cfg.FieldsToDelete {
		if deleteAttribute(lr, key) {
			actions = append(actions, "Deleted: "+key)
		}
	}

	// 3. PCI redaction
	for i, pattern := range cfg.PCIPatterns {
		if redactPattern(lr, pattern, "[PCI-REDACTED]") {
			actions = append(actions, "Redacted PCI pattern #"+string(rune('1'+i)))
		}
	}

	// 4. Truncate body
	if cfg.MaxBodyLength > 0 {
		if truncateBody(lr, cfg.MaxBodyLength) {
			actions = append(actions, "Truncated body to max length")
		}
	}

	if len(actions) == 0 {
		actions = append(actions, "No transformations applied")
	}

	return lr, actions
}

// renameAttribute renames an attribute key. Returns true if renamed.
func renameAttribute(lr *logspb.LogRecord, oldKey, newKey string) bool {
	for _, attr := range lr.GetAttributes() {
		if attr.GetKey() == oldKey {
			attr.Key = newKey
			return true
		}
	}
	return false
}

// deleteAttribute removes an attribute by key. Returns true if deleted.
func deleteAttribute(lr *logspb.LogRecord, key string) bool {
	attrs := lr.GetAttributes()
	for i, attr := range attrs {
		if attr.GetKey() == key {
			// Remove by replacing with last element and truncating
			attrs[i] = attrs[len(attrs)-1]
			lr.Attributes = attrs[:len(attrs)-1]
			return true
		}
	}
	return false
}

// redactPattern applies regex redaction to the log body. Returns true if any matches replaced.
func redactPattern(lr *logspb.LogRecord, pattern *regexp.Regexp, replacement string) bool {
	body := lr.GetBody()
	if body == nil {
		return false
	}

	str := body.GetStringValue()
	if str == "" {
		return false
	}

	if !pattern.MatchString(str) {
		return false
	}

	redacted := pattern.ReplaceAllString(str, replacement)
	lr.Body = &commonpb.AnyValue{
		Value: &commonpb.AnyValue_StringValue{StringValue: redacted},
	}
	return true
}

// truncateBody truncates the log body if it exceeds maxLen. Returns true if truncated.
func truncateBody(lr *logspb.LogRecord, maxLen int) bool {
	body := lr.GetBody()
	if body == nil {
		return false
	}

	str := body.GetStringValue()
	if len(str) <= maxLen {
		return false
	}

	truncated := str[:maxLen] + "...[TRUNCATED]"
	lr.Body = &commonpb.AnyValue{
		Value: &commonpb.AnyValue_StringValue{StringValue: truncated},
	}
	return true
}

// ShouldAllow checks if a log should be allowed based on app allowlist.
// Returns true if allowed, false if should be dropped.
func ShouldAllow(lr *logspb.LogRecord, allowedApps []string) bool {
	if len(allowedApps) == 0 {
		return true // No allowlist = allow all
	}

	appName := getAttributeValue(lr, "cf_app_name")
	if appName == "" {
		appName = getAttributeValue(lr, "application_name")
	}

	for _, allowed := range allowedApps {
		if strings.EqualFold(appName, allowed) {
			return true
		}
	}
	return false
}

// getAttributeValue retrieves a string attribute value by key
func getAttributeValue(lr *logspb.LogRecord, key string) string {
	for _, attr := range lr.GetAttributes() {
		if attr.GetKey() == key {
			return attr.GetValue().GetStringValue()
		}
	}
	return ""
}

// SetAttribute sets or updates an attribute value
func SetAttribute(lr *logspb.LogRecord, key, value string) {
	// Check if exists
	for _, attr := range lr.GetAttributes() {
		if attr.GetKey() == key {
			attr.Value = &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: value},
			}
			return
		}
	}

	// Add new attribute
	lr.Attributes = append(lr.Attributes, &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value},
		},
	})
}

// DetermineIndex returns which Splunk index a log should route to.
// This simulates Cribl routing logic.
func DetermineIndex(lr *logspb.LogRecord) string {
	// Check severity
	severity := lr.GetSeverityNumber()
	if severity >= logspb.SeverityNumber_SEVERITY_NUMBER_ERROR {
		return "tas_errors"
	}

	// Check app name for special routing
	appName := getAttributeValue(lr, "cf_app_name")
	if strings.HasPrefix(appName, "security-") {
		return "tas_security"
	}

	// Default index
	return "tas_logs"
}

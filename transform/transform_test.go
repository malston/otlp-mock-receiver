// ABOUTME: Tests for log transformation logic.
// ABOUTME: Covers field renaming, PCI redaction, truncation, and filtering.

package transform

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// Helper to create a log record with attributes
func makeLogRecord(attrs map[string]string) *logspb.LogRecord {
	lr := &logspb.LogRecord{}
	for k, v := range attrs {
		lr.Attributes = append(lr.Attributes, &commonpb.KeyValue{
			Key: k,
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: v},
			},
		})
	}
	return lr
}

// Helper to get attribute value from log record
func getAttr(lr *logspb.LogRecord, key string) string {
	for _, attr := range lr.GetAttributes() {
		if attr.GetKey() == key {
			return attr.GetValue().GetStringValue()
		}
	}
	return ""
}

func TestFieldRenames(t *testing.T) {
	tests := []struct {
		name   string
		oldKey string
		newKey string
		value  string
	}{
		// Existing renames (regression tests)
		{"application_name to cf_app_name", "application_name", "cf_app_name", "my-app"},
		{"organization_name to cf_org_name", "organization_name", "cf_org_name", "my-org"},
		{"space_name to cf_space_name", "space_name", "cf_space_name", "dev"},
		{"instance_id to cf_instance_id", "instance_id", "cf_instance_id", "abc-123"},
		// Exercise 1 renames
		{"app_id to cf_app_guid", "app_id", "cf_app_guid", "guid-12345"},
		{"organization_id to cf_org_guid", "organization_id", "cf_org_guid", "org-guid-678"},
		{"space_id to cf_space_guid", "space_id", "cf_space_guid", "space-guid-999"},
		{"source_type to cf_source_type", "source_type", "cf_source_type", "APP/PROC/WEB"},
		{"log_type to cf_log_type", "log_type", "cf_log_type", "OUT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lr := makeLogRecord(map[string]string{tt.oldKey: tt.value})

			Apply(lr)

			if got := getAttr(lr, tt.oldKey); got != "" {
				t.Errorf("old key %q still exists with value %q", tt.oldKey, got)
			}
			if got := getAttr(lr, tt.newKey); got != tt.value {
				t.Errorf("new key %q = %q, want %q", tt.newKey, got, tt.value)
			}
		})
	}
}

func TestFieldRenames_MissingField_NoOp(t *testing.T) {
	// When field doesn't exist, should be a no-op (no error, no new field)
	lr := makeLogRecord(map[string]string{
		"unrelated_field": "some_value",
	})

	transformed, actions := Apply(lr)

	// Should still have the unrelated field
	if got := getAttr(transformed, "unrelated_field"); got != "some_value" {
		t.Errorf("unrelated_field = %q, want %q", got, "some_value")
	}

	// Should not have created cf_app_guid from nothing
	if got := getAttr(transformed, "cf_app_guid"); got != "" {
		t.Errorf("cf_app_guid should not exist, got %q", got)
	}

	// Actions should not mention any renames
	for _, action := range actions {
		if action != "No transformations applied" && action != "Deleted: diego_cell_ip" && action != "Deleted: process_id" && action != "Deleted: source_id" {
			// Allow deletion actions but no renames
			if len(action) > 8 && action[:8] == "Renamed:" {
				t.Errorf("unexpected rename action: %s", action)
			}
		}
	}
}

func TestFieldRenames_AllNewFieldsInDefaultConfig(t *testing.T) {
	// Verify DefaultConfig contains all required new field renames
	cfg := DefaultConfig()

	requiredRenames := map[string]string{
		"app_id":          "cf_app_guid",
		"organization_id": "cf_org_guid",
		"space_id":        "cf_space_guid",
		"source_type":     "cf_source_type",
		"log_type":        "cf_log_type",
	}

	for oldKey, expectedNewKey := range requiredRenames {
		newKey, exists := cfg.FieldRenames[oldKey]
		if !exists {
			t.Errorf("DefaultConfig missing rename for %q", oldKey)
			continue
		}
		if newKey != expectedNewKey {
			t.Errorf("DefaultConfig[%q] = %q, want %q", oldKey, newKey, expectedNewKey)
		}
	}
}

// ABOUTME: Tests for log routing logic.
// ABOUTME: Covers routing rules, priority, and default fallback behavior.

package routing

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// Helper to create a log record with severity and attributes
func makeLogRecord(severity logspb.SeverityNumber, attrs map[string]string) *logspb.LogRecord {
	lr := &logspb.LogRecord{
		SeverityNumber: severity,
	}
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

func TestRouter_ErrorSeverityRoutesToTasErrors(t *testing.T) {
	router := DefaultRouter()

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_ERROR, map[string]string{
		"cf_app_name": "my-app",
	})

	index, rule := router.Route(lr)

	if index != "tas_errors" {
		t.Errorf("ERROR severity should route to tas_errors, got %q", index)
	}
	if rule != "error-severity" {
		t.Errorf("expected rule 'error-severity', got %q", rule)
	}
}

func TestRouter_FatalSeverityRoutesToTasErrors(t *testing.T) {
	router := DefaultRouter()

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_FATAL, map[string]string{
		"cf_app_name": "my-app",
	})

	index, rule := router.Route(lr)

	if index != "tas_errors" {
		t.Errorf("FATAL severity should route to tas_errors, got %q", index)
	}
	if rule != "error-severity" {
		t.Errorf("expected rule 'error-severity', got %q", rule)
	}
}

func TestRouter_SecurityAppRoutesToTasSecurity(t *testing.T) {
	router := DefaultRouter()

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_INFO, map[string]string{
		"cf_app_name": "security-scanner",
	})

	index, rule := router.Route(lr)

	if index != "tas_security" {
		t.Errorf("security-* apps should route to tas_security, got %q", index)
	}
	if rule != "security-app" {
		t.Errorf("expected rule 'security-app', got %q", rule)
	}
}

func TestRouter_AuditAppRoutesToTasAudit(t *testing.T) {
	router := DefaultRouter()

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_INFO, map[string]string{
		"cf_app_name": "audit-logger",
	})

	index, rule := router.Route(lr)

	if index != "tas_audit" {
		t.Errorf("audit-* apps should route to tas_audit, got %q", index)
	}
	if rule != "audit-app" {
		t.Errorf("expected rule 'audit-app', got %q", rule)
	}
}

func TestRouter_ProductionSpaceRoutesToTasProd(t *testing.T) {
	router := DefaultRouter()

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_INFO, map[string]string{
		"cf_app_name":   "my-app",
		"cf_space_name": "production",
	})

	index, rule := router.Route(lr)

	if index != "tas_prod" {
		t.Errorf("production space should route to tas_prod, got %q", index)
	}
	if rule != "production-space" {
		t.Errorf("expected rule 'production-space', got %q", rule)
	}
}

func TestRouter_DefaultFallback(t *testing.T) {
	router := DefaultRouter()

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_INFO, map[string]string{
		"cf_app_name":   "my-app",
		"cf_space_name": "development",
	})

	index, rule := router.Route(lr)

	if index != "tas_logs" {
		t.Errorf("default should route to tas_logs, got %q", index)
	}
	if rule != "default" {
		t.Errorf("expected rule 'default', got %q", rule)
	}
}

func TestRouter_PriorityErrorBeforeSecurityApp(t *testing.T) {
	// ERROR from security app should go to tas_errors, not tas_security
	router := DefaultRouter()

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_ERROR, map[string]string{
		"cf_app_name": "security-scanner",
	})

	index, rule := router.Route(lr)

	if index != "tas_errors" {
		t.Errorf("ERROR from security app should route to tas_errors (priority), got %q", index)
	}
	if rule != "error-severity" {
		t.Errorf("expected rule 'error-severity', got %q", rule)
	}
}

func TestRouter_PrioritySecurityBeforeProduction(t *testing.T) {
	// security app in production should go to tas_security, not tas_prod
	router := DefaultRouter()

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_INFO, map[string]string{
		"cf_app_name":   "security-scanner",
		"cf_space_name": "production",
	})

	index, rule := router.Route(lr)

	if index != "tas_security" {
		t.Errorf("security app in production should route to tas_security (priority), got %q", index)
	}
	if rule != "security-app" {
		t.Errorf("expected rule 'security-app', got %q", rule)
	}
}

func TestRouter_CustomRules(t *testing.T) {
	router := NewRouter([]RoutingRule{
		{
			Name:       "custom-rule",
			Conditions: map[string]string{"cf_app_name": "^custom-"},
			Index:      "custom_index",
			Priority:   1,
		},
	})

	lr := makeLogRecord(logspb.SeverityNumber_SEVERITY_NUMBER_INFO, map[string]string{
		"cf_app_name": "custom-app",
	})

	index, rule := router.Route(lr)

	if index != "custom_index" {
		t.Errorf("custom rule should route to custom_index, got %q", index)
	}
	if rule != "custom-rule" {
		t.Errorf("expected rule 'custom-rule', got %q", rule)
	}
}

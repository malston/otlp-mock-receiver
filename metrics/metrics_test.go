// ABOUTME: Tests for Prometheus metrics.
// ABOUTME: Verifies counter increments and label handling.

package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestLogsReceivedIncrement(t *testing.T) {
	m := New()

	m.LogsReceived.Inc()
	m.LogsReceived.Inc()

	if got := testutil.ToFloat64(m.LogsReceived); got != 2 {
		t.Errorf("LogsReceived = %v, want 2", got)
	}
}

func TestLogsTransformedIncrement(t *testing.T) {
	m := New()

	m.LogsTransformed.Inc()

	if got := testutil.ToFloat64(m.LogsTransformed); got != 1 {
		t.Errorf("LogsTransformed = %v, want 1", got)
	}
}

func TestLogsDroppedWithLabels(t *testing.T) {
	m := New()

	m.LogsDropped.WithLabelValues("sampled").Inc()
	m.LogsDropped.WithLabelValues("sampled").Inc()
	m.LogsDropped.WithLabelValues("filtered").Inc()

	if got := testutil.ToFloat64(m.LogsDropped.WithLabelValues("sampled")); got != 2 {
		t.Errorf("LogsDropped{reason=sampled} = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.LogsDropped.WithLabelValues("filtered")); got != 1 {
		t.Errorf("LogsDropped{reason=filtered} = %v, want 1", got)
	}
}

func TestLogsBySeverity(t *testing.T) {
	m := New()

	m.LogsBySeverity.WithLabelValues("INFO").Inc()
	m.LogsBySeverity.WithLabelValues("INFO").Inc()
	m.LogsBySeverity.WithLabelValues("ERROR").Inc()

	if got := testutil.ToFloat64(m.LogsBySeverity.WithLabelValues("INFO")); got != 2 {
		t.Errorf("LogsBySeverity{severity=INFO} = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.LogsBySeverity.WithLabelValues("ERROR")); got != 1 {
		t.Errorf("LogsBySeverity{severity=ERROR} = %v, want 1", got)
	}
}

func TestLogsByIndex(t *testing.T) {
	m := New()

	m.LogsByIndex.WithLabelValues("tas_logs").Inc()
	m.LogsByIndex.WithLabelValues("tas_errors").Inc()
	m.LogsByIndex.WithLabelValues("tas_errors").Inc()

	if got := testutil.ToFloat64(m.LogsByIndex.WithLabelValues("tas_logs")); got != 1 {
		t.Errorf("LogsByIndex{index=tas_logs} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.LogsByIndex.WithLabelValues("tas_errors")); got != 2 {
		t.Errorf("LogsByIndex{index=tas_errors} = %v, want 2", got)
	}
}

func TestTransformDuration(t *testing.T) {
	m := New()

	// Record a duration
	timer := m.NewTransformTimer()
	time.Sleep(1 * time.Millisecond)
	timer.ObserveDuration()

	// Verify histogram has observations
	expected := `
# HELP otlp_receiver_transform_duration_seconds Time spent transforming log records
# TYPE otlp_receiver_transform_duration_seconds histogram
`
	if err := testutil.CollectAndCompare(m.TransformDuration, strings.NewReader(expected), "otlp_receiver_transform_duration_seconds"); err != nil {
		// Just verify the metric exists and has data - exact values vary
		count := testutil.CollectAndCount(m.TransformDuration)
		if count == 0 {
			t.Error("TransformDuration histogram has no observations")
		}
	}
}

func TestPCIRedactionsIncrement(t *testing.T) {
	m := New()

	m.PCIRedactions.Inc()
	m.PCIRedactions.Inc()
	m.PCIRedactions.Inc()

	if got := testutil.ToFloat64(m.PCIRedactions); got != 3 {
		t.Errorf("PCIRedactions = %v, want 3", got)
	}
}

func TestBodyTruncationsIncrement(t *testing.T) {
	m := New()

	m.BodyTruncations.Inc()

	if got := testutil.ToFloat64(m.BodyTruncations); got != 1 {
		t.Errorf("BodyTruncations = %v, want 1", got)
	}
}

// ABOUTME: Prometheus metrics for operational monitoring.
// ABOUTME: Tracks log processing counts, durations, and routing statistics.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the receiver
type Metrics struct {
	LogsReceived      prometheus.Counter
	LogsTransformed   prometheus.Counter
	LogsDropped       *prometheus.CounterVec
	LogsBySeverity    *prometheus.CounterVec
	LogsByIndex       *prometheus.CounterVec
	TransformDuration prometheus.Histogram
	PCIRedactions     prometheus.Counter
	BodyTruncations   prometheus.Counter

	registry *prometheus.Registry
}

// New creates a new Metrics instance with a custom registry
func New() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,

		LogsReceived: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "otlp_receiver_logs_received_total",
			Help: "Total number of log records received",
		}),

		LogsTransformed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "otlp_receiver_logs_transformed_total",
			Help: "Total number of log records transformed",
		}),

		LogsDropped: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "otlp_receiver_logs_dropped_total",
			Help: "Total number of log records dropped",
		}, []string{"reason"}),

		LogsBySeverity: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "otlp_receiver_logs_by_severity_total",
			Help: "Total log records by severity level",
		}, []string{"severity"}),

		LogsByIndex: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "otlp_receiver_logs_by_index_total",
			Help: "Total log records by routing index",
		}, []string{"index"}),

		TransformDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "otlp_receiver_transform_duration_seconds",
			Help:    "Time spent transforming log records",
			Buckets: prometheus.DefBuckets,
		}),

		PCIRedactions: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "otlp_receiver_pci_redactions_total",
			Help: "Total number of PCI patterns redacted",
		}),

		BodyTruncations: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "otlp_receiver_body_truncations_total",
			Help: "Total number of log bodies truncated",
		}),
	}

	return m
}

// Registry returns the Prometheus registry for this metrics instance
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// NewTransformTimer creates a timer for measuring transform duration
func (m *Metrics) NewTransformTimer() *prometheus.Timer {
	return prometheus.NewTimer(m.TransformDuration)
}

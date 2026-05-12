package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds common Prometheus metrics for all services.
type Metrics struct {
	HTTPRequestsTotal    *prometheus.CounterVec
	HTTPRequestDuration  *prometheus.HistogramVec
	SQSMessagesProcessed *prometheus.CounterVec
	SQSMessageDuration   *prometheus.HistogramVec
}

// NewMetrics registers and returns common metrics.
func NewMetrics(serviceName string, reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "bodybuddy",
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total number of HTTP requests.",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"method", "path", "status"},
		),
		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   "bodybuddy",
				Subsystem:   "http",
				Name:        "request_duration_seconds",
				Help:        "HTTP request latency in seconds.",
				ConstLabels: prometheus.Labels{"service": serviceName},
				Buckets:     prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		SQSMessagesProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "bodybuddy",
				Subsystem: "sqs",
				Name:      "messages_processed_total",
				Help:      "Total number of SQS messages processed.",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"queue", "result"},
		),
		SQSMessageDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   "bodybuddy",
				Subsystem:   "sqs",
				Name:        "message_duration_seconds",
				Help:        "Time spent processing an SQS message.",
				ConstLabels: prometheus.Labels{"service": serviceName},
				Buckets:     []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{"queue"},
		),
	}

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.SQSMessagesProcessed,
		m.SQSMessageDuration,
	)

	return m
}

// NewRegistry creates a new Prometheus registry and registers standard metrics.
func NewRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// Handler returns the HTTP handler for /metrics.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

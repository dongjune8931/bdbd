package observability

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Metrics holds common Prometheus metrics for all services.
type Metrics struct {
	HTTPRequestsTotal     *prometheus.CounterVec
	HTTPRequestDuration   *prometheus.HistogramVec
	SQSMessagesProcessed  *prometheus.CounterVec
	SQSMessageDuration    *prometheus.HistogramVec
	UploadRequestsTotal   *prometheus.CounterVec
	AnalysisJobsProcessed *prometheus.CounterVec
	AnalysisJobDuration   prometheus.Histogram
	ScoreUpdatesTotal     *prometheus.CounterVec
	NotificationSentTotal *prometheus.CounterVec
	RankingReadDuration   prometheus.Histogram
}

// NewMetrics registers and returns common metrics.
func NewMetrics(serviceName string, reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   "bodybuddy",
				Subsystem:   "http",
				Name:        "requests_total",
				Help:        "Total number of HTTP requests.",
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
				Namespace:   "bodybuddy",
				Subsystem:   "sqs",
				Name:        "messages_processed_total",
				Help:        "Total number of SQS messages processed.",
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
		UploadRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   "bodybuddy",
				Name:        "upload_requests_total",
				Help:        "Total number of upload API requests processed by the service.",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"result"},
		),
		AnalysisJobsProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   "bodybuddy",
				Name:        "analysis_jobs_processed_total",
				Help:        "Total number of analysis jobs processed by the worker.",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"result"},
		),
		AnalysisJobDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace:   "bodybuddy",
				Name:        "analysis_job_duration_seconds",
				Help:        "End-to-end latency for a single analysis job.",
				ConstLabels: prometheus.Labels{"service": serviceName},
				Buckets:     []float64{0.5, 1, 2, 3, 5, 8, 13, 21, 34},
			},
		),
		ScoreUpdatesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   "bodybuddy",
				Name:        "score_updates_total",
				Help:        "Total number of score update requests handled by score-service.",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"result"},
		),
		NotificationSentTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   "bodybuddy",
				Name:        "notification_sent_total",
				Help:        "Total number of notifications sent by notification-worker.",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"result"},
		),
		RankingReadDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace:   "bodybuddy",
				Name:        "ranking_read_duration_seconds",
				Help:        "Latency of ranking reads served by score-service.",
				ConstLabels: prometheus.Labels{"service": serviceName},
				Buckets:     []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
			},
		),
	}

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequestsTotal,
		m.HTTPRequestDuration,
		m.SQSMessagesProcessed,
		m.SQSMessageDuration,
		m.UploadRequestsTotal,
		m.AnalysisJobsProcessed,
		m.AnalysisJobDuration,
		m.ScoreUpdatesTotal,
		m.NotificationSentTotal,
		m.RankingReadDuration,
	)

	return m
}

// NewRegistry creates a new Prometheus registry and registers standard metrics.
func NewRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// InitTracer sets up the global OpenTelemetry tracer with an OTLP gRPC exporter.
// endpoint should be the OTel Collector address, e.g. "opentelemetry-collector.bodybuddy-system:4317".
// Returns a shutdown function that must be called on service exit.
func InitTracer(ctx context.Context, serviceName, endpoint string) (func(context.Context) error, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, err
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// Handler returns the HTTP handler for /metrics.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

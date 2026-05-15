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

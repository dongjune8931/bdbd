package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/bodybuddy/app/internal/cache"
	"github.com/bodybuddy/app/internal/config"
	"github.com/bodybuddy/app/internal/db"
	"github.com/bodybuddy/app/internal/domain"
	bbhttp "github.com/bodybuddy/app/internal/http"
	"github.com/bodybuddy/app/internal/inference"
	"github.com/bodybuddy/app/internal/observability"
	"github.com/bodybuddy/app/internal/queue"
	"github.com/gin-gonic/gin"
)

type analysisPayload struct {
	UserID   string `json:"user_id"`
	UploadID string `json:"upload_id"`
	S3Key    string `json:"s3_key"`
}

type s3ObjectCreatedEvent struct {
	Records []struct {
		S3 struct {
			Object struct {
				Key string `json:"key"`
			} `json:"object"`
		} `json:"s3"`
	} `json:"Records"`
	Detail struct {
		Object struct {
			Key string `json:"key"`
		} `json:"object"`
	} `json:"detail"`
}

func main() {
	// --- Logging ---
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger.With("service", "analysis-worker"))

	// --- Config ---
	cfg := config.MustLoadAnalysisWorker()

	// --- Tracer ---
	if cfg.OTelEndpoint != "" {
		shutdown, err := observability.InitTracer(context.Background(), cfg.ServiceName, cfg.OTelEndpoint)
		if err != nil {
			slog.Warn("failed to init tracer, continuing without tracing", "error", err)
		} else {
			defer shutdown(context.Background()) //nolint:errcheck
		}
	}

	// --- Observability ---
	reg := observability.NewRegistry()
	metrics := observability.NewMetrics(cfg.ServiceName, reg)

	// --- DB ---
	dbPool, err := db.New(context.Background(), cfg.DSN(), 4)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	// --- Cache ---
	redisClient := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisTLSEnabled)
	defer redisClient.Close()

	// --- SQS ---
	sqsClient, err := queue.New(context.Background(), cfg.AWSRegion, cfg.SQSEndpoint)
	if err != nil {
		slog.Error("failed to create SQS client", "error", err)
		os.Exit(1)
	}

	var inferenceClient *inference.Client
	if cfg.InferenceServiceURL != "" {
		inferenceClient = inference.NewClient(
			cfg.InferenceServiceURL,
			time.Duration(cfg.InferenceTimeoutSeconds)*time.Second,
		)
	}

	// --- Metrics HTTP Server (separate from SQS polling loop) ---
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(bbhttp.Recovery(slog.Default()))

	health := bbhttp.NewHealthHandlers(dbPool, redisClient)
	bbhttp.RegisterHealthRoutes(r, health)
	r.GET("/metrics", gin.WrapH(observability.Handler(reg)))

	metricsAddr := fmt.Sprintf(":%d", cfg.MetricsPort)
	metricsSrv := &http.Server{
		Addr:         metricsAddr,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("analysis-worker metrics server starting", "addr", metricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
		}
	}()

	// --- Graceful Shutdown Setup ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- SQS Polling Loop ---
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runPollingLoop(ctx, cfg, sqsClient, inferenceClient, metrics)
	}()

	// Wait for termination signal.
	<-ctx.Done()
	slog.Info("shutdown signal received, stopping polling loop")

	// Wait for in-flight messages to finish before shutting down the metrics server.
	wg.Wait()
	slog.Info("all in-flight messages processed")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("metrics server shutdown failed", "error", err)
	}
	slog.Info("analysis-worker stopped")
}

// runPollingLoop polls the analysis queue until ctx is cancelled.
// When the context is cancelled (SIGTERM/SIGINT), it stops accepting new messages
// but finishes processing any messages already received.
func runPollingLoop(ctx context.Context, cfg *config.AnalysisWorker, sqsClient *queue.Client, inferenceClient *inference.Client, metrics *observability.Metrics) {
	slog.Info("analysis-worker polling started", "queue", cfg.AnalysisQueueURL)

	for {
		// Stop polling as soon as the shutdown signal arrives.
		select {
		case <-ctx.Done():
			slog.Info("polling loop stopping")
			return
		default:
		}

		msgs, err := sqsClient.ReceiveMessages(
			ctx,
			cfg.AnalysisQueueURL,
			cfg.SQSMaxMessages,
			cfg.SQSWaitTimeSeconds,
			cfg.SQSVisibilityTimeout,
		)
		if err != nil {
			// ctx cancelled during long poll — normal shutdown path.
			if ctx.Err() != nil {
				return
			}
			slog.Error("failed to receive SQS messages", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range msgs {
			// Process each message synchronously to keep the shutdown flow simple.
			// In a high-throughput scenario, spawn bounded goroutines with a semaphore.
			processMessage(ctx, cfg, sqsClient, inferenceClient, msg, metrics)
		}
	}
}

// processMessage executes the analysis pipeline for a single SQS message.
// Message body is expected to contain a JSON object with at least:
//
//	{ "user_id": "...", "upload_id": "...", "s3_key": "..." }
func processMessage(ctx context.Context, cfg *config.AnalysisWorker, sqsClient *queue.Client, inferenceClient *inference.Client, msg queue.Message, metrics *observability.Metrics) {
	// Extract trace context propagated via SQS message attributes.
	ctx = otel.GetTextMapPropagator().Extract(ctx, queue.NewSQSReadCarrier(msg.Attributes))

	tracer := otel.Tracer("analysis-worker")
	ctx, span := tracer.Start(ctx, "analysis-worker.processMessage")
	defer span.End()

	start := time.Now()

	payload, err := decodeAnalysisPayload(msg.Body)
	if err != nil {
		slog.Error("failed to parse message body", "message_id", msg.MessageID, "error", err)
		span.SetStatus(codes.Error, "failed to parse message body")
		// Do not delete — let DLQ handle malformed messages after maxReceiveCount.
		return
	}

	span.SetAttributes(
		attribute.String("messaging.message_id", msg.MessageID),
		attribute.String("user.id", payload.UserID),
		attribute.String("upload.id", payload.UploadID),
	)

	slog.Info("processing analysis message",
		"message_id", msg.MessageID,
		"user_id", payload.UserID,
		"upload_id", payload.UploadID,
	)

	// Step 1: Run remote inference if configured. Fall back to in-process mock
	// when the MVP inference service is unavailable or intentionally disabled.
	breakdown, accelerator, modelVersion, err := runInference(ctx, cfg, inferenceClient, payload, msg.MessageID, metrics)
	if err != nil {
		slog.Error("failed to analyze upload",
			"message_id", msg.MessageID,
			"error", err,
		)
		span.SetStatus(codes.Error, err.Error())
		metrics.AnalysisJobsProcessed.WithLabelValues("failure").Inc()
		return
	}

	span.SetAttributes(
		attribute.String("inference.accelerator", accelerator),
		attribute.String("inference.model_version", modelVersion),
	)

	slog.Info("analysis complete",
		"message_id", msg.MessageID,
		"user_id", payload.UserID,
		"score", breakdown.Total,
		"accelerator", accelerator,
		"model_version", modelVersion,
	)

	// Step 2: Call score-service to persist the result.
	if err := postScore(ctx, cfg.ScoreServiceURL, payload.UserID, payload.UploadID, breakdown); err != nil {
		slog.Error("failed to post score to score-service",
			"message_id", msg.MessageID,
			"error", err,
		)
		span.SetStatus(codes.Error, err.Error())
		metrics.AnalysisJobsProcessed.WithLabelValues("failure").Inc()
		// Do not delete — let SQS retry (up to maxReceiveCount=3) before DLQ.
		return
	}

	// Step 3: Delete the message only after successful processing (at-least-once semantics).
	if err := sqsClient.DeleteMessage(ctx, cfg.AnalysisQueueURL, msg.ReceiptHandle); err != nil {
		slog.Error("failed to delete SQS message", "message_id", msg.MessageID, "error", err)
	}

	duration := time.Since(start)
	metrics.SQSMessagesProcessed.WithLabelValues("analysis-queue", "success").Inc()
	metrics.SQSMessageDuration.WithLabelValues("analysis-queue").Observe(duration.Seconds())
	metrics.AnalysisJobsProcessed.WithLabelValues("success").Inc()
	metrics.AnalysisJobDuration.Observe(duration.Seconds())

	slog.Info("message processed successfully",
		"message_id", msg.MessageID,
		"duration_ms", duration.Milliseconds(),
	)
}

func runInference(
	ctx context.Context,
	cfg *config.AnalysisWorker,
	inferenceClient *inference.Client,
	payload analysisPayload,
	messageID string,
	metrics *observability.Metrics,
) (domain.ScoreBreakdown, string, string, error) {
	if inferenceClient != nil {
		inferenceStart := time.Now()
		inferCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.InferenceTimeoutSeconds)*time.Second)
		defer cancel()

		resp, err := inferenceClient.Analyze(inferCtx, inference.Request{
			UserID:   payload.UserID,
			UploadID: payload.UploadID,
			S3Key:    payload.S3Key,
		})
		metrics.InferenceRequestDuration.WithLabelValues("remote").Observe(time.Since(inferenceStart).Seconds())
		if err == nil {
			metrics.InferenceRequestsTotal.WithLabelValues("success", "remote").Inc()
			return resp.ScoreBreakdown, resp.Accelerator, resp.ModelVersion, nil
		}

		slog.Warn("remote inference failed",
			"message_id", messageID,
			"upload_id", payload.UploadID,
			"error", err,
		)
		metrics.InferenceRequestsTotal.WithLabelValues("failure", "remote").Inc()
		if !cfg.InferenceFallbackToMock {
			return domain.ScoreBreakdown{}, "", "", err
		}
	}

	mockStart := time.Now()
	breakdown, err := runMockInference(ctx, payload.UserID, messageID, metrics)
	metrics.InferenceRequestDuration.WithLabelValues("mock").Observe(time.Since(mockStart).Seconds())
	if err != nil {
		metrics.InferenceRequestsTotal.WithLabelValues("failure", "mock").Inc()
		return domain.ScoreBreakdown{}, "", "", err
	}

	if inferenceClient != nil {
		metrics.InferenceRequestsTotal.WithLabelValues("fallback", "mock").Inc()
	} else {
		metrics.InferenceRequestsTotal.WithLabelValues("success", "mock").Inc()
	}
	return breakdown, "cpu-mock-fallback", "mock-ocr-v1", nil
}

func decodeAnalysisPayload(body string) (analysisPayload, error) {
	var direct analysisPayload
	if err := json.Unmarshal([]byte(body), &direct); err == nil && direct.UserID != "" && direct.UploadID != "" && direct.S3Key != "" {
		return direct, nil
	}

	var event s3ObjectCreatedEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		return analysisPayload{}, fmt.Errorf("decoding S3 event: %w", err)
	}

	key := event.Detail.Object.Key
	if key == "" && len(event.Records) > 0 {
		key = event.Records[0].S3.Object.Key
	}
	if key == "" {
		return analysisPayload{}, fmt.Errorf("message contains no supported upload payload or S3 object key")
	}

	decodedKey, err := url.QueryUnescape(key)
	if err != nil {
		return analysisPayload{}, fmt.Errorf("decoding S3 object key: %w", err)
	}
	parts := strings.Split(strings.Trim(decodedKey, "/"), "/")
	if len(parts) != 3 || parts[0] != "uploads" || parts[1] == "" || parts[2] == "" {
		return analysisPayload{}, fmt.Errorf("unexpected S3 upload key %q", decodedKey)
	}

	return analysisPayload{
		UserID:   parts[1],
		UploadID: parts[2],
		S3Key:    decodedKey,
	}, nil
}

func runMockInference(ctx context.Context, userID, messageID string, metrics *observability.Metrics) (domain.ScoreBreakdown, error) {
	// #nosec G404 — intentional use of weak RNG for jitter simulation in mock mode.
	jitter := time.Duration(2000+rand.Intn(3000)) * time.Millisecond //nolint:gosec
	select {
	case <-time.After(jitter):
	case <-ctx.Done():
		slog.Warn("context cancelled during mock OCR sleep, re-queuing message",
			"message_id", messageID,
		)
		metrics.AnalysisJobsProcessed.WithLabelValues("retry").Inc()
		return domain.ScoreBreakdown{}, ctx.Err()
	}

	return domain.CalculateMockScore(userID), nil
}

// postScore calls score-service POST /internal/v1/score with the analysis result.
func postScore(ctx context.Context, baseURL, userID, uploadID string, breakdown domain.ScoreBreakdown) error {
	body := map[string]interface{}{
		"user_id":         userID,
		"upload_id":       uploadID,
		"score":           breakdown.Total,
		"score_breakdown": breakdown,
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding score payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/internal/v1/score",
		bytes.NewReader(encoded),
	)
	if err != nil {
		return fmt.Errorf("creating score request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling score-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("score-service returned status %d", resp.StatusCode)
	}
	return nil
}

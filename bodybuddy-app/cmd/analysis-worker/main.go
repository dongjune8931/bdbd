package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bodybuddy/app/internal/cache"
	"github.com/bodybuddy/app/internal/config"
	"github.com/bodybuddy/app/internal/db"
	bbhttp "github.com/bodybuddy/app/internal/http"
	"github.com/bodybuddy/app/internal/domain"
	"github.com/bodybuddy/app/internal/observability"
	"github.com/bodybuddy/app/internal/queue"
	"github.com/gin-gonic/gin"
)

func main() {
	// --- Logging ---
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger.With("service", "analysis-worker"))

	// --- Config ---
	cfg := config.MustLoadAnalysisWorker()

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
		runPollingLoop(ctx, cfg, sqsClient, metrics)
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
func runPollingLoop(ctx context.Context, cfg *config.AnalysisWorker, sqsClient *queue.Client, metrics *observability.Metrics) {
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
			processMessage(ctx, cfg, sqsClient, msg, metrics)
		}
	}
}

// processMessage executes the Mock OCR pipeline for a single SQS message.
// Message body is expected to contain a JSON object with at least:
//
//	{ "user_id": "...", "upload_id": "...", "s3_key": "..." }
func processMessage(ctx context.Context, cfg *config.AnalysisWorker, sqsClient *queue.Client, msg queue.Message, metrics *observability.Metrics) {
	start := time.Now()

	var payload struct {
		UserID   string `json:"user_id"`
		UploadID string `json:"upload_id"`
		S3Key    string `json:"s3_key"`
	}
	if err := json.Unmarshal([]byte(msg.Body), &payload); err != nil {
		slog.Error("failed to parse message body", "message_id", msg.MessageID, "error", err)
		// Do not delete — let DLQ handle malformed messages after maxReceiveCount.
		return
	}

	slog.Info("processing analysis message",
		"message_id", msg.MessageID,
		"user_id", payload.UserID,
		"upload_id", payload.UploadID,
	)

	// Step 1: Simulate OCR processing delay (2–5 seconds).
	// Real implementation would call an OCR API here with retry + timeout.
	// #nosec G404 — intentional use of weak RNG for jitter simulation
	jitter := time.Duration(2000+rand.Intn(3000)) * time.Millisecond //nolint:gosec
	select {
	case <-time.After(jitter):
	case <-ctx.Done():
		slog.Warn("context cancelled during mock OCR sleep, re-queuing message",
			"message_id", msg.MessageID,
		)
		return
	}

	// Step 2: Generate a deterministic mock score based on user ID.
	breakdown := domain.CalculateMockScore(payload.UserID)

	slog.Info("mock OCR complete",
		"message_id", msg.MessageID,
		"user_id", payload.UserID,
		"score", breakdown.Total,
	)

	// Step 3: Call score-service to persist the result.
	if err := postScore(ctx, cfg.ScoreServiceURL, payload.UserID, payload.UploadID, breakdown); err != nil {
		slog.Error("failed to post score to score-service",
			"message_id", msg.MessageID,
			"error", err,
		)
		// Do not delete — let SQS retry (up to maxReceiveCount=3) before DLQ.
		return
	}

	// Step 4: Delete the message only after successful processing (at-least-once semantics).
	if err := sqsClient.DeleteMessage(ctx, cfg.AnalysisQueueURL, msg.ReceiptHandle); err != nil {
		slog.Error("failed to delete SQS message", "message_id", msg.MessageID, "error", err)
	}

	duration := time.Since(start)
	metrics.SQSMessagesProcessed.WithLabelValues("analysis-queue", "success").Inc()
	metrics.SQSMessageDuration.WithLabelValues("analysis-queue").Observe(duration.Seconds())

	slog.Info("message processed successfully",
		"message_id", msg.MessageID,
		"duration_ms", duration.Milliseconds(),
	)
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

	client := &http.Client{Timeout: 10 * time.Second}
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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bodybuddy/app/internal/config"
	"github.com/bodybuddy/app/internal/domain"
	bbhttp "github.com/bodybuddy/app/internal/http"
	"github.com/bodybuddy/app/internal/inference"
	"github.com/bodybuddy/app/internal/observability"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type inferenceRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	UploadID string `json:"upload_id" binding:"required"`
	S3Key    string `json:"s3_key" binding:"required"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger.With("service", "inference-service"))

	cfg := config.MustLoadInferenceService()

	if cfg.OTelEndpoint != "" {
		shutdown, err := observability.InitTracer(context.Background(), cfg.ServiceName, cfg.OTelEndpoint)
		if err != nil {
			slog.Warn("failed to init tracer, continuing without tracing", "error", err)
		} else {
			defer shutdown(context.Background()) //nolint:errcheck
		}
	}

	reg := observability.NewRegistry()
	metrics := observability.NewMetrics(cfg.ServiceName, reg)
	imageStore, err := inference.NewS3ImageStore(
		context.Background(),
		cfg.AWSRegion,
		cfg.S3Endpoint,
		cfg.S3Bucket,
		cfg.OCRMaxImageBytes,
	)
	if err != nil {
		slog.Error("failed to initialize S3 image store", "error", err)
		os.Exit(1)
	}
	runtimeClient := inference.NewRuntimeClient(
		cfg.OCRRuntimeURL,
		time.Duration(cfg.OCRRuntimeTimeoutSeconds)*time.Second,
	)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(bbhttp.Recovery(slog.Default()))
	r.Use(bbhttp.RequestID())
	r.Use(otelgin.Middleware(cfg.ServiceName))
	r.Use(bbhttp.Metrics(metrics))
	r.Use(bbhttp.Logger(slog.Default()))

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		readyCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := runtimeClient.Ready(readyCtx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	r.GET("/metrics", gin.WrapH(observability.Handler(reg)))
	r.POST("/internal/v1/inference", func(c *gin.Context) {
		start := time.Now()
		var req inferenceRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		image, _, err := imageStore.Load(c.Request.Context(), req.S3Key)
		if err != nil {
			metrics.InferenceRequestsTotal.WithLabelValues("failure", "ocr-runtime").Inc()
			slog.Warn("failed to load inference image", "upload_id", req.UploadID, "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "inference image unavailable"})
			return
		}

		runtimeResult, err := runtimeClient.Analyze(c.Request.Context(), image)
		if err != nil {
			metrics.InferenceRequestsTotal.WithLabelValues("failure", "ocr-runtime").Inc()
			slog.Warn("OCR runtime request failed", "upload_id", req.UploadID, "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OCR runtime unavailable"})
			return
		}

		lines := make([]domain.OCRTextLine, 0, len(runtimeResult.Lines))
		for _, line := range runtimeResult.Lines {
			lines = append(lines, domain.OCRTextLine{Text: line.Text, Confidence: line.Confidence})
		}
		ocr, err := domain.ParseOCRLines(lines)
		if err != nil {
			metrics.InferenceRequestsTotal.WithLabelValues("failure", runtimeMode(runtimeResult.Accelerator)).Inc()
			slog.Warn("OCR result missing required fields",
				"upload_id", req.UploadID,
				"recognized_lines", len(lines),
				"error", err,
			)
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "required body-composition fields not recognized"})
			return
		}
		score := domain.CalculateScoreFromOCR(ocr)
		duration := time.Since(start)
		mode := runtimeMode(runtimeResult.Accelerator)

		metrics.InferenceRequestsTotal.WithLabelValues("success", mode).Inc()
		metrics.InferenceRequestDuration.WithLabelValues(mode).Observe(duration.Seconds())

		trace.SpanFromContext(c.Request.Context()).SetAttributes(
			attribute.String("inference.accelerator", runtimeResult.Accelerator),
			attribute.String("inference.model_version", runtimeResult.ModelVersion),
			attribute.Int("inference.recognized_lines", len(lines)),
			attribute.Int64("inference.model_duration_ms", runtimeResult.InferenceMs),
		)

		c.JSON(http.StatusOK, gin.H{
			"ocr":             ocr,
			"score_breakdown": score,
			"accelerator":     runtimeResult.Accelerator,
			"model_version":   runtimeResult.ModelVersion,
			"duration_ms":     duration.Milliseconds(),
		})
	})

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("inference-service starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
	slog.Info("inference-service stopped")
}

func runtimeMode(accelerator string) string {
	if strings.EqualFold(strings.TrimSpace(accelerator), "cpu") {
		return "cpu"
	}
	return "gpu"
}

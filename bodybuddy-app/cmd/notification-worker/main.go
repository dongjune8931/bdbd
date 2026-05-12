package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/gin-gonic/gin"

	"github.com/bodybuddy/app/internal/cache"
	"github.com/bodybuddy/app/internal/config"
	"github.com/bodybuddy/app/internal/db"
	bbhttp "github.com/bodybuddy/app/internal/http"
	"github.com/bodybuddy/app/internal/observability"
	"github.com/bodybuddy/app/internal/queue"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger.With("service", "notification-worker"))

	cfg := config.MustLoadNotificationWorker()

	reg := observability.NewRegistry()
	metrics := observability.NewMetrics(cfg.ServiceName, reg)

	dbPool, err := db.New(context.Background(), cfg.DSN(), 4)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	redisClient := cache.New(cfg.RedisAddr, cfg.RedisPassword)
	defer redisClient.Close()

	sqsClient, err := queue.New(context.Background(), cfg.AWSRegion, cfg.SQSEndpoint)
	if err != nil {
		slog.Error("failed to create SQS client", "error", err)
		os.Exit(1)
	}

	sesClient, err := newSESClient(context.Background(), cfg)
	if err != nil {
		slog.Error("failed to create SES client", "error", err)
		os.Exit(1)
	}

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
		slog.Info("notification-worker metrics server starting", "addr", metricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runPollingLoop(ctx, cfg, sqsClient, sesClient, dbPool, metrics)
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received, stopping polling loop")

	wg.Wait()
	slog.Info("all in-flight notifications processed")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("metrics server shutdown failed", "error", err)
	}
	slog.Info("notification-worker stopped")
}

func newSESClient(ctx context.Context, cfg *config.NotificationWorker) (*ses.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.SESRegion),
	)
	if err != nil {
		return nil, fmt.Errorf("loading aws config for SES: %w", err)
	}

	// Override endpoint for LocalStack (SES_ENDPOINT env var).
	opts := []func(*ses.Options){}
	if cfg.SESEndpoint != "" {
		endpoint := cfg.SESEndpoint
		opts = append(opts, func(o *ses.Options) {
			o.BaseEndpoint = &endpoint
		})
	}
	return ses.NewFromConfig(awsCfg, opts...), nil
}

func runPollingLoop(ctx context.Context, cfg *config.NotificationWorker, sqsClient *queue.Client, sesClient *ses.Client, pool *db.Pool, metrics *observability.Metrics) {
	slog.Info("notification-worker polling started", "queue", cfg.NotificationQueueURL)

	for {
		select {
		case <-ctx.Done():
			slog.Info("polling loop stopping")
			return
		default:
		}

		msgs, err := sqsClient.ReceiveMessages(ctx, cfg.NotificationQueueURL, 10, 20, 60)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("failed to receive SQS messages", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range msgs {
			processNotification(ctx, cfg, sqsClient, sesClient, pool, msg, metrics)
		}
	}
}

// notificationPayload matches what score-service publishes.
// Email is not included in the queue message; we look it up from DB to keep messages thin.
type notificationPayload struct {
	UserID   string `json:"user_id"`
	UploadID string `json:"upload_id"`
	Score    int    `json:"score"`
}

func processNotification(ctx context.Context, cfg *config.NotificationWorker, sqsClient *queue.Client, sesClient *ses.Client, pool *db.Pool, msg queue.Message, metrics *observability.Metrics) {
	start := time.Now()

	var payload notificationPayload
	if err := json.Unmarshal([]byte(msg.Body), &payload); err != nil {
		slog.Error("failed to parse notification message", "message_id", msg.MessageID, "error", err)
		// Malformed message — do not delete, let DLQ handle after maxReceiveCount.
		return
	}

	// Look up recipient email from DB; keeps the queue message schema thin.
	var email string
	err := pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`,
		payload.UserID,
	).Scan(&email)
	if err != nil {
		slog.Error("failed to look up user email",
			"message_id", msg.MessageID,
			"user_id", payload.UserID,
			"error", err,
		)
		// Transient DB error — do not delete, let SQS retry.
		return
	}

	slog.Info("processing notification",
		"message_id", msg.MessageID,
		"user_id", payload.UserID,
		"email", email,
	)

	if err := sendEmail(ctx, sesClient, cfg.FromEmail, email, payload.Score); err != nil {
		slog.Error("failed to send notification email",
			"message_id", msg.MessageID,
			"user_id", payload.UserID,
			"error", err,
		)
		// SES failure — do not delete, let SQS retry → DLQ.
		return
	}

	if err := sqsClient.DeleteMessage(ctx, cfg.NotificationQueueURL, msg.ReceiptHandle); err != nil {
		slog.Error("failed to delete notification message", "message_id", msg.MessageID, "error", err)
	}

	duration := time.Since(start)
	metrics.SQSMessagesProcessed.WithLabelValues("notification-queue", "success").Inc()
	metrics.SQSMessageDuration.WithLabelValues("notification-queue").Observe(duration.Seconds())

	slog.Info("notification sent",
		"message_id", msg.MessageID,
		"user_id", payload.UserID,
		"duration_ms", duration.Milliseconds(),
	)
}

func sendEmail(ctx context.Context, client *ses.Client, fromEmail, toEmail string, score int) error {
	subject := "BodyBuddy: Your score has been updated!"
	body := fmt.Sprintf(
		"Hello!\n\nYour inbody analysis is complete. Your new score is %d.\n\nKeep it up!\n\nBodyBuddy Team",
		score,
	)

	_, err := client.SendEmail(ctx, &ses.SendEmailInput{
		Source: aws.String(fromEmail),
		Destination: &sestypes.Destination{
			ToAddresses: []string{toEmail},
		},
		Message: &sestypes.Message{
			Subject: &sestypes.Content{Data: aws.String(subject), Charset: aws.String("UTF-8")},
			Body: &sestypes.Body{
				Text: &sestypes.Content{Data: aws.String(body), Charset: aws.String("UTF-8")},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("SES send email: %w", err)
	}
	return nil
}

var _ bbhttp.Pinger = (*db.Pool)(nil)
var _ bbhttp.Pinger = (*cache.Client)(nil)

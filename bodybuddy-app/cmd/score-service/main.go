package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/bodybuddy/app/internal/auth"
	"github.com/bodybuddy/app/internal/cache"
	"github.com/bodybuddy/app/internal/config"
	"github.com/bodybuddy/app/internal/db"
	"github.com/bodybuddy/app/internal/domain"
	bbhttp "github.com/bodybuddy/app/internal/http"
	"github.com/bodybuddy/app/internal/observability"
	"github.com/bodybuddy/app/internal/queue"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger.With("service", "score-service"))

	cfg := config.MustLoadScoreService()

	// Init tracer — non-fatal if OTel Collector is unreachable.
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

	dbPool, err := db.New(context.Background(), cfg.DSN(), 8)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()
	observability.RegisterScoreDBPoolMetrics(reg, cfg.ServiceName, dbPool.Pool)

	redisClient := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisTLSEnabled)
	defer redisClient.Close()

	sqsClient, err := queue.New(context.Background(), cfg.AWSRegion, cfg.SQSEndpoint)
	if err != nil {
		slog.Error("failed to create SQS client", "error", err)
		os.Exit(1)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(bbhttp.Recovery(slog.Default()))
	r.Use(bbhttp.RequestID())
	r.Use(otelgin.Middleware(cfg.ServiceName))
	r.Use(bbhttp.Metrics(metrics))
	r.Use(bbhttp.Logger(slog.Default()))

	health := bbhttp.NewHealthHandlers(dbPool, redisClient)
	bbhttp.RegisterHealthRoutes(r, health)
	r.GET("/metrics", gin.WrapH(observability.Handler(reg)))

	rdb := redisClient.Client // *redis.Client (embedded field)

	// Public ranking endpoints (read-heavy, no auth required).
	v1 := r.Group("/api/v1")
	{
		v1.GET("/ranking", getRankingHandler(rdb, metrics))
		v1.GET("/score/:user_id", getScoreHandler(dbPool, rdb))
	}

	// Internal endpoint called by analysis-worker.
	internal := r.Group("/internal/v1")
	{
		internal.POST("/score", updateScoreHandler(cfg, dbPool, rdb, sqsClient, metrics))
	}

	// Protected user endpoints.
	protected := v1.Group("")
	protected.Use(auth.Middleware(cfg.JWTSecret))
	{
		protected.GET("/my/score", getMyScoreHandler(dbPool, rdb))
		protected.GET("/my/character", getCharacterHandler(dbPool))
	}

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("score-service starting", "addr", addr)
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
	slog.Info("score-service stopped")
}

// getRankingHandler returns top-N ranking from Redis sorted set.
func getRankingHandler(rdb *redis.Client, metrics *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		tracer := otel.Tracer("score-service")
		start := time.Now()
		limitStr := c.DefaultQuery("limit", "10")
		limit, err := strconv.ParseInt(limitStr, 10, 64)
		if err != nil || limit <= 0 || limit > 100 {
			limit = 10
		}

		redisStart := time.Now()
		lookupCtx, lookupSpan := tracer.Start(c.Request.Context(), "score-service.rankingLookup")
		ranking, err := domain.GetTopRanking(lookupCtx, rdb, limit)
		lookupSpan.End()
		metrics.ScoreRedisReadDuration.Observe(time.Since(redisStart).Seconds())
		if err != nil {
			metrics.ScoreRankingCacheLookupsTotal.WithLabelValues("error").Inc()
			slog.Error("failed to get ranking", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if len(ranking) == 0 {
			metrics.ScoreRankingCacheLookupsTotal.WithLabelValues("miss").Inc()
		} else {
			metrics.ScoreRankingCacheLookupsTotal.WithLabelValues("hit").Inc()
		}
		metrics.RankingReadDuration.Observe(time.Since(start).Seconds())

		c.JSON(http.StatusOK, gin.H{"ranking": ranking})
	}
}

// getScoreHandler returns character state and rank for a given user.
func getScoreHandler(pool *db.Pool, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.Param("user_id")

		ch, err := domain.GetUserScore(c.Request.Context(), pool.Pool, userID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}

		rank, _ := domain.GetUserRank(c.Request.Context(), rdb, userID)
		c.JSON(http.StatusOK, gin.H{
			"user_id":     ch.UserID,
			"name":        ch.Name,
			"level":       ch.Level,
			"total_score": ch.TotalScore,
			"rank":        rank,
		})
	}
}

type updateScoreRequest struct {
	UserID         string                `json:"user_id" binding:"required"`
	UploadID       string                `json:"upload_id" binding:"required"`
	Score          int                   `json:"score" binding:"required"`
	ScoreBreakdown domain.ScoreBreakdown `json:"score_breakdown"`
}

// updateScoreHandler is called by analysis-worker after Mock OCR processing.
func updateScoreHandler(cfg *config.ScoreService, pool *db.Pool, rdb *redis.Client, sqsClient *queue.Client, metrics *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		tracer := otel.Tracer("score-service")
		var req updateScoreRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		entry := domain.ScoreEntry{
			UserID:    req.UserID,
			UploadID:  req.UploadID,
			Score:     req.Score,
			Breakdown: req.ScoreBreakdown,
		}

		dbStart := time.Now()
		updateCtx, updateSpan := tracer.Start(c.Request.Context(), "score-service.persistScore")
		inserted, err := domain.UpdateScore(updateCtx, pool.Pool, rdb, entry)
		updateSpan.End()
		metrics.ScoreDBOperationDuration.WithLabelValues("update_score").Observe(time.Since(dbStart).Seconds())
		if err != nil {
			slog.Error("failed to update score", "user_id", req.UserID, "error", err)
			metrics.ScoreUpdatesTotal.WithLabelValues("error").Inc()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		// Publish notification only when the score was actually saved.
		// Duplicate SQS redeliveries return inserted=false — skipping here
		// closes idempotency at the full workflow level, not just score storage.
		if inserted {
			notifMsg := fmt.Sprintf(
				`{"user_id":%q,"upload_id":%q,"score":%d}`,
				req.UserID, req.UploadID, req.Score,
			)
			enqueueStart := time.Now()
			enqueueCtx, enqueueSpan := tracer.Start(c.Request.Context(), "score-service.enqueueNotification")
			_, err := sqsClient.SendMessage(enqueueCtx, cfg.NotificationQueueURL, notifMsg, nil)
			enqueueSpan.End()
			metrics.ScoreNotificationEnqueueDuration.Observe(time.Since(enqueueStart).Seconds())
			if err != nil {
				slog.Warn("failed to publish notification event", "user_id", req.UserID, "error", err)
			}
		}

		slog.Info("score updated",
			"user_id", req.UserID,
			"upload_id", req.UploadID,
			"score", req.Score,
		)
		if inserted {
			metrics.ScoreUpdatesTotal.WithLabelValues("success").Inc()
		} else {
			metrics.ScoreUpdatesTotal.WithLabelValues("duplicate").Inc()
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok", "score": req.Score})
	}
}

// getMyScoreHandler returns the authenticated user's character state and rank.
func getMyScoreHandler(pool *db.Pool, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, _ := auth.ClaimsFromContext(c)

		ch, err := domain.GetUserScore(c.Request.Context(), pool.Pool, claims.UserID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "character not found"})
			return
		}

		rank, _ := domain.GetUserRank(c.Request.Context(), rdb, claims.UserID)
		c.JSON(http.StatusOK, gin.H{
			"user_id":     ch.UserID,
			"name":        ch.Name,
			"level":       ch.Level,
			"total_score": ch.TotalScore,
			"rank":        rank,
		})
	}
}

// getCharacterHandler returns the authenticated user's character.
func getCharacterHandler(pool *db.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, _ := auth.ClaimsFromContext(c)

		ch, err := domain.GetUserScore(c.Request.Context(), pool.Pool, claims.UserID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "character not found"})
			return
		}

		c.JSON(http.StatusOK, ch)
	}
}

var _ bbhttp.Pinger = (*db.Pool)(nil)
var _ bbhttp.Pinger = (*cache.Client)(nil)

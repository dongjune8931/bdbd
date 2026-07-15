package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bodybuddy/app/internal/auth"
	"github.com/bodybuddy/app/internal/cache"
	"github.com/bodybuddy/app/internal/config"
	"github.com/bodybuddy/app/internal/db"
	"github.com/bodybuddy/app/internal/domain"
	bbhttp "github.com/bodybuddy/app/internal/http"
	"github.com/bodybuddy/app/internal/observability"
	"github.com/bodybuddy/app/internal/storage"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger.With("service", "user-service"))

	cfg := config.MustLoadUserService()

	// Init tracer — non-fatal if OTel Collector is unreachable
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

	redisClient := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisTLSEnabled)
	defer redisClient.Close()

	presignEndpoint := cfg.S3PresignEndpoint
	if presignEndpoint == "" {
		presignEndpoint = cfg.S3Endpoint
	}
	s3Presigner, err := storage.NewS3Presigner(context.Background(), cfg.AWSRegion, presignEndpoint, cfg.S3Bucket)
	if err != nil {
		slog.Error("failed to create S3 presigner", "error", err)
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

	v1 := r.Group("/api/v1")
	{
		v1.POST("/auth/register", registerHandler(cfg, dbPool))
		v1.POST("/auth/login", loginHandler(cfg, dbPool))
	}

	protected := v1.Group("")
	protected.Use(auth.Middleware(cfg.JWTSecret))
	{
		protected.GET("/profile", getProfileHandler(dbPool))
		protected.POST("/uploads", createUploadHandler(dbPool, s3Presigner, metrics))
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
		slog.Info("user-service starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// 110 seconds: Spot interruption notice arrives 120 s before termination.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
	slog.Info("user-service stopped")
}

type registerRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

func registerHandler(cfg *config.UserService, pool *db.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		user, err := domain.CreateUser(c.Request.Context(), pool.Pool, req.Email, req.Password)
		if err != nil {
			slog.Error("failed to create user", "error", err)
			if isUniqueViolation(err) {
				c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		token, err := auth.GenerateToken(user.ID, user.Email, cfg.JWTSecret, 24*time.Hour)
		if err != nil {
			slog.Error("failed to generate token", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"user_id": user.ID,
			"email":   user.Email,
			"token":   token,
		})
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func loginHandler(cfg *config.UserService, pool *db.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		user, err := domain.GetUserByEmail(c.Request.Context(), pool.Pool, req.Email)
		if err != nil || user == nil || !domain.CheckPassword(user.PasswordHash, req.Password) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		token, err := auth.GenerateToken(user.ID, user.Email, cfg.JWTSecret, 24*time.Hour)
		if err != nil {
			slog.Error("failed to generate token", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"user_id": user.ID,
			"email":   user.Email,
			"token":   token,
		})
	}
}

func getProfileHandler(pool *db.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, _ := auth.ClaimsFromContext(c)
		user, err := domain.GetUserByEmail(c.Request.Context(), pool.Pool, claims.Email)
		if err != nil || user == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"user_id": user.ID,
			"email":   user.Email,
		})
	}
}

type uploadRequest struct {
	Filename    string `json:"filename" binding:"required"`
	ContentType string `json:"content_type"`
}

// createUploadHandler records an upload and returns a direct S3 PUT URL. The
// S3 ObjectCreated event enqueues analysis only after the image exists.
func createUploadHandler(pool *db.Pool, presigner *storage.S3Presigner, metrics *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, _ := auth.ClaimsFromContext(c)

		var req uploadRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			metrics.UploadRequestsTotal.WithLabelValues("rejected").Inc()
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		uploadID := uuid.New().String()
		s3Key := fmt.Sprintf("uploads/%s/%s", claims.UserID, uploadID)
		contentType := strings.TrimSpace(req.ContentType)
		if contentType == "" {
			contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(req.Filename)))
		}
		if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
			metrics.UploadRequestsTotal.WithLabelValues("rejected").Inc()
			c.JSON(http.StatusBadRequest, gin.H{"error": "content_type must be an image"})
			return
		}

		_, err := pool.Exec(c.Request.Context(),
			`INSERT INTO inbody_uploads (id, user_id, s3_key, status, idempotency_key)
			 VALUES ($1, $2, $3, 'pending', $4)`,
			uploadID, claims.UserID, s3Key, uploadID,
		)
		if err != nil {
			slog.Error("failed to create upload record", "error", err)
			metrics.UploadRequestsTotal.WithLabelValues("error").Inc()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		presigned, err := presigner.PresignUpload(c.Request.Context(), s3Key, contentType, 15*time.Minute)
		if err != nil {
			slog.Error("failed to presign upload", "upload_id", uploadID, "error", err)
			metrics.UploadRequestsTotal.WithLabelValues("error").Inc()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare upload"})
			return
		}

		slog.Info("upload URL created",
			"user_id", claims.UserID,
			"upload_id", uploadID,
		)
		metrics.UploadRequestsTotal.WithLabelValues("accepted").Inc()

		c.JSON(http.StatusCreated, gin.H{
			"upload_id":          uploadID,
			"s3_key":             s3Key,
			"status":             "awaiting_upload",
			"upload_url":         presigned.URL,
			"upload_method":      presigned.Method,
			"upload_headers":     gin.H{"Content-Type": contentType},
			"expires_in_seconds": 900,
		})
	}
}

var _ bbhttp.Pinger = (*db.Pool)(nil)
var _ bbhttp.Pinger = (*cache.Client)(nil)

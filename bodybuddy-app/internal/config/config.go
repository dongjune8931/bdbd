package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/kelseyhightower/envconfig"
)

// Common holds configuration shared across all services.
type Common struct {
	ServiceName string `envconfig:"SERVICE_NAME" required:"true"`
	LogLevel    string `envconfig:"LOG_LEVEL" default:"info"`

	DBHost     string `envconfig:"DB_HOST" required:"true"`
	DBPort     int    `envconfig:"DB_PORT" default:"5432"`
	DBUser     string `envconfig:"DB_USER" required:"true"`
	DBPassword string `envconfig:"DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"DB_NAME" required:"true"`
	DBSSLMode  string `envconfig:"DB_SSL_MODE" default:"disable"`

	RedisAddr     string `envconfig:"REDIS_ADDR" required:"true"`
	RedisPassword string `envconfig:"REDIS_PASSWORD" default:""`
	RedisTLSEnabled bool `envconfig:"REDIS_TLS_ENABLED" default:"false"`

	// SQSEndpoint is used to override the SQS endpoint for localstack.
	SQSEndpoint          string `envconfig:"SQS_ENDPOINT" default:""`
	AWSRegion            string `envconfig:"AWS_REGION" default:"ap-northeast-2"`
	AnalysisQueueURL     string `envconfig:"ANALYSIS_QUEUE_URL" required:"true"`
	NotificationQueueURL string `envconfig:"NOTIFICATION_QUEUE_URL" required:"true"`

	// S3Endpoint is used to override the S3 endpoint for localstack.
	S3Endpoint string `envconfig:"S3_ENDPOINT" default:""`
	S3Bucket   string `envconfig:"S3_BUCKET" required:"true"`

	JWTSecret string `envconfig:"JWT_SECRET" required:"true"`
}

// DSN returns a PostgreSQL connection string.
func (c *Common) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}

// UserService holds configuration for the user-service.
type UserService struct {
	Common
	HTTPPort int `envconfig:"HTTP_PORT" default:"8080"`
}

// ScoreService holds configuration for the score-service.
type ScoreService struct {
	Common
	HTTPPort int `envconfig:"HTTP_PORT" default:"8081"`
}

// AnalysisWorker holds configuration for the analysis-worker.
type AnalysisWorker struct {
	Common
	MetricsPort          int   `envconfig:"METRICS_PORT" default:"9090"`
	SQSVisibilityTimeout int32 `envconfig:"SQS_VISIBILITY_TIMEOUT" default:"90"`
	SQSMaxMessages       int32 `envconfig:"SQS_MAX_MESSAGES" default:"10"`
	SQSWaitTimeSeconds   int32 `envconfig:"SQS_WAIT_TIME_SECONDS" default:"20"`
	ScoreServiceURL      string `envconfig:"SCORE_SERVICE_URL" required:"true"`
}

// NotificationWorker holds configuration for the notification-worker.
type NotificationWorker struct {
	Common
	MetricsPort int    `envconfig:"METRICS_PORT" default:"9091"`
	SESRegion   string `envconfig:"SES_REGION" default:"ap-northeast-2"`
	SESEndpoint string `envconfig:"SES_ENDPOINT" default:""`  // localstack용
	FromEmail   string `envconfig:"FROM_EMAIL" required:"true"`
}

// MustLoadUserService loads UserService config or exits.
func MustLoadUserService() *UserService {
	var cfg UserService
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("failed to load user-service config", "error", err)
		os.Exit(1)
	}
	return &cfg
}

// MustLoadScoreService loads ScoreService config or exits.
func MustLoadScoreService() *ScoreService {
	var cfg ScoreService
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("failed to load score-service config", "error", err)
		os.Exit(1)
	}
	return &cfg
}

// MustLoadAnalysisWorker loads AnalysisWorker config or exits.
func MustLoadAnalysisWorker() *AnalysisWorker {
	var cfg AnalysisWorker
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("failed to load analysis-worker config", "error", err)
		os.Exit(1)
	}
	return &cfg
}

// MustLoadNotificationWorker loads NotificationWorker config or exits.
func MustLoadNotificationWorker() *NotificationWorker {
	var cfg NotificationWorker
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("failed to load notification-worker config", "error", err)
		os.Exit(1)
	}
	return &cfg
}

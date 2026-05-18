package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3ObjectDeletedDetail struct {
	Bucket struct {
		Name string `json:"name"`
	} `json:"bucket"`
	Object struct {
		Key string `json:"key"`
	} `json:"object"`
	Reason       string `json:"reason"`
	DeletionType string `json:"deletion-type"`
}

type recoveryResult struct {
	Bucket                string `json:"bucket"`
	Key                   string `json:"key"`
	Recovered             bool   `json:"recovered"`
	DeleteMarkerVersionID string `json:"delete_marker_version_id,omitempty"`
	Message               string `json:"message"`
	DurationMS            int64  `json:"duration_ms"`
}

type handler struct {
	s3              *s3.Client
	cloudwatch      *cloudwatch.Client
	defaultBucket   string
	metricNamespace string
	logger          *slog.Logger
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		logger.Error("loading aws config", "error", err)
		os.Exit(1)
	}

	h := handler{
		s3:              s3.NewFromConfig(cfg),
		cloudwatch:      cloudwatch.NewFromConfig(cfg),
		defaultBucket:   os.Getenv("BUCKET_NAME"),
		metricNamespace: getenv("METRIC_NAMESPACE", "BodyBuddy/DR"),
		logger:          logger.With("component", "s3-auto-recovery"),
	}

	lambda.Start(h.handle)
}

func (h handler) handle(ctx context.Context, event events.CloudWatchEvent) (recoveryResult, error) {
	started := time.Now()

	result, err := h.recoverDeleteMarker(ctx, event)
	result.DurationMS = time.Since(started).Milliseconds()

	metricErr := h.putMetrics(ctx, result, err)
	if metricErr != nil {
		h.logger.Warn("putting cloudwatch metrics", "error", metricErr)
	}

	if err != nil {
		h.logger.Error("s3 auto recovery failed", "error", err, "bucket", result.Bucket, "key", result.Key)
		return result, err
	}

	h.logger.Info("s3 auto recovery finished",
		"bucket", result.Bucket,
		"key", result.Key,
		"recovered", result.Recovered,
		"delete_marker_version_id", result.DeleteMarkerVersionID,
		"duration_ms", result.DurationMS,
	)
	return result, nil
}

func (h handler) recoverDeleteMarker(ctx context.Context, event events.CloudWatchEvent) (recoveryResult, error) {
	var detail s3ObjectDeletedDetail
	if err := json.Unmarshal(event.Detail, &detail); err != nil {
		return recoveryResult{Message: "invalid event detail"}, fmt.Errorf("unmarshal event detail: %w", err)
	}

	bucket := detail.Bucket.Name
	if bucket == "" {
		bucket = h.defaultBucket
	}
	key := decodeS3Key(detail.Object.Key)

	result := recoveryResult{
		Bucket: bucket,
		Key:    key,
	}

	if bucket == "" || key == "" {
		result.Message = "missing bucket or key"
		return result, fmt.Errorf("missing bucket or key in event: bucket=%q key=%q", bucket, key)
	}

	versions, err := h.s3.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
		Bucket:  aws.String(bucket),
		Prefix:  aws.String(key),
		MaxKeys: aws.Int32(20),
	})
	if err != nil {
		result.Message = "failed to list object versions"
		return result, fmt.Errorf("list object versions for s3://%s/%s: %w", bucket, key, err)
	}

	var latestDeleteMarkerVersionID *string
	for _, marker := range versions.DeleteMarkers {
		if aws.ToString(marker.Key) == key && aws.ToBool(marker.IsLatest) {
			latestDeleteMarkerVersionID = marker.VersionId
			break
		}
	}

	if latestDeleteMarkerVersionID == nil || aws.ToString(latestDeleteMarkerVersionID) == "" {
		result.Message = "latest version is not a delete marker; no recovery required"
		return result, nil
	}

	_, err = h.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:                    aws.String(bucket),
		Key:                       aws.String(key),
		VersionId:                 latestDeleteMarkerVersionID,
		BypassGovernanceRetention: aws.Bool(true),
	})
	if err != nil {
		result.Message = "failed to remove delete marker"
		return result, fmt.Errorf("remove delete marker for s3://%s/%s version %s: %w", bucket, key, aws.ToString(latestDeleteMarkerVersionID), err)
	}

	result.Recovered = true
	result.DeleteMarkerVersionID = aws.ToString(latestDeleteMarkerVersionID)
	result.Message = "delete marker removed; previous object version is current again"
	return result, nil
}

func (h handler) putMetrics(ctx context.Context, result recoveryResult, recoveryErr error) error {
	valueRecovered := 0.0
	valueFailure := 0.0
	if result.Recovered {
		valueRecovered = 1
	}
	if recoveryErr != nil {
		valueFailure = 1
	}

	_, err := h.cloudwatch.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: aws.String(h.metricNamespace),
		MetricData: []cwtypes.MetricDatum{
			{
				MetricName: aws.String("S3AutoRecoveryRecoveredObjects"),
				Unit:       cwtypes.StandardUnitCount,
				Value:      aws.Float64(valueRecovered),
			},
			{
				MetricName: aws.String("S3AutoRecoveryFailures"),
				Unit:       cwtypes.StandardUnitCount,
				Value:      aws.Float64(valueFailure),
			},
			{
				MetricName: aws.String("S3AutoRecoveryDurationMilliseconds"),
				Unit:       cwtypes.StandardUnitMilliseconds,
				Value:      aws.Float64(float64(result.DurationMS)),
			},
		},
	})
	return err
}

func decodeS3Key(key string) string {
	if key == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(strings.ReplaceAll(key, "+", "%20"))
	if err != nil {
		return key
	}
	return decoded
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

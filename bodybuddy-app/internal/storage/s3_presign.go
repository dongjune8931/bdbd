package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Presigner creates short-lived direct upload URLs for the protected bucket.
type S3Presigner struct {
	client *s3.PresignClient
	bucket string
}

// PresignedUpload contains the client-facing parts of a signed PUT request.
type PresignedUpload struct {
	URL    string
	Method string
}

// NewS3Presigner creates a presigner. endpoint is only used for local development.
func NewS3Presigner(ctx context.Context, region, endpoint, bucket string) (*S3Presigner, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config for S3 presigning: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		}
	})
	return &S3Presigner{client: s3.NewPresignClient(client), bucket: bucket}, nil
}

// PresignUpload returns a PUT request that requires the declared image content type.
func (p *S3Presigner) PresignUpload(ctx context.Context, key, contentType string, expires time.Duration) (*PresignedUpload, error) {
	request, err := p.client.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(p.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return nil, fmt.Errorf("presigning S3 upload: %w", err)
	}
	return &PresignedUpload{URL: request.URL, Method: request.Method}, nil
}

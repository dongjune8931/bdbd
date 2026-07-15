package inference

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3ImageStore loads protected upload objects using the inference-service IRSA.
type S3ImageStore struct {
	client   *s3.Client
	bucket   string
	maxBytes int64
}

// NewS3ImageStore creates an S3 image loader. endpoint is only set for LocalStack.
func NewS3ImageStore(ctx context.Context, region, endpoint, bucket string, maxBytes int64) (*S3ImageStore, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config for S3: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		}
	})
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}

	return &S3ImageStore{client: client, bucket: bucket, maxBytes: maxBytes}, nil
}

// Load returns one image object while enforcing content type and size limits.
func (s *S3ImageStore) Load(ctx context.Context, key string) ([]byte, string, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", fmt.Errorf("getting S3 object %q: %w", key, err)
	}
	defer output.Body.Close()

	contentType := aws.ToString(output.ContentType)
	if contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return nil, contentType, fmt.Errorf("S3 object %q is not an image: %s", key, contentType)
	}

	image, err := io.ReadAll(io.LimitReader(output.Body, s.maxBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("reading S3 object %q: %w", key, err)
	}
	if int64(len(image)) > s.maxBytes {
		return nil, contentType, fmt.Errorf("S3 object %q exceeds %d bytes", key, s.maxBytes)
	}
	if len(image) == 0 {
		return nil, contentType, fmt.Errorf("S3 object %q is empty", key)
	}
	return image, contentType, nil
}

package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bodybuddy/app/internal/domain"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Request is sent by analysis-worker to the inference service.
type Request struct {
	UserID   string `json:"user_id"`
	UploadID string `json:"upload_id"`
	S3Key    string `json:"s3_key"`
}

// Response is returned by the inference service after mock OCR / GPU simulation.
type Response struct {
	OCR            domain.OCRResult      `json:"ocr"`
	ScoreBreakdown domain.ScoreBreakdown `json:"score_breakdown"`
	Accelerator    string                `json:"accelerator"`
	ModelVersion   string                `json:"model_version"`
	DurationMs     int64                 `json:"duration_ms"`
}

// Client wraps access to the inference service.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient builds an inference client with OpenTelemetry HTTP instrumentation.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// Analyze sends an inference request to the remote service.
func (c *Client) Analyze(ctx context.Context, reqPayload Request) (*Response, error) {
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("encoding inference request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/inference", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating inference request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling inference-service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("inference-service returned status %d", resp.StatusCode)
	}

	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding inference response: %w", err)
	}
	return &out, nil
}

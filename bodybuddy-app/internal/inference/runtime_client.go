package inference

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// OCRLine is one text line recognized by the OCR model.
type OCRLine struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

// RuntimeResponse is returned by the Python OCR model runtime.
type RuntimeResponse struct {
	Lines        []OCRLine `json:"lines"`
	Accelerator  string    `json:"accelerator"`
	ModelVersion string    `json:"model_version"`
	InferenceMs  int64     `json:"inference_ms"`
}

// RuntimeClient calls the OCR sidecar over localhost HTTP.
type RuntimeClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRuntimeClient creates an instrumented OCR runtime client.
func NewRuntimeClient(baseURL string, timeout time.Duration) *RuntimeClient {
	return &RuntimeClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// Analyze executes OCR for an encoded image.
func (c *RuntimeClient) Analyze(ctx context.Context, image []byte) (*RuntimeResponse, error) {
	payload := struct {
		ImageBase64 string `json:"image_base64"`
	}{ImageBase64: base64.StdEncoding.EncodeToString(image)}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding OCR runtime request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/ocr", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating OCR runtime request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling OCR runtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OCR runtime returned status %d", resp.StatusCode)
	}

	var out RuntimeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding OCR runtime response: %w", err)
	}
	return &out, nil
}

// Ready reports whether the sidecar model has finished loading.
func (c *RuntimeClient) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/readyz", nil)
	if err != nil {
		return fmt.Errorf("creating OCR readiness request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling OCR readiness endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OCR runtime readiness returned status %d", resp.StatusCode)
	}
	return nil
}

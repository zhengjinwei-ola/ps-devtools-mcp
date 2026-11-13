package testapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/olaola-chat/ps-devtools-mcp/internal/redact"
)

const maxRequestBodyBytes = 64 << 10

type Service struct {
	client           *http.Client
	maxResponseBytes int64
	logger           *log.Logger
}

func NewService(client *http.Client, maxResponseBytes int64, logger *log.Logger) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("http client is required")
	}
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}
	return &Service{client: client, maxResponseBytes: maxResponseBytes, logger: logger}, nil
}

func (s *Service) Call(ctx context.Context, input CallInput) (CallOutput, error) {
	parsed, err := url.Parse(strings.TrimSpace(input.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return CallOutput{}, fmt.Errorf("url must be an absolute HTTP or HTTPS URL")
	}
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !validMethod(method) {
		return CallOutput{}, fmt.Errorf("invalid HTTP method %q", input.Method)
	}
	query := parsed.Query()
	for key, value := range input.Query {
		if len(value) > 2048 {
			return CallOutput{}, fmt.Errorf("query value for %q is too long", key)
		}
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	var body io.Reader
	if input.Body != nil {
		var encoded []byte
		if raw, ok := input.Body.(string); ok {
			encoded = []byte(raw)
		} else {
			encoded, err = json.Marshal(input.Body)
		}
		if err != nil {
			return CallOutput{}, fmt.Errorf("encode request body: %w", err)
		}
		if len(encoded) > maxRequestBodyBytes {
			return CallOutput{}, fmt.Errorf("request body exceeds %d bytes", maxRequestBodyBytes)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return CallOutput{}, fmt.Errorf("create read-only request: %w", err)
	}
	for key, value := range input.Headers {
		req.Header.Set(key, value)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	if input.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	started := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		return CallOutput{}, fmt.Errorf("call read-only endpoint: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, s.maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return CallOutput{}, fmt.Errorf("read endpoint response: %w", err)
	}
	truncated := int64(len(data)) > s.maxResponseBytes
	if truncated {
		data = data[:s.maxResponseBytes]
	}
	output := CallOutput{
		URL: parsed.String(), Method: method, StatusCode: resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"), ElapsedMS: time.Since(started).Milliseconds(), Truncated: truncated,
	}
	output.Body = redact.Response(data, output.ContentType, truncated)
	if s.logger != nil {
		s.logger.Printf("tool=call_test_readonly_api url=%q method=%s status=%d bytes=%d truncated=%t duration_ms=%d",
			parsed.String(), method, resp.StatusCode, len(data), truncated, output.ElapsedMS)
	}
	return output, nil
}

func validMethod(method string) bool {
	for _, char := range method {
		if char <= 32 || char >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", char) {
			return false
		}
	}
	return method != ""
}

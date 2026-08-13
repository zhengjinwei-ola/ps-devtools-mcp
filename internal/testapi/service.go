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
	"regexp"
	"strings"
	"time"
)

const maxRequestBodyBytes = 64 << 10

var textSecrets = regexp.MustCompile(`(?i)((?:authorization|access_?token|refresh_?token|password|passwd|secret|cookie|session)\s*[:=]\s*)[^\s,;]+`)

type Service struct {
	endpoints        map[string]Endpoint
	client           *http.Client
	maxResponseBytes int64
	logger           *log.Logger
}

type Unavailable struct{}

func (Unavailable) Call(context.Context, CallInput) (CallOutput, error) {
	return CallOutput{}, fmt.Errorf("read-only API allowlist is not configured; set PS_MCP_READONLY_API_CONFIG")
}

func NewService(config Config, client *http.Client, logger *log.Logger) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("http client is required")
	}
	endpoints := make(map[string]Endpoint, len(config.Endpoints))
	for _, endpoint := range config.Endpoints {
		endpoints[endpoint.Name] = endpoint
	}
	return &Service{endpoints: endpoints, client: client, maxResponseBytes: config.MaxResponseBytes, logger: logger}, nil
}

func (s *Service) Call(ctx context.Context, input CallInput) (CallOutput, error) {
	endpoint, ok := s.endpoints[input.Endpoint]
	if !ok {
		return CallOutput{}, fmt.Errorf("unknown read-only endpoint %q", input.Endpoint)
	}
	if err := validateKeys(input.Query, endpoint.AllowedQueryKeys, "query"); err != nil {
		return CallOutput{}, err
	}
	if err := validateAnyKeys(input.Body, endpoint.AllowedBodyFields); err != nil {
		return CallOutput{}, err
	}
	parsed, _ := url.Parse(endpoint.URL)
	query := parsed.Query()
	for key, value := range input.Query {
		if len(value) > 2048 {
			return CallOutput{}, fmt.Errorf("query value for %q is too long", key)
		}
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	var body io.Reader
	if endpoint.Method == http.MethodPost {
		encoded, err := json.Marshal(input.Body)
		if err != nil {
			return CallOutput{}, fmt.Errorf("encode request body: %w", err)
		}
		if len(encoded) > maxRequestBodyBytes {
			return CallOutput{}, fmt.Errorf("request body exceeds %d bytes", maxRequestBodyBytes)
		}
		body = bytes.NewReader(encoded)
	} else if len(input.Body) > 0 {
		return CallOutput{}, fmt.Errorf("endpoint %q does not accept a body", input.Endpoint)
	}
	req, err := http.NewRequestWithContext(ctx, endpoint.Method, parsed.String(), body)
	if err != nil {
		return CallOutput{}, fmt.Errorf("create read-only request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if endpoint.Method == http.MethodPost {
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
		Endpoint: input.Endpoint, StatusCode: resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"), ElapsedMS: time.Since(started).Milliseconds(), Truncated: truncated,
	}
	if strings.Contains(strings.ToLower(output.ContentType), "json") && !truncated {
		var decoded any
		if json.Unmarshal(data, &decoded) == nil {
			output.Body = redactJSON(decoded)
		} else {
			output.Body = textSecrets.ReplaceAllString(string(data), `${1}[REDACTED]`)
		}
	} else {
		output.Body = textSecrets.ReplaceAllString(string(data), `${1}[REDACTED]`)
	}
	if s.logger != nil {
		s.logger.Printf("tool=call_test_readonly_api endpoint=%q method=%s status=%d bytes=%d truncated=%t duration_ms=%d",
			input.Endpoint, endpoint.Method, resp.StatusCode, len(data), truncated, output.ElapsedMS)
	}
	return output, nil
}

func validateKeys(values map[string]string, allowed []string, kind string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range values {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("%s field %q is not allowlisted", kind, key)
		}
	}
	return nil
}

func validateAnyKeys(values map[string]any, allowed []string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range values {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("body field %q is not allowlisted", key)
		}
	}
	return nil
}

func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "cookie") || strings.Contains(lower, "session") || strings.Contains(lower, "authorization") || strings.Contains(lower, "mobile") || strings.Contains(lower, "phone") {
				typed[key] = "[REDACTED]"
			} else {
				typed[key] = redactJSON(item)
			}
		}
	case []any:
		for index := range typed {
			typed[index] = redactJSON(typed[index])
		}
	}
	return value
}

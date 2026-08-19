package testbot

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxRequestBodyBytes = 64 << 10

const legacyFirewallSignSuffix = "!rilegoule#"

var legacyFirewallSignFields = []string{"_abi", "_index", "_ipv", "_model", "_platform", "_timestamp", "format", "package"}

var textSecrets = regexp.MustCompile(`(?i)((?:authorization|user-token|access_?token|refresh_?token|password|passwd|secret|cookie|session)["']?\s*[:=]\s*["']?)[^"'\s,;]+`)

type Credentials struct {
	Area     string
	Mobile   string
	Password string
}

type Service struct {
	config    Config
	client    *http.Client
	logger    *log.Logger
	endpoints map[string]Endpoint
	creds     Credentials

	loginMu           sync.Mutex
	token             string
	signMu            sync.Mutex
	lastSignTimestamp int64
}

type Unavailable struct{}

func (Unavailable) Call(context.Context, CallInput) (CallOutput, error) {
	return CallOutput{}, fmt.Errorf("testbot is not configured; set PSL_MCP_TESTBOT_CONFIG and testbot credentials")
}
func (Unavailable) List(context.Context, ListInput) (ListOutput, error) {
	return ListOutput{}, fmt.Errorf("testbot is not configured; set PSL_MCP_TESTBOT_CONFIG and testbot credentials")
}

func NewService(config Config, credentials Credentials, client *http.Client, logger *log.Logger) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("http client is required")
	}
	credentials.Area = strings.TrimSpace(credentials.Area)
	credentials.Mobile = strings.TrimSpace(credentials.Mobile)
	if credentials.Area == "" || credentials.Mobile == "" || credentials.Password == "" {
		return nil, fmt.Errorf("testbot area, mobile and password are required")
	}
	endpoints := make(map[string]Endpoint, len(config.Endpoints))
	for _, endpoint := range config.Endpoints {
		endpoints[endpoint.Name] = endpoint
	}
	return &Service{config: config, client: client, logger: logger, endpoints: endpoints, creds: credentials}, nil
}

func (s *Service) List(context.Context, ListInput) (ListOutput, error) {
	output := ListOutput{Endpoints: make([]EndpointInfo, 0, len(s.endpoints))}
	for _, endpoint := range s.endpoints {
		output.Endpoints = append(output.Endpoints, EndpointInfo{Name: endpoint.Name, Method: endpoint.Method, SideEffect: endpoint.SideEffect})
	}
	sort.Slice(output.Endpoints, func(i, j int) bool { return output.Endpoints[i].Name < output.Endpoints[j].Name })
	return output, nil
}

func (s *Service) Call(ctx context.Context, input CallInput) (CallOutput, error) {
	endpoint, ok := s.endpoints[input.Endpoint]
	if !ok {
		return CallOutput{}, fmt.Errorf("unknown testbot endpoint %q", input.Endpoint)
	}
	if endpoint.SideEffect && !input.ConfirmSideEffect {
		return CallOutput{}, fmt.Errorf("endpoint %q has side effects; confirm_side_effect must be true", input.Endpoint)
	}
	if err := validateKeys(input.Query, endpoint.AllowedQueryKeys, "query"); err != nil {
		return CallOutput{}, err
	}
	if err := validateKeys(input.Body, endpoint.AllowedBodyFields, "body"); err != nil {
		return CallOutput{}, err
	}
	token, err := s.login(ctx, false)
	if err != nil {
		return CallOutput{}, err
	}
	output, err := s.callWithToken(ctx, endpoint, input, token)
	if err != nil {
		return CallOutput{}, err
	}
	if output.StatusCode != http.StatusUnauthorized && output.StatusCode != http.StatusForbidden {
		return output, nil
	}
	token, err = s.login(ctx, true)
	if err != nil {
		return CallOutput{}, err
	}
	return s.callWithToken(ctx, endpoint, input, token)
}

func (s *Service) login(ctx context.Context, force bool) (string, error) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.token != "" && !force {
		return s.token, nil
	}
	form := url.Values{"area": {s.creds.Area}, "mobile": {s.creds.Mobile}, "password": {sha1Hex(s.creds.Password)}}
	loginURL, err := s.buildURL(s.config.LoginPath, s.config.LoginQuery)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create testbot login request: %w", err)
	}
	applyHeaders(req, s.config.Headers)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	started := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("testbot login: %w", err)
	}
	defer resp.Body.Close()
	data, err := readLimited(resp.Body, s.config.MaxResponseBytes)
	if err != nil {
		return "", fmt.Errorf("read testbot login response: %w", err)
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Token string `json:"token"`
			UID   uint32 `json:"uid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("decode testbot login response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !payload.Success || payload.Data.Token == "" {
		return "", fmt.Errorf("testbot login failed with status %d", resp.StatusCode)
	}
	s.token = payload.Data.Token
	if s.logger != nil {
		s.logger.Printf("tool=testbot action=login uid=%d token_fingerprint=%s duration_ms=%d", payload.Data.UID, tokenFingerprint(s.token), time.Since(started).Milliseconds())
	}
	return s.token, nil
}

func (s *Service) callWithToken(ctx context.Context, endpoint Endpoint, input CallInput, token string) (CallOutput, error) {
	requestID := newRequestID()
	query := input.Query
	if endpoint.LegacyFirewallSign {
		query = s.legacySignedQuery(input.Query)
	}
	target, err := s.buildURL(endpoint.Path, query)
	if err != nil {
		return CallOutput{}, err
	}
	body, contentType, err := encodeBody(endpoint, input.Body)
	if err != nil {
		return CallOutput{}, err
	}
	req, err := http.NewRequestWithContext(ctx, endpoint.Method, target, body)
	if err != nil {
		return CallOutput{}, fmt.Errorf("create testbot request: %w", err)
	}
	applyHeaders(req, s.config.Headers)
	applyHeaders(req, endpoint.Headers)
	req.Header.Set("User-Token", token)
	req.Header.Set("X-TestBot-Request-ID", requestID)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	started := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		return CallOutput{}, fmt.Errorf("call testbot endpoint: %w", err)
	}
	defer resp.Body.Close()
	data, truncated, err := readLimitedWithTruncation(resp.Body, s.config.MaxResponseBytes)
	if err != nil {
		return CallOutput{}, fmt.Errorf("read testbot endpoint response: %w", err)
	}
	output := CallOutput{Endpoint: endpoint.Name, RequestID: requestID, StatusCode: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), ElapsedMS: time.Since(started).Milliseconds(), Truncated: truncated}
	output.Body = redactResponse(data, output.ContentType, truncated)
	if s.logger != nil {
		s.logger.Printf("tool=testbot action=call endpoint=%q method=%s side_effect=%t status=%d request_id=%q token_fingerprint=%s bytes=%d truncated=%t duration_ms=%d", endpoint.Name, endpoint.Method, endpoint.SideEffect, resp.StatusCode, requestID, tokenFingerprint(token), len(data), truncated, output.ElapsedMS)
	}
	return output, nil
}

// legacySignedQuery reproduces the old Partystar client signature. The timestamp
// is monotonic because the legacy firewall treats an identical same-second sign
// as a replay, even when the endpoint or request body differs.
func (s *Service) legacySignedQuery(input map[string]string) map[string]string {
	query := make(map[string]string, len(s.config.LoginQuery)+len(input)+2)
	for key, value := range input {
		query[key] = value
	}
	// Configured client identity is authoritative; callers cannot override the
	// fields covered by the legacy signature through an endpoint allowlist.
	for key, value := range s.config.LoginQuery {
		query[key] = value
	}

	s.signMu.Lock()
	timestamp := time.Now().Unix()
	if timestamp <= s.lastSignTimestamp {
		timestamp = s.lastSignTimestamp + 1
	}
	s.lastSignTimestamp = timestamp
	s.signMu.Unlock()
	query["_timestamp"] = fmt.Sprintf("%d", timestamp)

	query["_sign"] = legacyFirewallSign(query)
	return query
}

func legacyFirewallSign(query map[string]string) string {
	parts := make([]string, 0, len(legacyFirewallSignFields))
	for _, key := range legacyFirewallSignFields {
		value, exists := query[key]
		if key == "format" && !exists {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + legacyFirewallSignSuffix))
	return hex.EncodeToString(sum[:])
}

func (s *Service) buildURL(path string, query map[string]string) (string, error) {
	parsed, err := url.Parse(s.config.BaseURL + path)
	if err != nil {
		return "", fmt.Errorf("build testbot URL: %w", err)
	}
	values := parsed.Query()
	for key, value := range query {
		if len(value) > 2048 {
			return "", fmt.Errorf("query value for %q is too long", key)
		}
		values.Set(key, value)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func encodeBody(endpoint Endpoint, body map[string]string) (io.Reader, string, error) {
	if endpoint.Method != http.MethodPost {
		if len(body) > 0 {
			return nil, "", fmt.Errorf("endpoint %q does not accept a body", endpoint.Name)
		}
		return nil, "", nil
	}
	if endpoint.Encoding == "form" {
		values := url.Values{}
		for key, value := range body {
			values.Set(key, value)
		}
		encoded := values.Encode()
		if len(encoded) > maxRequestBodyBytes {
			return nil, "", fmt.Errorf("request body exceeds %d bytes", maxRequestBodyBytes)
		}
		return strings.NewReader(encoded), "application/x-www-form-urlencoded", nil
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("encode request body: %w", err)
	}
	if len(encoded) > maxRequestBodyBytes {
		return nil, "", fmt.Errorf("request body exceeds %d bytes", maxRequestBodyBytes)
	}
	return bytes.NewReader(encoded), "application/json", nil
}

func validateKeys(values map[string]string, allowed []string, label string) error {
	allowset := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowset[key] = struct{}{}
	}
	for key := range values {
		if _, ok := allowset[key]; !ok {
			return fmt.Errorf("%s field %q is not allowlisted", label, key)
		}
	}
	return nil
}

func applyHeaders(req *http.Request, headers map[string]string) {
	for name, value := range headers {
		req.Header.Set(name, value)
	}
}

func sha1Hex(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:4])
}

func newRequestID() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err == nil {
		return "tb-" + hex.EncodeToString(random)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return "tb-" + hex.EncodeToString(sum[:8])
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, truncated, err := readLimitedWithTruncation(reader, limit)
	if truncated {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, err
}

func readLimitedWithTruncation(reader io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, false, err
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	return data, truncated, nil
}

func redactResponse(data []byte, contentType string, truncated bool) any {
	if strings.Contains(strings.ToLower(contentType), "json") && !truncated {
		var value any
		if json.Unmarshal(data, &value) == nil {
			return redactJSON(value)
		}
	}
	return textSecrets.ReplaceAllString(string(data), "${1}[REDACTED]")
}

func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "cookie") || strings.Contains(lower, "session") || strings.Contains(lower, "mobile") || strings.Contains(lower, "phone") {
				typed[key] = "[REDACTED]"
				continue
			}
			typed[key] = redactJSON(item)
		}
	case []any:
		for index := range typed {
			typed[index] = redactJSON(typed[index])
		}
	}
	return value
}

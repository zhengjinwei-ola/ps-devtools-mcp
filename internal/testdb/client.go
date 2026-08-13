package testdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type HTTPClient struct {
	endpoint string
	client   *http.Client
	maxBody  int64
}

type queryRequest struct {
	Statement string `json:"statement"`
	Engine    int    `json:"engine"`
}

type queryResponse struct {
	Common struct {
		Code int32  `json:"code"`
		Msg  string `json:"msg"`
	} `json:"common"`
	ResultJSON      string        `json:"result_json"`
	ResultJSONCamel string        `json:"resultJson"`
	RowCount        flexibleInt64 `json:"row_count"`
	RowCountCamel   flexibleInt64 `json:"rowCount"`
	ElapsedMS       flexibleInt64 `json:"elapsed_ms"`
	ElapsedMSCamel  flexibleInt64 `json:"elapsedMs"`
}

type flexibleInt64 int64

func (value *flexibleInt64) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("parse integer %q: %w", raw, err)
	}
	*value = flexibleInt64(parsed)
	return nil
}

func NewHTTPClient(endpoint string, client *http.Client, maxBody int64) (*HTTPClient, error) {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid test query endpoint %q", endpoint)
	}
	if client == nil {
		return nil, fmt.Errorf("http client is required")
	}
	if maxBody <= 0 {
		return nil, fmt.Errorf("max response body must be positive")
	}
	return &HTTPClient{endpoint: endpoint, client: client, maxBody: maxBody}, nil
}

func (c *HTTPClient) Query(ctx context.Context, input QueryInput) (QueryOutput, error) {
	body, err := json.Marshal(queryRequest{Statement: input.Statement, Engine: input.Engine})
	if err != nil {
		return QueryOutput{}, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return QueryOutput{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return QueryOutput{}, fmt.Errorf("call test query endpoint: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, c.maxBody+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return QueryOutput{}, fmt.Errorf("read response: %w", err)
	}
	if int64(len(responseBody)) > c.maxBody {
		return QueryOutput{}, fmt.Errorf("response exceeds %d bytes", c.maxBody)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return QueryOutput{}, fmt.Errorf("test query endpoint returned HTTP %d: %s", resp.StatusCode, truncate(responseBody, 512))
	}

	var decoded queryResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return QueryOutput{}, fmt.Errorf("decode response: %w", err)
	}
	if decoded.Common.Code != 0 {
		return QueryOutput{}, fmt.Errorf("test query rejected request: code=%d msg=%s", decoded.Common.Code, decoded.Common.Msg)
	}
	if decoded.ResultJSON == "" {
		decoded.ResultJSON = decoded.ResultJSONCamel
	}
	if decoded.RowCount == 0 {
		decoded.RowCount = decoded.RowCountCamel
	}
	if decoded.ElapsedMS == 0 {
		decoded.ElapsedMS = decoded.ElapsedMSCamel
	}
	if !json.Valid([]byte(decoded.ResultJSON)) {
		return QueryOutput{}, fmt.Errorf("test query returned invalid result_json")
	}
	return QueryOutput{
		Engine: input.Engine, ResultJSON: decoded.ResultJSON,
		RowCount: int64(decoded.RowCount), ElapsedMS: int64(decoded.ElapsedMS),
	}, nil
}

func truncate(value []byte, max int) string {
	if len(value) <= max {
		return string(value)
	}
	return string(value[:max]) + "..."
}

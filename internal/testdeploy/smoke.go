package testdeploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	defaultSmokeTimeout = 5 * time.Second
	minSmokeTimeout     = 100 * time.Millisecond
	maxSmokeTimeout     = 30 * time.Second
)

// SmokeEndpoint 是一个服务部署后的健康探针配置；expect_status 为空时默认
// 接受 2xx-3xx。未配置的服务完全跳过冒烟检查，部署行为不变。
type SmokeEndpoint struct {
	URL          string `json:"url"`
	ExpectStatus []int  `json:"expect_status,omitempty"`
	TimeoutMS    int    `json:"timeout_ms,omitempty"`
}

type SmokeConfig struct {
	Services map[string]SmokeEndpoint `json:"services"`
}

// SmokeSummary 记录一次部署后健康探针的结果，随部署状态与 Slack 通知暴露
// “部署成功但服务不健康”的情况；探针失败不触发回滚，仅改变终态。
type SmokeSummary struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code,omitempty"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	ElapsedMS  int64  `json:"elapsed_ms"`
}

type SmokeChecker struct {
	services map[string]SmokeEndpoint
	client   *http.Client
}

// LoadSmokeConfig 读取部署冒烟检查配置，URL 必须是绝对 HTTP(S) 地址。
func LoadSmokeConfig(path string) (*SmokeChecker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read deploy smoke config: %w", err)
	}
	var config SmokeConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode deploy smoke config: %w", err)
	}
	if config.Services == nil {
		config.Services = map[string]SmokeEndpoint{}
	}
	for service, endpoint := range config.Services {
		parsed, err := url.Parse(endpoint.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("smoke endpoint for %q must be an absolute URL: %q", service, endpoint.URL)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("smoke endpoint for %q must use http or https", service)
		}
		if endpoint.TimeoutMS != 0 &&
			(time.Duration(endpoint.TimeoutMS)*time.Millisecond < minSmokeTimeout || time.Duration(endpoint.TimeoutMS)*time.Millisecond > maxSmokeTimeout) {
			return nil, fmt.Errorf("smoke timeout for %q must be between %d and %d ms", service, minSmokeTimeout.Milliseconds(), maxSmokeTimeout.Milliseconds())
		}
		for _, status := range endpoint.ExpectStatus {
			if status < 100 || status > 599 {
				return nil, fmt.Errorf("smoke expect_status %d for %q is not a valid HTTP status", status, service)
			}
		}
	}
	return &SmokeChecker{services: config.Services, client: &http.Client{Timeout: maxSmokeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Check 探测服务的健康端点。第二个返回值表示该服务是否配置了探针；
// 未配置时调用方应跳过（部署状态不受影响）。
func (c *SmokeChecker) Check(ctx context.Context, service string) (SmokeSummary, bool) {
	endpoint, ok := c.services[service]
	if !ok {
		return SmokeSummary{}, false
	}
	timeout := defaultSmokeTimeout
	if endpoint.TimeoutMS != 0 {
		timeout = time.Duration(endpoint.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	summary := SmokeSummary{URL: endpoint.URL}
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URL, nil)
	if err != nil {
		summary.Error = err.Error()
		return summary, true
	}
	response, err := c.client.Do(request)
	summary.ElapsedMS = time.Since(started).Milliseconds()
	if err != nil {
		summary.Error = err.Error()
		return summary, true
	}
	defer response.Body.Close()
	summary.StatusCode = response.StatusCode
	summary.OK = smokeStatusExpected(endpoint.ExpectStatus, response.StatusCode)
	if !summary.OK {
		summary.Error = fmt.Sprintf("unexpected status %d", response.StatusCode)
	}
	return summary, true
}

func smokeStatusExpected(expected []int, status int) bool {
	if len(expected) == 0 {
		return status >= 200 && status < 400
	}
	for _, candidate := range expected {
		if candidate == status {
			return true
		}
	}
	return false
}

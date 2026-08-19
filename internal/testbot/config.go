package testbot

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const defaultMaxResponseBytes = 512 << 10

type Endpoint struct {
	Name               string            `json:"name"`
	Path               string            `json:"path"`
	Method             string            `json:"method"`
	Encoding           string            `json:"encoding,omitempty"`
	LegacyFirewallSign bool              `json:"legacy_firewall_signing,omitempty"`
	AllowedQueryKeys   []string          `json:"allowed_query_keys,omitempty"`
	AllowedBodyFields  []string          `json:"allowed_body_fields,omitempty"`
	Headers            map[string]string `json:"headers,omitempty"`
	SideEffect         bool              `json:"side_effect,omitempty"`
}

type Config struct {
	BaseURL          string            `json:"base_url"`
	LoginPath        string            `json:"login_path"`
	LoginQuery       map[string]string `json:"login_query,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Endpoints        []Endpoint        `json:"endpoints"`
	MaxResponseBytes int64             `json:"max_response_bytes,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read testbot config: %w", err)
	}
	var config Config
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode testbot config: %w", err)
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Config{}, fmt.Errorf("testbot base_url is invalid")
	}
	if err := validatePath(config.LoginPath, "login_path"); err != nil {
		return Config{}, err
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	if config.MaxResponseBytes < 1024 || config.MaxResponseBytes > 2<<20 {
		return Config{}, fmt.Errorf("max_response_bytes must be between 1024 and 2097152")
	}
	if len(config.Endpoints) == 0 {
		return Config{}, fmt.Errorf("at least one testbot endpoint is required")
	}
	seen := make(map[string]struct{}, len(config.Endpoints))
	for index := range config.Endpoints {
		endpoint := &config.Endpoints[index]
		endpoint.Name = strings.TrimSpace(endpoint.Name)
		endpoint.Method = strings.ToUpper(strings.TrimSpace(endpoint.Method))
		endpoint.Encoding = strings.ToLower(strings.TrimSpace(endpoint.Encoding))
		if endpoint.Encoding == "" {
			endpoint.Encoding = "json"
		}
		if endpoint.Name == "" || strings.ContainsAny(endpoint.Name, " /\\") {
			return Config{}, fmt.Errorf("endpoint name %q is invalid", endpoint.Name)
		}
		if _, duplicate := seen[endpoint.Name]; duplicate {
			return Config{}, fmt.Errorf("endpoint name %q is duplicated", endpoint.Name)
		}
		seen[endpoint.Name] = struct{}{}
		if err := validatePath(endpoint.Path, "endpoint path"); err != nil {
			return Config{}, fmt.Errorf("endpoint %q: %w", endpoint.Name, err)
		}
		switch endpoint.Method {
		case "GET", "HEAD":
			if len(endpoint.AllowedBodyFields) > 0 {
				return Config{}, fmt.Errorf("endpoint %q cannot allow a body for %s", endpoint.Name, endpoint.Method)
			}
		case "POST":
		default:
			return Config{}, fmt.Errorf("endpoint %q method must be GET, HEAD, or POST", endpoint.Name)
		}
		if endpoint.Encoding != "json" && endpoint.Encoding != "form" {
			return Config{}, fmt.Errorf("endpoint %q encoding must be json or form", endpoint.Name)
		}
		if err := validateHeaders(endpoint.Headers); err != nil {
			return Config{}, fmt.Errorf("endpoint %q: %w", endpoint.Name, err)
		}
	}
	if err := validateHeaders(config.Headers); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validatePath(path, field string) error {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return fmt.Errorf("%s must be an absolute URL path", field)
	}
	parsed, err := url.Parse(path)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s is invalid", field)
	}
	return nil
}

func validateHeaders(headers map[string]string) error {
	for name, value := range headers {
		if strings.ContainsAny(name, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("header %q contains a newline", name)
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "user-token", "authorization", "cookie", "host", "content-length":
			return fmt.Errorf("header %q is managed or forbidden", name)
		}
	}
	return nil
}

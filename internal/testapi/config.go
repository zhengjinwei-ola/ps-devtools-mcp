package testapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const defaultMaxResponseBytes = 512 << 10

type Endpoint struct {
	Name              string   `json:"name"`
	URL               string   `json:"url"`
	Method            string   `json:"method"`
	AllowedQueryKeys  []string `json:"allowed_query_keys,omitempty"`
	AllowedBodyFields []string `json:"allowed_body_fields,omitempty"`
}

type Config struct {
	Endpoints        []Endpoint `json:"endpoints"`
	MaxResponseBytes int64      `json:"max_response_bytes,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read read-only API config: %w", err)
	}
	var config Config
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode read-only API config: %w", err)
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	if config.MaxResponseBytes < 1024 || config.MaxResponseBytes > 2<<20 {
		return Config{}, fmt.Errorf("max_response_bytes must be between 1024 and 2097152")
	}
	if len(config.Endpoints) == 0 {
		return Config{}, fmt.Errorf("at least one read-only endpoint is required")
	}
	seen := make(map[string]struct{}, len(config.Endpoints))
	for index := range config.Endpoints {
		endpoint := &config.Endpoints[index]
		endpoint.Name = strings.TrimSpace(endpoint.Name)
		endpoint.Method = strings.ToUpper(strings.TrimSpace(endpoint.Method))
		if endpoint.Name == "" || strings.ContainsAny(endpoint.Name, " /\\") {
			return Config{}, fmt.Errorf("endpoint name %q is invalid", endpoint.Name)
		}
		if _, duplicate := seen[endpoint.Name]; duplicate {
			return Config{}, fmt.Errorf("endpoint name %q is duplicated", endpoint.Name)
		}
		seen[endpoint.Name] = struct{}{}
		parsed, err := url.Parse(endpoint.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return Config{}, fmt.Errorf("endpoint %q has an invalid URL", endpoint.Name)
		}
		if parsed.RawQuery != "" {
			return Config{}, fmt.Errorf("endpoint %q URL cannot contain query parameters", endpoint.Name)
		}
		switch endpoint.Method {
		case "GET", "HEAD":
			if len(endpoint.AllowedBodyFields) > 0 {
				return Config{}, fmt.Errorf("endpoint %q cannot allow a body for %s", endpoint.Name, endpoint.Method)
			}
		case "POST":
		default:
			return Config{}, fmt.Errorf("endpoint %q method must be GET, HEAD, or explicitly allowlisted POST", endpoint.Name)
		}
	}
	return config, nil
}

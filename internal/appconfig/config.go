package appconfig

import (
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

type Config struct {
	Transport         string
	ListenAddress     string
	AuthToken         string
	AuthTokens        map[string]string
	MaxConcurrent     int
	TestDBHost        string
	TestDBPort        int
	TestDBUser        string
	TestDBPassword    string
	XianshiDatabase   string
	ConfigDatabase    string
	TestRedisAddress  string
	TestRedisPassword string
	TestRedisDatabase int
	LogMCPCommand     string
	LogMCPArgs        []string
	ReadOnlyAPIConfig string
}

func Parse(args []string, getenv func(string) string) (Config, error) {
	config := Config{
		Transport:         envOrDefault(getenv, "PS_MCP_TRANSPORT", TransportStdio),
		ListenAddress:     envOrDefault(getenv, "PS_MCP_LISTEN_ADDR", "127.0.0.1:8080"),
		AuthToken:         strings.TrimSpace(getenv("PS_MCP_AUTH_TOKEN")),
		MaxConcurrent:     envIntOrDefault(getenv, "PS_MCP_MAX_CONCURRENT", 16),
		TestDBHost:        strings.TrimSpace(getenv("PS_TEST_DB_HOST")),
		TestDBPort:        envIntOrDefault(getenv, "PS_TEST_DB_PORT", 3306),
		TestDBUser:        strings.TrimSpace(getenv("PS_TEST_DB_USER")),
		TestDBPassword:    getenv("PS_TEST_DB_PASSWORD"),
		XianshiDatabase:   envOrDefault(getenv, "PS_TEST_DB_XIANSHI_DATABASE", "xianshi"),
		ConfigDatabase:    envOrDefault(getenv, "PS_TEST_DB_CONFIG_DATABASE", "config"),
		TestRedisAddress:  strings.TrimSpace(getenv("PS_TEST_REDIS_ADDRESS")),
		TestRedisPassword: getenv("PS_TEST_REDIS_PASSWORD"),
		TestRedisDatabase: envIntOrDefault(getenv, "PS_TEST_REDIS_DATABASE", 0),
		LogMCPCommand:     strings.TrimSpace(getenv("PS_LOG_MCP_COMMAND")),
		ReadOnlyAPIConfig: strings.TrimSpace(getenv("PS_MCP_READONLY_API_CONFIG")),
	}
	if raw := strings.TrimSpace(getenv("PS_MCP_AUTH_TOKENS_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &config.AuthTokens); err != nil {
			return Config{}, fmt.Errorf("PS_MCP_AUTH_TOKENS_JSON must be a JSON object of token names to token values: %w", err)
		}
		if err := validateAuthTokens(config.AuthTokens); err != nil {
			return Config{}, err
		}
	}
	if config.AuthToken != "" && len(config.AuthTokens) > 0 {
		if _, exists := config.AuthTokens["legacy"]; exists {
			return Config{}, fmt.Errorf("PS_MCP_AUTH_TOKENS_JSON token name %q is reserved when PS_MCP_AUTH_TOKEN is set", "legacy")
		}
		for name, value := range config.AuthTokens {
			if subtleTokenEqual(value, config.AuthToken) {
				return Config{}, fmt.Errorf("PS_MCP_AUTH_TOKEN duplicates PS_MCP_AUTH_TOKENS_JSON token %q", name)
			}
		}
	}
	if raw := strings.TrimSpace(getenv("PS_LOG_MCP_ARGS_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &config.LogMCPArgs); err != nil {
			return Config{}, fmt.Errorf("PS_LOG_MCP_ARGS_JSON must be a JSON string array: %w", err)
		}
	}

	flags := flag.NewFlagSet("ps-devtools-mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&config.Transport, "transport", config.Transport, "MCP transport: stdio or http")
	flags.StringVar(&config.ListenAddress, "listen", config.ListenAddress, "HTTP listen address")
	flags.IntVar(&config.MaxConcurrent, "max-concurrent", config.MaxConcurrent, "maximum concurrent HTTP MCP requests")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}

	config.Transport = strings.ToLower(strings.TrimSpace(config.Transport))
	config.ListenAddress = strings.TrimSpace(config.ListenAddress)
	switch config.Transport {
	case TransportStdio:
	case TransportHTTP:
		if config.ListenAddress == "" {
			return Config{}, fmt.Errorf("HTTP listen address is required")
		}
		if config.AuthToken == "" && len(config.AuthTokens) == 0 {
			return Config{}, fmt.Errorf("PS_MCP_AUTH_TOKEN or PS_MCP_AUTH_TOKENS_JSON is required for HTTP transport")
		}
	default:
		return Config{}, fmt.Errorf("transport must be %q or %q", TransportStdio, TransportHTTP)
	}
	if config.MaxConcurrent <= 0 {
		return Config{}, fmt.Errorf("max-concurrent must be positive")
	}
	if config.TestDBHost != "" {
		if config.TestDBPort < 1 || config.TestDBPort > 65535 {
			return Config{}, fmt.Errorf("PS_TEST_DB_PORT must be between 1 and 65535")
		}
		if config.TestDBUser == "" {
			return Config{}, fmt.Errorf("PS_TEST_DB_USER is required when PS_TEST_DB_HOST is set")
		}
		if config.TestDBPassword == "" {
			return Config{}, fmt.Errorf("PS_TEST_DB_PASSWORD is required when PS_TEST_DB_HOST is set")
		}
	}
	if config.TestRedisDatabase < 0 {
		return Config{}, fmt.Errorf("PS_TEST_REDIS_DATABASE must not be negative")
	}
	if config.LogMCPCommand == "" && len(config.LogMCPArgs) > 0 {
		return Config{}, fmt.Errorf("PS_LOG_MCP_COMMAND is required when PS_LOG_MCP_ARGS_JSON is set")
	}
	if config.ReadOnlyAPIConfig != "" && !strings.HasPrefix(config.ReadOnlyAPIConfig, "/") {
		return Config{}, fmt.Errorf("PS_MCP_READONLY_API_CONFIG must be an absolute path")
	}
	return config, nil
}

func subtleTokenEqual(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func validateAuthTokens(tokens map[string]string) error {
	seenValues := make(map[string]string, len(tokens))
	for name, value := range tokens {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("PS_MCP_AUTH_TOKENS_JSON contains an empty token name")
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("PS_MCP_AUTH_TOKENS_JSON token %q is empty", name)
		}
		if previous, exists := seenValues[value]; exists {
			return fmt.Errorf("PS_MCP_AUTH_TOKENS_JSON token value is shared by %q and %q", previous, name)
		}
		tokens[name] = value
		seenValues[value] = name
	}
	return nil
}

func (c Config) DirectDBEnabled() bool {
	return c.TestDBHost != ""
}

func (c Config) DirectRedisEnabled() bool {
	return c.TestRedisAddress != ""
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envIntOrDefault(getenv func(string) string, key string, fallback int) int {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

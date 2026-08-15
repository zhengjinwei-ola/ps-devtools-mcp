package appconfig

import "testing"

func TestParseDefaultsToStdio(t *testing.T) {
	config, err := Parse(nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if config.Transport != TransportStdio || config.ListenAddress != "127.0.0.1:8080" || config.MaxConcurrent != 16 {
		t.Fatalf("config = %+v", config)
	}
}

func TestParseHTTPRequiresToken(t *testing.T) {
	_, err := Parse([]string{"--transport=http"}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestParseHTTPFromEnvironment(t *testing.T) {
	values := map[string]string{
		"PS_MCP_TRANSPORT":           "http",
		"PS_MCP_LISTEN_ADDR":         ":9090",
		"PS_MCP_AUTH_TOKEN":          "secret",
		"PS_MCP_MAX_CONCURRENT":      "8",
		"PS_TEST_DB_HOST":            "db.test.internal",
		"PS_TEST_DB_PORT":            "3307",
		"PS_TEST_DB_USER":            "readonly",
		"PS_TEST_DB_PASSWORD":        "db-secret",
		"PS_TEST_REDIS_ADDRESS":      "127.0.0.1:6379",
		"PS_TEST_REDIS_DATABASE":     "2",
		"PS_LOG_MCP_COMMAND":         "/usr/local/bin/log-mcp",
		"PS_LOG_MCP_ARGS_JSON":       `["--config=/etc/logs.json"]`,
		"PS_MCP_READONLY_API_CONFIG": "/etc/readonly-apis.json",
	}
	config, err := Parse(nil, func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.Transport != TransportHTTP || config.ListenAddress != ":9090" || config.AuthToken != "secret" || config.MaxConcurrent != 8 || len(config.LogMCPArgs) != 1 || !config.DirectDBEnabled() || config.TestDBPort != 3307 || !config.DirectRedisEnabled() || config.TestRedisDatabase != 2 {
		t.Fatalf("config = %+v", config)
	}
}

func TestParseRejectsNegativeRedisDatabase(t *testing.T) {
	_, err := Parse(nil, func(key string) string {
		if key == "PS_TEST_REDIS_DATABASE" {
			return "-1"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected invalid Redis database error")
	}
}

func TestParseHTTPWithNamedTokens(t *testing.T) {
	values := map[string]string{
		"PS_MCP_TRANSPORT":        "http",
		"PS_MCP_AUTH_TOKENS_JSON": `{"alice":"alice-secret","bob":"bob-secret"}`,
	}
	config, err := Parse(nil, func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if len(config.AuthTokens) != 2 || config.AuthTokens["alice"] != "alice-secret" || config.AuthTokens["bob"] != "bob-secret" {
		t.Fatalf("auth tokens = %#v", config.AuthTokens)
	}
}

func TestParseRejectsInvalidNamedTokens(t *testing.T) {
	tests := map[string]string{
		"invalid JSON":     `["secret"]`,
		"empty name":       `{"":"secret"}`,
		"empty value":      `{"alice":""}`,
		"duplicate values": `{"alice":"secret","bob":"secret"}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			values := map[string]string{"PS_MCP_AUTH_TOKENS_JSON": raw}
			if _, err := Parse(nil, func(key string) string { return values[key] }); err == nil {
				t.Fatal("expected invalid named token error")
			}
		})
	}
}

func TestParseRejectsConflictingLegacyAndNamedTokens(t *testing.T) {
	tests := map[string]string{
		"reserved name":   `{"legacy":"another-secret"}`,
		"duplicate value": `{"alice":"secret"}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			values := map[string]string{
				"PS_MCP_AUTH_TOKEN":       "secret",
				"PS_MCP_AUTH_TOKENS_JSON": raw,
			}
			if _, err := Parse(nil, func(key string) string { return values[key] }); err == nil {
				t.Fatal("expected conflicting token error")
			}
		})
	}
}

func TestParseDirectDBRequiresCredentials(t *testing.T) {
	for _, missing := range []string{"PS_TEST_DB_USER", "PS_TEST_DB_PASSWORD"} {
		t.Run(missing, func(t *testing.T) {
			values := map[string]string{
				"PS_TEST_DB_HOST":     "127.0.0.1",
				"PS_TEST_DB_USER":     "readonly",
				"PS_TEST_DB_PASSWORD": "secret",
			}
			delete(values, missing)
			if _, err := Parse(nil, func(key string) string { return values[key] }); err == nil {
				t.Fatalf("expected error when %s is missing", missing)
			}
		})
	}
}

func TestParseRejectsInvalidLogArguments(t *testing.T) {
	_, err := Parse(nil, func(key string) string {
		if key == "PS_LOG_MCP_ARGS_JSON" {
			return `{"bad":true}`
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected invalid log argument error")
	}
}

func TestFlagsOverrideTransportEnvironment(t *testing.T) {
	values := map[string]string{"PS_MCP_TRANSPORT": "http", "PS_MCP_AUTH_TOKEN": "secret"}
	config, err := Parse([]string{"--transport=stdio"}, func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.Transport != TransportStdio {
		t.Fatalf("transport = %q", config.Transport)
	}
}

func TestParseTestBotRequiresCompleteConfiguration(t *testing.T) {
	values := map[string]string{
		"PS_MCP_TESTBOT_CONFIG": "/etc/testbot.json",
		"PS_TESTBOT_AREA":       "86",
		"PS_TESTBOT_MOBILE":     "test-mobile",
		"PS_TESTBOT_PASSWORD":   "test-password",
	}
	config, err := Parse(nil, func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !config.TestBotEnabled() {
		t.Fatal("testbot should be enabled")
	}
	delete(values, "PS_TESTBOT_PASSWORD")
	if _, err := Parse(nil, func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected incomplete testbot configuration error")
	}
}

func TestParseSlackDeployWebhook(t *testing.T) {
	config, err := Parse(nil, func(key string) string {
		if key == "PS_MCP_SLACK_DEPLOY_WEBHOOK_URL" {
			return "https://hooks.slack.com/services/T/B/S"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.SlackDeployWebhookURL != "https://hooks.slack.com/services/T/B/S" {
		t.Fatalf("webhook URL = %q", config.SlackDeployWebhookURL)
	}
}

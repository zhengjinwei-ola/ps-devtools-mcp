package redact

import (
	"strings"
	"testing"
)

func TestJSONRedactsSensitiveKeys(t *testing.T) {
	value := JSON(map[string]any{
		"access_token": "secret",
		"mobile":       "13800000000",
		"nested":       map[string]any{"user_password": "p", "name": "kept"},
		"list":         []any{map[string]any{"session_id": "s"}},
	}).(map[string]any)
	if value["access_token"] != "[REDACTED]" || value["mobile"] != "[REDACTED]" {
		t.Fatalf("value = %#v", value)
	}
	nested := value["nested"].(map[string]any)
	if nested["user_password"] != "[REDACTED]" || nested["name"] != "kept" {
		t.Fatalf("nested = %#v", nested)
	}
	item := value["list"].([]any)[0].(map[string]any)
	if item["session_id"] != "[REDACTED]" {
		t.Fatalf("list = %#v", value["list"])
	}
}

func TestTextRedactsPlainAndQuotedForms(t *testing.T) {
	plain := Text("password=abc123 note=hello")
	if strings.Contains(plain, "abc123") || !strings.Contains(plain, "[REDACTED]") || !strings.Contains(plain, "hello") {
		t.Fatalf("plain = %q", plain)
	}
	quoted := Text(`{"user-token":"secret-token","ok":true}`)
	if strings.Contains(quoted, "secret-token") || !strings.Contains(quoted, `"ok":true`) {
		t.Fatalf("quoted = %q", quoted)
	}
}

func TestResponseFallsBackToTextForTruncatedJSON(t *testing.T) {
	body := Response([]byte(`{"access_token":"secret","value":"partial`), "application/json", true).(string)
	if strings.Contains(body, "secret") || !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("body = %q", body)
	}
}

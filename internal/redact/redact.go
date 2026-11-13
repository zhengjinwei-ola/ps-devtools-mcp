// Package redact 统一响应脱敏：JSON 键名递归替换与非 JSON 文本正则替换。
// testapi 与 testbot 共用，避免两边脱敏规则漂移。
package redact

import (
	"encoding/json"
	"regexp"
	"strings"
)

var textSecrets = regexp.MustCompile(`(?i)((?:authorization|user-token|access_?token|refresh_?token|password|passwd|secret|cookie|session)["']?\s*[:=]\s*["']?)[^"'\s,;]+`)

// JSON 递归替换 map 中敏感键名的值（含嵌套数组）。
func JSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "cookie") || strings.Contains(lower, "session") || strings.Contains(lower, "authorization") || strings.Contains(lower, "mobile") || strings.Contains(lower, "phone") {
				typed[key] = "[REDACTED]"
				continue
			}
			typed[key] = JSON(item)
		}
	case []any:
		for index := range typed {
			typed[index] = JSON(typed[index])
		}
	}
	return value
}

// Text 对非 JSON（或截断后不完整）的响应做文本正则脱敏。
func Text(data string) string {
	return textSecrets.ReplaceAllString(data, "${1}[REDACTED]")
}

// Response 按内容类型选择脱敏方式：完整 JSON 走键名递归，其余走文本正则。
func Response(data []byte, contentType string, truncated bool) any {
	if strings.Contains(strings.ToLower(contentType), "json") && !truncated {
		var value any
		if json.Unmarshal(data, &value) == nil {
			return JSON(value)
		}
	}
	return Text(string(data))
}

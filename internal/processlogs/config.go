package processlogs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	defaultMaxLines      = 200
	defaultMaxLineBytes  = 8192
	absoluteMaxLines     = 500
	absoluteMaxLineBytes = 64 << 10
)

type Source struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
}

type Config struct {
	Sources      []Source `json:"sources"`
	MaxLines     int      `json:"max_lines,omitempty"`
	MaxLineBytes int      `json:"max_line_bytes,omitempty"`
}

// LoadConfig 读取进程日志白名单配置。文件路径与 glob 只来自服务端配置，
// 调用方仅能按 source 名称取尾部，不存在任意路径输入。
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read process log config: %w", err)
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode process log config: %w", err)
	}
	config.applyDefaults()
	if config.MaxLines < 1 || config.MaxLines > absoluteMaxLines {
		return Config{}, fmt.Errorf("max_lines must be between 1 and %d", absoluteMaxLines)
	}
	if config.MaxLineBytes < 1024 || config.MaxLineBytes > absoluteMaxLineBytes {
		return Config{}, fmt.Errorf("max_line_bytes must be between 1024 and %d", absoluteMaxLineBytes)
	}
	seen := make(map[string]struct{}, len(config.Sources))
	for _, source := range config.Sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			return Config{}, fmt.Errorf("process log source name is required")
		}
		if _, ok := seen[name]; ok {
			return Config{}, fmt.Errorf("duplicate process log source %q", name)
		}
		seen[name] = struct{}{}
		if len(source.Patterns) == 0 {
			return Config{}, fmt.Errorf("process log source %q has no patterns", name)
		}
		for _, pattern := range source.Patterns {
			if !strings.HasPrefix(pattern, "/") {
				return Config{}, fmt.Errorf("process log source %q pattern must be an absolute path: %q", name, pattern)
			}
		}
	}
	return config, nil
}

func (config *Config) applyDefaults() {
	if config.MaxLines == 0 {
		config.MaxLines = defaultMaxLines
	}
	if config.MaxLineBytes == 0 {
		config.MaxLineBytes = defaultMaxLineBytes
	}
}

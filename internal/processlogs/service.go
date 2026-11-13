// Package processlogs 提供白名单进程日志尾部读取：004 排障时查看
// Supervisor 进程自身的 stdout/stderr 日志尾部，每行截断并脱敏。
// 文件路径与 glob 只来自服务端配置，调用方仅按 source 名称取尾部。
package processlogs

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/olaola-chat/ps-devtools-mcp/internal/redact"
)

const (
	defaultLines      = 100
	maxFilesPerSource = 16
)

type TailInput struct {
	Source string `json:"source" jsonschema:"Process log source name (Supervisor process name) from the server-side allowlist."`
	Lines  int    `json:"lines,omitempty" jsonschema:"Tail lines per file; zero uses 100, capped at the configured max_lines."`
}

type FileTail struct {
	File  string   `json:"file"`
	Lines []string `json:"lines"`
	Error string   `json:"error,omitempty"`
}

type TailOutput struct {
	Files []FileTail `json:"files"`
}

type Service struct {
	sources      map[string]Source
	maxLines     int
	maxLineBytes int
	logger       *log.Logger
}

func NewService(config Config, logger *log.Logger) *Service {
	config.applyDefaults()
	sources := make(map[string]Source, len(config.Sources))
	for _, source := range config.Sources {
		sources[strings.TrimSpace(source.Name)] = source
	}
	return &Service{sources: sources, maxLines: config.MaxLines, maxLineBytes: config.MaxLineBytes, logger: logger}
}

type Unavailable struct{}

func (Unavailable) Tail(context.Context, TailInput) (TailOutput, error) {
	return TailOutput{}, fmt.Errorf("process log tailing is not configured; set PS_MCP_PROCESS_LOGS_CONFIG")
}

func (s *Service) Tail(_ context.Context, input TailInput) (TailOutput, error) {
	source, ok := s.sources[strings.TrimSpace(input.Source)]
	if !ok {
		return TailOutput{}, fmt.Errorf("unknown process log source %q", input.Source)
	}
	lines := input.Lines
	if lines == 0 {
		lines = defaultLines
	}
	if lines < 1 {
		return TailOutput{}, fmt.Errorf("lines must be at least 1")
	}
	if lines > s.maxLines {
		lines = s.maxLines
	}

	files := make([]string, 0, len(source.Patterns))
	seen := make(map[string]struct{})
	for _, pattern := range source.Patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return TailOutput{}, fmt.Errorf("expand pattern %q: %w", pattern, err)
		}
		for _, match := range matches {
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			files = append(files, match)
		}
	}
	sort.Strings(files)
	// Homogeneous groups (e.g. xs.admin.01..10) can match many files; cap the
	// blast radius instead of returning an unbounded response.
	if len(files) > maxFilesPerSource {
		if s.logger != nil {
			s.logger.Printf("tool=tail_test_process_logs source=%q files=%d capped_to=%d", input.Source, len(files), maxFilesPerSource)
		}
		files = files[:maxFilesPerSource]
	}

	output := TailOutput{Files: make([]FileTail, 0, len(files))}
	for _, file := range files {
		tail, err := tailFile(file, lines, s.maxLineBytes)
		if err != nil {
			output.Files = append(output.Files, FileTail{File: file, Error: err.Error()})
			continue
		}
		output.Files = append(output.Files, FileTail{File: file, Lines: tail})
	}
	return output, nil
}

// tailFile 从文件尾部读取最后 lines 行，每行按 maxLineBytes UTF-8 安全截断
// 并做敏感字段脱敏。只读文件尾部一个有界窗口，避免整读大日志文件。
func tailFile(path string, lines, maxLineBytes int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	window := int64(lines)*int64(maxLineBytes) + int64(maxLineBytes)
	offset := int64(0)
	if info.Size() > window {
		offset = info.Size() - window
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, window))
	if err != nil {
		return nil, err
	}
	content := string(data)
	// 从窗口起点切割的第一行大概率不完整，丢弃；文件本身较短时保留首行。
	if offset > 0 {
		if index := strings.IndexByte(content, '\n'); index >= 0 {
			content = content[index+1:]
		}
	}
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return []string{}, nil
	}
	all := strings.Split(content, "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	result := make([]string, 0, len(all))
	for _, line := range all {
		result = append(result, redact.Text(truncateLine(line, maxLineBytes)))
	}
	return result, nil
}

// truncateLine 按 UTF-8 边界截断超长行，避免切割多字节字符产生乱码。
func truncateLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	cut := limit
	for cut > 0 && !utf8.ValidString(line[:cut]) {
		cut--
	}
	return line[:cut] + "…"
}

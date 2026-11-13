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
	"time"
	"unicode/utf8"

	"github.com/olaola-chat/ps-devtools-mcp/internal/redact"
)

const (
	defaultLines      = 100
	maxFilesPerSource = 16
	defaultListLimit  = 200
	maxListFiles      = 500
)

type TailInput struct {
	Source string `json:"source,omitempty" jsonschema:"Process log source name (Supervisor process name) from the server-side allowlist. Required unless file is set."`
	Lines  int    `json:"lines,omitempty" jsonschema:"Tail lines per file; zero uses 100, capped at the configured max_lines."`
	File   string `json:"file,omitempty" jsonschema:"Bare file name inside the configured log_files_root (no path separators). Mutually exclusive with source."`
}

type ListInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum files to return; zero uses 200, capped at 500. Files are ordered by most recent modification."`
}

type LogFileInfo struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"size_bytes"`
	ModifiedAt string `json:"modified_at"`
}

type ListOutput struct {
	Root      string        `json:"root"`
	Files     []LogFileInfo `json:"files"`
	Total     int           `json:"total"`
	Truncated bool          `json:"truncated"`
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
	logFilesRoot string
	logger       *log.Logger
}

func NewService(config Config, logger *log.Logger) *Service {
	config.applyDefaults()
	sources := make(map[string]Source, len(config.Sources))
	for _, source := range config.Sources {
		sources[strings.TrimSpace(source.Name)] = source
	}
	return &Service{
		sources:      sources,
		maxLines:     config.MaxLines,
		maxLineBytes: config.MaxLineBytes,
		logFilesRoot: cleanLogFilesRoot(config.LogFilesRoot),
		logger:       logger,
	}
}

// cleanLogFilesRoot 规整日志根目录；未配置时保持为空，避免 Clean 把空串
// 变成 "." 导致列表落在服务进程的工作目录上。
func cleanLogFilesRoot(root string) string {
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

type Unavailable struct{}

func (Unavailable) Tail(context.Context, TailInput) (TailOutput, error) {
	return TailOutput{}, fmt.Errorf("process log tailing is not configured; set PS_MCP_PROCESS_LOGS_CONFIG")
}

func (Unavailable) List(context.Context, ListInput) (ListOutput, error) {
	return ListOutput{}, fmt.Errorf("log file listing is not configured; set PS_MCP_PROCESS_LOGS_CONFIG with log_files_root")
}

// resolveTailLines 校验并收敛尾部行数上限。
func resolveTailLines(input int, maxLines int) (int, error) {
	lines := input
	if lines == 0 {
		lines = defaultLines
	}
	if lines < 1 {
		return 0, fmt.Errorf("lines must be at least 1")
	}
	if lines > maxLines {
		lines = maxLines
	}
	return lines, nil
}

func (s *Service) Tail(_ context.Context, input TailInput) (TailOutput, error) {
	lines, err := resolveTailLines(input.Lines, s.maxLines)
	if err != nil {
		return TailOutput{}, err
	}
	if file := strings.TrimSpace(input.File); file != "" {
		if strings.TrimSpace(input.Source) != "" {
			return TailOutput{}, fmt.Errorf("source and file are mutually exclusive")
		}
		return s.tailByName(file, lines)
	}
	source, ok := s.sources[strings.TrimSpace(input.Source)]
	if !ok {
		return TailOutput{}, fmt.Errorf("unknown process log source %q", input.Source)
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

// List 枚举 log_files_root 下的普通日志文件，按修改时间倒序返回有界窗口。
// 只读目录一层且跳过符号链接，避免目录逃逸；结果仅含文件名与元数据。
func (s *Service) List(_ context.Context, input ListInput) (ListOutput, error) {
	if s.logFilesRoot == "" {
		return ListOutput{}, fmt.Errorf("log file listing is not configured; set log_files_root in the process logs config")
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultListLimit
	}
	if limit < 1 {
		return ListOutput{}, fmt.Errorf("limit must be at least 1")
	}
	if limit > maxListFiles {
		limit = maxListFiles
	}

	entries, err := os.ReadDir(s.logFilesRoot)
	if err != nil {
		return ListOutput{}, fmt.Errorf("read log root %q: %w", s.logFilesRoot, err)
	}
	type listedFile struct {
		info LogFileInfo
		mod  time.Time
	}
	files := make([]listedFile, 0, len(entries))
	for _, entry := range entries {
		// entry.Info 走 lstat：符号链接不是普通文件，直接跳过，杜绝根目录逃逸。
		info, err := entry.Info()
		if err != nil {
			// 并发清理日志时条目可能消失，跳过而不是让整个列表失败。
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		mod := info.ModTime()
		files = append(files, listedFile{
			info: LogFileInfo{
				Name:       entry.Name(),
				SizeBytes:  info.Size(),
				ModifiedAt: mod.UTC().Format(time.RFC3339),
			},
			mod: mod,
		})
	}
	total := len(files)
	// 用原始 ModTime 排序：RFC3339 只有秒级精度，会丢失亚秒顺序。
	sort.Slice(files, func(i, j int) bool {
		if !files[i].mod.Equal(files[j].mod) {
			return files[i].mod.After(files[j].mod)
		}
		return files[i].info.Name < files[j].info.Name
	})
	truncated := total > limit
	if truncated {
		files = files[:limit]
	}
	output := make([]LogFileInfo, len(files))
	for i, file := range files {
		output[i] = file.info
	}
	return ListOutput{Root: s.logFilesRoot, Files: output, Total: total, Truncated: truncated}, nil
}

// tailByName 按裸文件名读取 log_files_root 内的日志尾部。名字不允许包含
// 路径分隔符，并用 lstat 校验目标必须是普通文件，防止符号链接与目录逃逸。
func (s *Service) tailByName(name string, lines int) (TailOutput, error) {
	if s.logFilesRoot == "" {
		return TailOutput{}, fmt.Errorf("tail by file requires log_files_root in the process logs config")
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return TailOutput{}, fmt.Errorf("file must be a bare file name inside the configured log root")
	}
	path := filepath.Join(s.logFilesRoot, name)
	info, err := os.Lstat(path)
	if err != nil {
		return TailOutput{}, fmt.Errorf("stat log file %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return TailOutput{}, fmt.Errorf("log file %q is not a regular file", name)
	}
	tail, err := tailFile(path, lines, s.maxLineBytes)
	if err != nil {
		return TailOutput{Files: []FileTail{{File: path, Error: err.Error()}}}, nil
	}
	return TailOutput{Files: []FileTail{{File: path, Lines: tail}}}, nil
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

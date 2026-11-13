package testdeploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// historyStore 把终态部署任务追加写入 JSONL 文件并在启动时加载回内存，
// 使部署历史跨 MCP 进程重启可查。写入失败不阻塞部署，仅返回错误由调用方
// 记录日志——历史落盘是尽力而为的增强，不是部署结果的一部分。
type historyStore struct {
	path  string
	limit int
	mu    sync.Mutex
}

func newHistoryStore(path string, limit int) *historyStore {
	return &historyStore{path: path, limit: limit}
}

// Load 读取文件末尾 limit 条终态任务；文件超过 2 倍 limit 时重写裁剪到
// limit，避免长期运行下无限增长。损坏行跳过（例如写入中途崩溃的半行）。
func (h *historyStore) Load() ([]DeploymentTask, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read deployment history: %w", err)
	}
	var all []DeploymentTask
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var task DeploymentTask
		if err := json.Unmarshal([]byte(line), &task); err != nil {
			continue
		}
		if isTerminal(task.Status) {
			all = append(all, task)
		}
	}
	if len(all) <= h.limit {
		return all, nil
	}
	all = all[len(all)-h.limit:]
	if err := h.rewriteLocked(all); err != nil {
		return all, err
	}
	return all, nil
}

// Append 把一个终态任务序列化为一行 JSONL 追加写入。
func (h *historyStore) Append(task DeploymentTask) error {
	if !isTerminal(task.Status) {
		return fmt.Errorf("refusing to persist non-terminal deployment %s", task.DeploymentID)
	}
	encoded, err := json.Marshal(cloneTask(task, true))
	if err != nil {
		return fmt.Errorf("encode deployment history entry: %w", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return fmt.Errorf("create deployment history directory: %w", err)
	}
	file, err := os.OpenFile(h.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open deployment history: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append deployment history: %w", err)
	}
	return nil
}

func (h *historyStore) rewriteLocked(tasks []DeploymentTask) error {
	buffer := &bytes.Buffer{}
	for _, task := range tasks {
		encoded, err := json.Marshal(cloneTask(task, true))
		if err != nil {
			continue
		}
		buffer.Write(encoded)
		buffer.WriteByte('\n')
	}
	temp := h.path + ".tmp"
	if err := os.WriteFile(temp, buffer.Bytes(), 0o600); err != nil {
		return fmt.Errorf("rewrite deployment history: %w", err)
	}
	if err := os.Rename(temp, h.path); err != nil {
		return fmt.Errorf("replace deployment history: %w", err)
	}
	return nil
}

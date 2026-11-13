// Package mcpinfo 暴露 MCP 自身的运行信息：版本、构建 revision、运行时长
// 与监听配置，用于部署后验收与排障，替代 stdio 探针绕路确认版本。
package mcpinfo

import (
	"context"
	"os"
	"runtime"
	"time"
)

type Input struct{}

type Info struct {
	Version       string    `json:"version"`
	Revision      string    `json:"revision"`
	GoVersion     string    `json:"go_version"`
	Transport     string    `json:"transport"`
	ListenAddress string    `json:"listen_address,omitempty"`
	PID           int       `json:"pid"`
	StartTime     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
}

type Service struct {
	info Info
}

func NewService(version, revision, transport, listenAddress string) *Service {
	return &Service{info: Info{
		Version: version, Revision: revision, GoVersion: runtime.Version(),
		Transport: transport, ListenAddress: listenAddress,
		PID: os.Getpid(), StartTime: time.Now().UTC(),
	}}
}

func (s *Service) Get(_ context.Context, _ Input) (Info, error) {
	info := s.info
	info.UptimeSeconds = int64(time.Since(info.StartTime).Round(time.Second) / time.Second)
	return info, nil
}

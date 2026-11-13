package mcpinfo

import (
	"context"
	"testing"
	"time"
)

func TestGetReturnsUptimeAndStaticInfo(t *testing.T) {
	service := NewService("v0.4.0", "abc1234", "http", "127.0.0.1:18081")
	service.info.StartTime = time.Now().Add(-90 * time.Second)
	info, err := service.Get(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v0.4.0" || info.Revision != "abc1234" || info.Transport != "http" ||
		info.ListenAddress != "127.0.0.1:18081" || info.GoVersion == "" || info.PID == 0 {
		t.Fatalf("info = %#v", info)
	}
	if info.UptimeSeconds != 90 {
		t.Fatalf("uptime = %d, want 90", info.UptimeSeconds)
	}
}

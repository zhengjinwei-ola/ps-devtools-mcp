package slacknotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/olaola-chat/ps-devtools-mcp/internal/testdeploy"
)

const maxResponseBytes = 4 << 10

type Webhook struct {
	url    string
	client *http.Client
}

func NewWebhook(rawURL string, client *http.Client) (*Webhook, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host != "hooks.slack.com" || !isSupportedWebhookPath(parsed.Path) {
		return nil, fmt.Errorf("Slack deployment webhook must be an https://hooks.slack.com/services/... or https://hooks.slack.com/triggers/... URL")
	}
	if client == nil {
		return nil, fmt.Errorf("Slack deployment webhook HTTP client is required")
	}
	return &Webhook{url: parsed.String(), client: client}, nil
}

func isSupportedWebhookPath(path string) bool {
	return strings.HasPrefix(path, "/services/") || strings.HasPrefix(path, "/triggers/")
}

func (w *Webhook) Notify(ctx context.Context, event testdeploy.DeploymentNotification) error {
	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: formatMessage(event)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Slack deployment notification: %w", err)
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("send Slack deployment notification: status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func formatMessage(event testdeploy.DeploymentNotification) string {
	status := "🛠️ *【开始编译】旧服务构建任务已启动*"
	switch event.Status {
	case testdeploy.DeploymentDeploying:
		status = "🚀 *【开始部署】编译完成，正在备份旧版本、替换新版本并重启服务*"
	case testdeploy.DeploymentSucceeded:
		status = "✅ *【部署成功】新版本已上线，服务运行正常*"
	case testdeploy.DeploymentCompileFailed:
		status = "❌ *【编译失败】构建未通过，旧服务未受影响*"
	case testdeploy.DeploymentFailed:
		status = "🚨 *【部署失败】新版本上线未完成，旧版本保护机制已生效*"
	}
	message := fmt.Sprintf("%s\n• 服务：`%s`\n• 进程：`%s`",
		status, event.Service, strings.Join(event.Processes, ", "))
	if event.Status == testdeploy.DeploymentSucceeded || event.Status == testdeploy.DeploymentCompileFailed || event.Status == testdeploy.DeploymentFailed {
		message += fmt.Sprintf("\n• 耗时：`%s`", event.Duration.Round(100*time.Millisecond))
	}
	return message
}

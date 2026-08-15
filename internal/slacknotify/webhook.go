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
	if err != nil || parsed.Scheme != "https" || parsed.Host != "hooks.slack.com" || !strings.HasPrefix(parsed.Path, "/services/") {
		return nil, fmt.Errorf("Slack deployment webhook must be an https://hooks.slack.com/services/... URL")
	}
	if client == nil {
		return nil, fmt.Errorf("Slack deployment webhook HTTP client is required")
	}
	return &Webhook{url: parsed.String(), client: client}, nil
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
	status := ":rocket: Test deployment started"
	switch event.Status {
	case testdeploy.DeploymentSucceeded:
		status = ":white_check_mark: Test deployment succeeded"
	case testdeploy.DeploymentFailed:
		status = ":x: Test deployment failed"
	}
	message := fmt.Sprintf("%s\n*Service:* `%s`\n*Processes:* `%s`\n*Skip tests:* `%t`",
		status, event.Service, strings.Join(event.Processes, ", "), event.SkipTests)
	if event.Status != testdeploy.DeploymentStarted {
		message += fmt.Sprintf("\n*Duration:* `%s`", event.Duration.Round(100*time.Millisecond))
	}
	return message
}

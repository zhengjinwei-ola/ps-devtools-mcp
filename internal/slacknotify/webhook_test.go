package slacknotify

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/olaola-chat/ps-devtools-mcp/internal/testdeploy"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestWebhookSendsSlackPayload(t *testing.T) {
	var gotBody string
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(body)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}
	webhook, err := NewWebhook("https://hooks.slack.com/services/T/B/S", client)
	if err != nil {
		t.Fatal(err)
	}
	if err := webhook.Notify(context.Background(), testdeploy.DeploymentNotification{
		Status: testdeploy.DeploymentSucceeded, Service: "psl-be-partystar", Processes: []string{"http"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"【部署成功】", "新版本已上线", "psl-be-partystar", "http"} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("payload = %q, want %q", gotBody, want)
		}
	}
}

func TestNewWebhookAcceptsSlackWorkflowTrigger(t *testing.T) {
	if _, err := NewWebhook("https://hooks.slack.com/triggers/T/B/S", http.DefaultClient); err != nil {
		t.Fatalf("expected Slack Workflow trigger URL to be accepted: %v", err)
	}
}

func TestNewWebhookRejectsNonSlackURL(t *testing.T) {
	if _, err := NewWebhook("https://example.com/hook", http.DefaultClient); err == nil {
		t.Fatal("expected non-Slack webhook URL error")
	}
}

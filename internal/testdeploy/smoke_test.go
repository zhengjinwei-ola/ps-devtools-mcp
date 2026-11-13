package testdeploy

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSmokeCheckerProbesConfiguredServicesOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/bad":
			writer.WriteHeader(http.StatusBadGateway)
		case "/moved":
			writer.WriteHeader(http.StatusFound)
		default:
			writer.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	checker := &SmokeChecker{
		services: map[string]SmokeEndpoint{
			"healthy":   {URL: server.URL + "/ok"},
			"unhealthy": {URL: server.URL + "/bad", ExpectStatus: []int{200}},
			"redirect":  {URL: server.URL + "/moved", ExpectStatus: []int{302}},
		},
		client: server.Client(),
	}
	if summary, configured := checker.Check(context.Background(), "healthy"); !configured || !summary.OK || summary.StatusCode != 200 {
		t.Fatalf("healthy summary = %#v", summary)
	}
	if summary, configured := checker.Check(context.Background(), "unhealthy"); !configured || summary.OK || summary.Error == "" {
		t.Fatalf("unhealthy summary = %#v", summary)
	}
	if summary, configured := checker.Check(context.Background(), "redirect"); !configured || !summary.OK {
		t.Fatalf("expected-status summary = %#v", summary)
	}
	if summary, configured := checker.Check(context.Background(), "unconfigured"); configured || summary.URL != "" {
		t.Fatalf("unconfigured summary = %#v", summary)
	}
	if summary, _ := checker.Check(context.Background(), "nohost"); summary.OK {
		t.Fatalf("unreachable summary = %#v", summary)
	}
}

func TestLoadSmokeConfigValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smoke.json")
	if err := os.WriteFile(path, []byte(`{"services":{"a":{"url":"http://127.0.0.1/healthz","expect_status":[200],"timeout_ms":5000}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	checker, err := LoadSmokeConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, configured := checker.Check(context.Background(), "a"); !configured {
		t.Fatal("configured service not found")
	}
	if err := os.WriteFile(path, []byte(`{"services":{"a":{"url":"ftp://x/y"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSmokeConfig(path); err == nil {
		t.Fatal("non-http scheme unexpectedly accepted")
	}
	if err := os.WriteFile(path, []byte(`{"services":{"a":{"url":"http://127.0.0.1/h"},"timeout_ms":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSmokeConfig(path); err == nil {
		t.Fatal("unknown field unexpectedly accepted")
	}
}

func TestDeployFailsTerminalWhenSmokeCheckFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	runner := &controlledRunner{started: make(chan struct{}), release: make(chan struct{})}
	checker := &SmokeChecker{services: map[string]SmokeEndpoint{"psl-be-room": {URL: server.URL}}, client: server.Client()}
	manager := NewManagerWithHistory(
		NewServiceWithOptions(runner, nil, checker, log.New(io.Discard, "", 0)),
		filepath.Join(t.TempDir(), "history.jsonl"), log.New(io.Discard, "", 0),
	)
	started, err := manager.Start(context.Background(), DeploymentInput{Service: "psl-be-room", Processes: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	close(runner.release)
	task := waitForTerminalTask(t, manager, started.DeploymentID)
	if task.Status != DeploymentDeployedUnhealthy || task.Smoke == nil || task.Smoke.OK || task.Smoke.StatusCode != http.StatusBadGateway {
		t.Fatalf("task = %#v", task)
	}
}

func TestDeployStaysSucceededWhenSmokePassesOrSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for _, smoke := range []*SmokeChecker{
		{services: map[string]SmokeEndpoint{"psl-be-room": {URL: server.URL}}, client: server.Client()},
		nil, // 未配置冒烟检查：行为与既有部署一致。
	} {
		runner := &controlledRunner{started: make(chan struct{}), release: make(chan struct{})}
		manager := NewManager(NewServiceWithOptions(runner, nil, smoke, log.New(io.Discard, "", 0)))
		started, err := manager.Start(context.Background(), DeploymentInput{Service: "psl-be-room", Processes: []string{"http"}})
		if err != nil {
			t.Fatal(err)
		}
		<-runner.started
		close(runner.release)
		task := waitForTerminalTask(t, manager, started.DeploymentID)
		if task.Status != DeploymentSucceeded {
			t.Fatalf("task = %#v", task)
		}
	}
}

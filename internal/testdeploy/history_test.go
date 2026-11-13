package testdeploy

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryStorePersistsAndReloadsTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history", "deploy-history.jsonl")
	store := newHistoryStore(path, 50)
	task := DeploymentTask{
		DeploymentID: "dep_persisted", Service: "psl-be-partystar", Processes: []string{"rpc"},
		Status: DeploymentSucceeded, Stage: "completed", Revision: "abc1234",
		Slack:     map[string]NotificationDelivery{"succeeded": {Status: "sent"}},
		StartedAt: time.Now().UTC().Add(-time.Minute), Logs: []string{"部署成功"},
	}
	completed := time.Now().UTC()
	task.CompletedAt = &completed
	if err := store.Append(task); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].DeploymentID != "dep_persisted" || loaded[0].Status != DeploymentSucceeded {
		t.Fatalf("loaded = %#v", loaded)
	}
	if len(loaded[0].Logs) != 1 || loaded[0].Slack["succeeded"].Status != "sent" {
		t.Fatalf("round trip lost fields: %#v", loaded[0])
	}
	if err := store.Append(DeploymentTask{DeploymentID: "dep_running", Status: DeploymentCompiling}); err == nil {
		t.Fatal("non-terminal task unexpectedly persisted")
	}
}

func TestHistoryStoreLoadTrimsOversizedFileAndSkipsCorruptLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy-history.jsonl")
	var lines []string
	lines = append(lines, "{corrupt json")
	for index := range 60 {
		lines = append(lines, `{"deployment_id":"dep_`+fmt.Sprintf("%03d", index)+`","service":"psl-be-room","processes":["http"],"status":"succeeded","stage":"completed","slack":{},"started_at":"2026-11-13T00:00:00Z"}`)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newHistoryStore(path, 50)
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 50 {
		t.Fatalf("loaded %d tasks, want 50", len(loaded))
	}
	if loaded[0].DeploymentID != "dep_010" || loaded[49].DeploymentID != "dep_059" {
		t.Fatalf("unexpected trim boundaries: first=%s last=%s", loaded[0].DeploymentID, loaded[49].DeploymentID)
	}
	// The oversized file must have been rewritten down to the limit.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 50 {
		t.Fatalf("file holds %d lines, want 50", strings.Count(string(data), "\n"))
	}
}

func TestManagerHistorySurvivesRestart(t *testing.T) {
	runner := &controlledRunner{started: make(chan struct{}), release: make(chan struct{})}
	path := filepath.Join(t.TempDir(), "deploy-history.jsonl")
	manager := NewManagerWithHistory(NewServiceWithRunner(runner, log.New(io.Discard, "", 0)), path, log.New(io.Discard, "", 0))
	started, err := manager.Start(context.Background(), DeploymentInput{Service: "psl-be-partystar", Processes: []string{"rpc"}})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	close(runner.release)
	task := waitForTerminalTask(t, manager, started.DeploymentID)
	if task.Status != DeploymentSucceeded {
		t.Fatalf("task = %#v", task)
	}

	// 模拟进程重启：同一文件重建 Manager，历史应可查且内存与落盘去重。
	restarted := NewManagerWithHistory(NewServiceWithRunner(runner, log.New(io.Discard, "", 0)), path, log.New(io.Discard, "", 0))
	recent, err := restarted.Recent(context.Background(), RecentDeploymentsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recent.Deployments) != 1 || recent.Deployments[0].DeploymentID != started.DeploymentID {
		t.Fatalf("recent = %#v", recent.Deployments)
	}
	output, err := restarted.Get(context.Background(), DeploymentIDInput{DeploymentID: started.DeploymentID})
	if err != nil || output.Deployment.Status != DeploymentSucceeded {
		t.Fatalf("get after restart = %#v, error = %v", output.Deployment, err)
	}
}

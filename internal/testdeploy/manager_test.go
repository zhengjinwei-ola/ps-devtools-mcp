package testdeploy

import (
	"context"
	"io"
	"log"
	"sync"
	"testing"
	"time"
)

type controlledRunner struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	output    string
	err       error
}

func (r *controlledRunner) Run(ctx context.Context, onDeploying func(), _ ...string) (string, error) {
	r.startOnce.Do(func() { close(r.started) })
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-r.release:
	}
	if onDeploying != nil {
		onDeploying()
	}
	return r.output, r.err
}

type successfulNotifier struct{}

func (successfulNotifier) Notify(context.Context, DeploymentNotification) error { return nil }

func TestManagerTracksStructuredDeploymentAndSlackDelivery(t *testing.T) {
	runner := &controlledRunner{
		started: make(chan struct{}), release: make(chan struct{}),
		output: "[deploy-server] source revision: 81719e5c871e92953f82f976ed7fe5a983d9c7fc\n" +
			"[deploy-server] restarting go.ps_rpc (1/5)\n" +
			"[deploy-server] restarting go.ps_rpc (2/5)\n" +
			"go.ps_rpc RUNNING pid 2796945, uptime 0:00:01\n",
	}
	service := NewServiceWithRunnerAndNotifier(runner, successfulNotifier{}, log.New(io.Discard, "", 0))
	manager := NewManager(service)
	started, err := manager.Start(context.Background(), DeploymentInput{Service: "psl-be-partystar", Processes: []string{"rpc"}, SkipTests: true})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	close(runner.release)
	task := waitForTerminalTask(t, manager, started.DeploymentID)
	if task.Status != DeploymentSucceeded || task.Revision != "81719e5c871e92953f82f976ed7fe5a983d9c7fc" {
		t.Fatalf("task = %#v", task)
	}
	if len(task.Results) != 1 || task.Results[0].RestartAttempts != 2 || task.Results[0].PID != 2796945 {
		t.Fatalf("results = %#v", task.Results)
	}
	for _, status := range []DeploymentStatus{DeploymentCompiling, DeploymentDeploying, DeploymentSucceeded} {
		if task.Slack[string(status)].Status != "sent" {
			t.Fatalf("Slack[%s] = %#v", status, task.Slack[string(status)])
		}
	}
}

func TestManagerRejectsConcurrentDeploymentAndCancelsRunningTask(t *testing.T) {
	runner := &controlledRunner{started: make(chan struct{}), release: make(chan struct{})}
	manager := NewManager(NewServiceWithRunner(runner, log.New(io.Discard, "", 0)))
	started, err := manager.Start(context.Background(), DeploymentInput{Service: "psl-be-partystar", Processes: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	if _, err := manager.Start(context.Background(), DeploymentInput{Service: "psl-be-partystar", Processes: []string{"rpc"}}); err == nil {
		t.Fatal("expected concurrent deployment rejection")
	}
	canceled, err := manager.Cancel(context.Background(), DeploymentIDInput{DeploymentID: started.DeploymentID})
	if err != nil || !canceled.Canceled {
		t.Fatalf("cancel = %#v, error = %v", canceled, err)
	}
	task := waitForTerminalTask(t, manager, started.DeploymentID)
	if task.Status != DeploymentCanceled {
		t.Fatalf("task = %#v", task)
	}
}

func TestManagerCloseCancelsAndWaitsForRunningTask(t *testing.T) {
	runner := &controlledRunner{started: make(chan struct{}), release: make(chan struct{})}
	manager := NewManager(NewServiceWithRunner(runner, log.New(io.Discard, "", 0)))
	started, err := manager.Start(context.Background(), DeploymentInput{Service: "psl-be-partystar", Processes: []string{"rpc"}})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	output, err := manager.Get(context.Background(), DeploymentIDInput{DeploymentID: started.DeploymentID})
	if err != nil {
		t.Fatal(err)
	}
	if output.Deployment.Status != DeploymentCanceled {
		t.Fatalf("task = %#v", output.Deployment)
	}
}

func TestParseProcessStatuses(t *testing.T) {
	statuses := parseProcessStatuses("go.ps_http RUNNING pid 123, uptime 0:01:00\ngo.ps_rpc BACKOFF Exited too quickly\n")
	if len(statuses) != 2 || statuses[0].PID != 123 || statuses[1].State != "BACKOFF" {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func waitForTerminalTask(t *testing.T, manager *Manager, id string) DeploymentTask {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		output, err := manager.Get(context.Background(), DeploymentIDInput{DeploymentID: id})
		if err != nil {
			t.Fatal(err)
		}
		if isTerminal(output.Deployment.Status) {
			return output.Deployment
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("deployment did not become terminal")
	return DeploymentTask{}
}

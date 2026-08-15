package testdeploy

import (
	"context"
	"errors"
	"io"
	"log"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	args             []string
	output           string
	err              error
	reachDeployPhase bool
}

type fakeNotifier struct {
	events []DeploymentNotification
	err    error
}

func (n *fakeNotifier) Notify(_ context.Context, event DeploymentNotification) error {
	n.events = append(n.events, event)
	return n.err
}

func (r *fakeRunner) Run(_ context.Context, onDeploying func(), args ...string) (string, error) {
	r.args = append([]string(nil), args...)
	if r.reachDeployPhase && onDeploying != nil {
		onDeploying()
	}
	if r.output == "" && r.err == nil {
		return "go.ps_http\ngo.ps_rpc\n", nil
	}
	return r.output, r.err
}

func TestCommandRunnerIncludesOutputInError(t *testing.T) {
	runner := commandRunner{path: "/bin/sh"}
	_, err := runner.Run(context.Background(), nil, "-c", "printf 'git fetch failed' >&2; exit 128")
	if err == nil {
		t.Fatal("expected command failure")
	}
	if !strings.Contains(err.Error(), "exit status 128") || !strings.Contains(err.Error(), "git fetch failed") {
		t.Fatalf("error = %q, want exit status and command output", err)
	}
}

func TestCommandRunnerReportsDeployPhaseMarker(t *testing.T) {
	reached := false
	runner := commandRunner{path: "/bin/sh"}
	_, err := runner.Run(context.Background(), func() { reached = true }, "-c", "printf '[deploy-server] phase=deploying\\n'")
	if err != nil {
		t.Fatal(err)
	}
	if !reached {
		t.Fatal("expected deployment phase callback")
	}
}

func TestDeployReturnsCommandOutputOnFailure(t *testing.T) {
	runner := &fakeRunner{output: "remote failure\n", err: errors.New("exit status 128")}
	service := NewServiceWithRunner(runner, log.New(io.Discard, "", 0))
	output, err := service.Deploy(context.Background(), DeploymentInput{
		Service: "psl-be-partystar", Processes: []string{"http"},
	})
	if err == nil {
		t.Fatal("expected deployment failure")
	}
	if output.Output != "remote failure\n" {
		t.Fatalf("output = %q, want runner output", output.Output)
	}
}

func TestDeployNotifiesCompilingDeployingAndSucceeded(t *testing.T) {
	runner := &fakeRunner{output: "deployed\n", reachDeployPhase: true}
	notifier := &fakeNotifier{}
	service := NewServiceWithRunnerAndNotifier(runner, notifier, log.New(io.Discard, "", 0))
	_, err := service.Deploy(context.Background(), DeploymentInput{
		Service: "psl-be-partystar", Processes: []string{"http"}, SkipTests: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notifier.events) != 3 || notifier.events[0].Status != DeploymentCompiling || notifier.events[1].Status != DeploymentDeploying || notifier.events[2].Status != DeploymentSucceeded {
		t.Fatalf("events = %#v", notifier.events)
	}
	if notifier.events[2].Duration < 0 {
		t.Fatalf("duration = %s", notifier.events[2].Duration)
	}
}

func TestDeployFailureNotificationDoesNotReplaceDeployError(t *testing.T) {
	deployErr := errors.New("exit status 128")
	runner := &fakeRunner{output: "remote failure\n", err: deployErr}
	notifier := &fakeNotifier{err: errors.New("Slack unavailable")}
	service := NewServiceWithRunnerAndNotifier(runner, notifier, log.New(io.Discard, "", 0))
	_, err := service.Deploy(context.Background(), DeploymentInput{
		Service: "psl-be-partystar", Processes: []string{"rpc"},
	})
	if !errors.Is(err, deployErr) {
		t.Fatalf("error = %v, want deployment error", err)
	}
	if len(notifier.events) != 2 || notifier.events[1].Status != DeploymentCompileFailed {
		t.Fatalf("events = %#v", notifier.events)
	}
}

func TestDeployNotifiesDeploymentFailureAfterDeployPhase(t *testing.T) {
	runner := &fakeRunner{output: "restart failed\n", err: errors.New("exit status 1"), reachDeployPhase: true}
	notifier := &fakeNotifier{}
	service := NewServiceWithRunnerAndNotifier(runner, notifier, log.New(io.Discard, "", 0))
	_, err := service.Deploy(context.Background(), DeploymentInput{Service: "psl-be-partystar", Processes: []string{"http"}})
	if err == nil {
		t.Fatal("expected deployment failure")
	}
	if len(notifier.events) != 3 || notifier.events[2].Status != DeploymentFailed {
		t.Fatalf("events = %#v", notifier.events)
	}
}

func TestDeployBuildsOnlyValidatedArguments(t *testing.T) {
	runner := &fakeRunner{}
	service := NewServiceWithRunner(runner, log.New(io.Discard, "", 0))
	_, err := service.Deploy(context.Background(), DeploymentInput{
		Service: "psl-be-partystar", Processes: []string{"http", "cmd.activity"}, SkipTests: true, KeepBackups: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"deploy", "psl-be-partystar", "http", "cmd.activity", "--skip-tests", "--keep-backups", "5"}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("args = %#v, want %#v", runner.args, want)
	}
}

func TestDeployRejectsUnapprovedInput(t *testing.T) {
	tests := []DeploymentInput{
		{Service: "other", Processes: []string{"http"}},
		{Service: "psl-be-partystar", Processes: []string{"../../bin/sh"}},
		{Service: "psl-be-partystar", Processes: []string{"http"}, KeepBackups: 21},
	}
	for _, input := range tests {
		runner := &fakeRunner{}
		service := NewServiceWithRunner(runner, log.New(io.Discard, "", 0))
		if _, err := service.Deploy(context.Background(), input); err == nil {
			t.Fatalf("input %#v was unexpectedly accepted", input)
		}
		if runner.args != nil {
			t.Fatalf("runner called for rejected input %#v", input)
		}
	}
}

func TestListProcessesParsesLines(t *testing.T) {
	runner := &fakeRunner{}
	service := NewServiceWithRunner(runner, log.New(io.Discard, "", 0))
	output, err := service.ListProcesses(context.Background(), ProcessesInput{Service: "psl-be-partystar"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"go.ps_http", "go.ps_rpc"}; !reflect.DeepEqual(output.Processes, want) {
		t.Fatalf("processes = %#v, want %#v", output.Processes, want)
	}
}

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
	args   []string
	output string
	err    error
}

func (r *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	r.args = append([]string(nil), args...)
	if r.output == "" && r.err == nil {
		return "go.ps_http\ngo.ps_rpc\n", nil
	}
	return r.output, r.err
}

func TestCommandRunnerIncludesOutputInError(t *testing.T) {
	runner := commandRunner{path: "/bin/sh"}
	_, err := runner.Run(context.Background(), "-c", "printf 'git fetch failed' >&2; exit 128")
	if err == nil {
		t.Fatal("expected command failure")
	}
	if !strings.Contains(err.Error(), "exit status 128") || !strings.Contains(err.Error(), "git fetch failed") {
		t.Fatalf("error = %q, want exit status and command output", err)
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

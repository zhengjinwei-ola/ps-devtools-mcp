package testdeploy

import (
	"context"
	"io"
	"log"
	"reflect"
	"testing"
)

type fakeRunner struct {
	args []string
}

func (r *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	r.args = append([]string(nil), args...)
	return "go.ps_http\ngo.ps_rpc\n", nil
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

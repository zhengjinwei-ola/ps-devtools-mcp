package testdeploy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	defaultScriptPath = "/home/ecs-user/sh/deploy-server.sh"
	maxOutputBytes    = 64 << 10
)

var selectorPattern = regexp.MustCompile(`^(?:all|http|rpc|cmd\.[A-Za-z0-9_-]+|go\.ps_(?:http|rpc|cmd\.[A-Za-z0-9_-]+))$`)

type ListServicesInput struct{}
type ListServicesOutput struct {
	Services []string `json:"services"`
}
type ProcessesInput struct {
	Service string `json:"service" jsonschema:"Allowlisted test service name."`
}
type ProcessesOutput struct {
	Processes []string `json:"processes"`
}
type DeploymentInput struct {
	Service     string   `json:"service" jsonschema:"Allowlisted test service name."`
	Processes   []string `json:"processes" jsonschema:"One or more allowlisted process selectors."`
	SkipTests   bool     `json:"skip_tests,omitempty" jsonschema:"Skip repository tests only when known historical failures have been reviewed."`
	KeepBackups int      `json:"keep_backups,omitempty" jsonschema:"Successful backups to retain; zero uses the server default of 3."`
}
type CommandOutput struct {
	Output string `json:"output"`
}

type Runner interface {
	Run(context.Context, ...string) (string, error)
}

type DeploymentStatus string

const (
	DeploymentStarted   DeploymentStatus = "started"
	DeploymentSucceeded DeploymentStatus = "succeeded"
	DeploymentFailed    DeploymentStatus = "failed"
	notificationTimeout                  = 5 * time.Second
)

type DeploymentNotification struct {
	Status    DeploymentStatus
	Service   string
	Processes []string
	SkipTests bool
	Duration  time.Duration
}

type DeploymentNotifier interface {
	Notify(context.Context, DeploymentNotification) error
}

type commandRunner struct{ path string }

func (r commandRunner) Run(ctx context.Context, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, r.path, args...).CombinedOutput()
	if len(output) > maxOutputBytes {
		output = output[len(output)-maxOutputBytes:]
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return "", fmt.Errorf("deploy command failed: %w", err)
		}
		// MCP error responses do not serialize CommandOutput. Include the bounded
		// command output in the error so callers can diagnose failed deployments.
		return string(output), fmt.Errorf("deploy command failed: %w\n%s", err, message)
	}
	return string(output), nil
}

type Service struct {
	runner   Runner
	logger   *log.Logger
	notifier DeploymentNotifier
}

func NewService(logger *log.Logger) *Service {
	return &Service{runner: commandRunner{path: defaultScriptPath}, logger: logger}
}

func NewServiceWithRunner(runner Runner, logger *log.Logger) *Service {
	return &Service{runner: runner, logger: logger}
}

func NewServiceWithNotifier(notifier DeploymentNotifier, logger *log.Logger) *Service {
	return &Service{runner: commandRunner{path: defaultScriptPath}, notifier: notifier, logger: logger}
}

func NewServiceWithRunnerAndNotifier(runner Runner, notifier DeploymentNotifier, logger *log.Logger) *Service {
	return &Service{runner: runner, notifier: notifier, logger: logger}
}

func (s *Service) ListServices(ctx context.Context, _ ListServicesInput) (ListServicesOutput, error) {
	output, err := s.runner.Run(ctx, "list")
	if err != nil {
		return ListServicesOutput{}, err
	}
	return ListServicesOutput{Services: nonEmptyLines(output)}, nil
}

func (s *Service) ListProcesses(ctx context.Context, input ProcessesInput) (ProcessesOutput, error) {
	if err := validateService(input.Service); err != nil {
		return ProcessesOutput{}, err
	}
	output, err := s.runner.Run(ctx, "processes", input.Service)
	if err != nil {
		return ProcessesOutput{}, err
	}
	return ProcessesOutput{Processes: nonEmptyLines(output)}, nil
}

func (s *Service) Plan(ctx context.Context, input DeploymentInput) (CommandOutput, error) {
	return s.runDeploymentCommand(ctx, "plan", input)
}

func (s *Service) Deploy(ctx context.Context, input DeploymentInput) (CommandOutput, error) {
	if err := validateDeployment(input); err != nil {
		return CommandOutput{}, err
	}
	startedAt := time.Now()
	s.notify(DeploymentNotification{Status: DeploymentStarted, Service: input.Service, Processes: input.Processes, SkipTests: input.SkipTests})
	output, err := s.runDeploymentCommand(ctx, "deploy", input)
	status := DeploymentSucceeded
	if err != nil {
		status = DeploymentFailed
	}
	s.notify(DeploymentNotification{Status: status, Service: input.Service, Processes: input.Processes, SkipTests: input.SkipTests, Duration: time.Since(startedAt)})
	return output, err
}

func (s *Service) notify(event DeploymentNotification) {
	if s.notifier == nil {
		return
	}
	// Notification is best-effort and must never change the deployment result.
	ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
	defer cancel()
	if err := s.notifier.Notify(ctx, event); err != nil {
		s.logger.Printf("slack_deploy_notification status=%q service=%q error=%q", event.Status, event.Service, err)
	}
}

func (s *Service) runDeploymentCommand(ctx context.Context, action string, input DeploymentInput) (CommandOutput, error) {
	if err := validateDeployment(input); err != nil {
		return CommandOutput{}, err
	}
	args := []string{action, input.Service}
	args = append(args, input.Processes...)
	if input.SkipTests {
		args = append(args, "--skip-tests")
	}
	if input.KeepBackups > 0 {
		args = append(args, "--keep-backups", fmt.Sprint(input.KeepBackups))
	}
	s.logger.Printf("tool=%s_test_deployment service=%q processes=%q skip_tests=%t keep_backups=%d", action, input.Service, input.Processes, input.SkipTests, input.KeepBackups)
	output, err := s.runner.Run(ctx, args...)
	return CommandOutput{Output: output}, err
}

func validateService(service string) error {
	if service != "psl-be-partystar" {
		return errors.New("service is not allowlisted")
	}
	return nil
}

func validateDeployment(input DeploymentInput) error {
	if err := validateService(input.Service); err != nil {
		return err
	}
	if len(input.Processes) == 0 {
		return errors.New("at least one process is required")
	}
	for _, process := range input.Processes {
		if !selectorPattern.MatchString(process) {
			return fmt.Errorf("unsupported process selector: %s", process)
		}
	}
	if input.KeepBackups < 0 || input.KeepBackups > 20 {
		return errors.New("keep_backups must be between 0 and 20")
	}
	return nil
}

func nonEmptyLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

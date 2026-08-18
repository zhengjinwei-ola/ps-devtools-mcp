package testdeploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultScriptPath = "/home/ecs-user/sh/deploy-server.sh"
	maxOutputBytes    = 64 << 10
	deployPhaseMarker = "[deploy-server] phase=deploying"
)

var selectorPattern = regexp.MustCompile(`^(?:all|http|rpc|cmd\.[A-Za-z0-9_.-]+|go\.ps_(?:http|rpc|cmd\.[A-Za-z0-9_-]+)|room\.(?:http|rpc|cmd\.[A-Za-z0-9_.-]+))$`)

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
	SkipTests   bool     `json:"skip_tests,omitempty" jsonschema:"Deprecated compatibility field; PSL 004 deployments do not run repository tests."`
	KeepBackups int      `json:"keep_backups,omitempty" jsonschema:"Successful backups to retain; zero uses the server default of 3."`
}
type CommandOutput struct {
	Output string `json:"output"`
}

type ProcessActionInput struct {
	Service   string   `json:"service" jsonschema:"Allowlisted test service name."`
	Processes []string `json:"processes" jsonschema:"One or more allowlisted process selectors."`
}

type ProcessStatus struct {
	Process string `json:"process"`
	State   string `json:"state"`
	PID     int    `json:"pid,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type ProcessStatusOutput struct {
	Processes []ProcessStatus `json:"processes"`
}

type RestartProcessesOutput struct {
	Processes []ProcessDeploymentResult `json:"processes"`
}

type Runner interface {
	Run(context.Context, func(), ...string) (string, error)
}

type DeploymentStatus string

const (
	DeploymentCompiling     DeploymentStatus = "compiling"
	DeploymentDeploying     DeploymentStatus = "deploying"
	DeploymentSucceeded     DeploymentStatus = "succeeded"
	DeploymentCompileFailed DeploymentStatus = "compile_failed"
	DeploymentFailed        DeploymentStatus = "failed"
	DeploymentCanceled      DeploymentStatus = "canceled"
	notificationTimeout                      = 5 * time.Second
)

type DeploymentNotification struct {
	Status    DeploymentStatus
	Service   string
	Processes []string
	SkipTests bool
	Duration  time.Duration
}

type NotificationDelivery struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type DeploymentEvent struct {
	Notification DeploymentNotification
	Delivery     NotificationDelivery
}

type DeploymentNotifier interface {
	Notify(context.Context, DeploymentNotification) error
}

type commandRunner struct{ path string }

func (r commandRunner) Run(ctx context.Context, onDeploying func(), args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	capture := &commandOutputCapture{onDeploying: onDeploying}
	cmd.Stdout = capture
	cmd.Stderr = capture
	err := cmd.Run()
	output := capture.Bytes()
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

type commandOutputCapture struct {
	mu          sync.Mutex
	buffer      bytes.Buffer
	onDeploying func()
	deployOnce  sync.Once
}

func (c *commandOutputCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	n, err := c.buffer.Write(p)
	deploying := c.onDeploying != nil && bytes.Contains(c.buffer.Bytes(), []byte(deployPhaseMarker))
	c.mu.Unlock()
	if deploying {
		c.deployOnce.Do(c.onDeploying)
	}
	return n, err
}

func (c *commandOutputCapture) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buffer.Bytes()...)
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
	output, err := s.runner.Run(ctx, nil, "list")
	if err != nil {
		return ListServicesOutput{}, err
	}
	return ListServicesOutput{Services: nonEmptyLines(output)}, nil
}

func (s *Service) ListProcesses(ctx context.Context, input ProcessesInput) (ProcessesOutput, error) {
	if err := validateService(input.Service); err != nil {
		return ProcessesOutput{}, err
	}
	output, err := s.runner.Run(ctx, nil, "processes", input.Service)
	if err != nil {
		return ProcessesOutput{}, err
	}
	return ProcessesOutput{Processes: nonEmptyLines(output)}, nil
}

func (s *Service) Plan(ctx context.Context, input DeploymentInput) (CommandOutput, error) {
	return s.runDeploymentCommand(ctx, "plan", input)
}

func (s *Service) Status(ctx context.Context, input ProcessActionInput) (ProcessStatusOutput, error) {
	if err := validateProcessAction(input); err != nil {
		return ProcessStatusOutput{}, err
	}
	output, err := s.runner.Run(ctx, nil, append([]string{"status", input.Service}, input.Processes...)...)
	if err != nil {
		return ProcessStatusOutput{}, err
	}
	return ProcessStatusOutput{Processes: parseProcessStatuses(output)}, nil
}

func (s *Service) Restart(ctx context.Context, input ProcessActionInput) (RestartProcessesOutput, error) {
	if err := validateProcessAction(input); err != nil {
		return RestartProcessesOutput{}, err
	}
	output, err := s.runner.Run(ctx, nil, append([]string{"restart", input.Service}, input.Processes...)...)
	_, results := parseDeploymentOutput(output)
	return RestartProcessesOutput{Processes: results}, err
}

func (s *Service) Deploy(ctx context.Context, input DeploymentInput) (CommandOutput, error) {
	return s.deploy(ctx, input, nil)
}

func (s *Service) deploy(ctx context.Context, input DeploymentInput, observe func(DeploymentEvent)) (CommandOutput, error) {
	if err := validateDeployment(input); err != nil {
		return CommandOutput{}, err
	}
	startedAt := time.Now()
	s.notifyAndObserve(DeploymentNotification{Status: DeploymentCompiling, Service: input.Service, Processes: input.Processes, SkipTests: input.SkipTests}, observe)
	deploying := false
	output, err := s.runDeploymentCommand(ctx, "deploy", input, func() {
		deploying = true
		s.notifyAndObserve(DeploymentNotification{Status: DeploymentDeploying, Service: input.Service, Processes: input.Processes, SkipTests: input.SkipTests}, observe)
	})
	status := DeploymentSucceeded
	if errors.Is(ctx.Err(), context.Canceled) {
		status = DeploymentCanceled
	} else if err != nil {
		status = DeploymentCompileFailed
		if deploying {
			status = DeploymentFailed
		}
	}
	s.notifyAndObserve(DeploymentNotification{Status: status, Service: input.Service, Processes: input.Processes, SkipTests: input.SkipTests, Duration: time.Since(startedAt)}, observe)
	return output, err
}

func (s *Service) notifyAndObserve(event DeploymentNotification, observe func(DeploymentEvent)) {
	delivery := s.notify(event)
	if observe != nil {
		observe(DeploymentEvent{Notification: event, Delivery: delivery})
	}
}

func (s *Service) notify(event DeploymentNotification) NotificationDelivery {
	if s.notifier == nil {
		return NotificationDelivery{Status: "disabled"}
	}
	// Notification is best-effort and must never change the deployment result.
	ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
	defer cancel()
	if err := s.notifier.Notify(ctx, event); err != nil {
		s.logger.Printf("slack_deploy_notification status=%q service=%q error=%q", event.Status, event.Service, err)
		return NotificationDelivery{Status: "failed", Error: err.Error()}
	}
	return NotificationDelivery{Status: "sent"}
}

func (s *Service) runDeploymentCommand(ctx context.Context, action string, input DeploymentInput, onDeploying ...func()) (CommandOutput, error) {
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
	var phaseCallback func()
	if len(onDeploying) > 0 {
		phaseCallback = onDeploying[0]
	}
	output, err := s.runner.Run(ctx, phaseCallback, args...)
	return CommandOutput{Output: output}, err
}

func validateService(service string) error {
	if service != "psl-be-partystar" && service != "psl-be-room" {
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

func validateProcessAction(input ProcessActionInput) error {
	return validateDeployment(DeploymentInput{Service: input.Service, Processes: input.Processes})
}

func parseProcessStatuses(output string) []ProcessStatus {
	var statuses []ProcessStatus
	for _, line := range nonEmptyLines(output) {
		fields := strings.Fields(line)
		if len(fields) < 2 || !selectorPattern.MatchString(fields[0]) {
			continue
		}
		status := ProcessStatus{Process: fields[0], State: fields[1]}
		if status.State == "RUNNING" && len(fields) >= 4 && fields[2] == "pid" {
			status.PID, _ = strconv.Atoi(strings.TrimSuffix(fields[3], ","))
		}
		if len(fields) > 2 {
			status.Detail = strings.Join(fields[2:], " ")
		}
		statuses = append(statuses, status)
	}
	return statuses
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

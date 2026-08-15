package testdeploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultHistoryLimit      = 50
	defaultDeploymentTimeout = 15 * time.Minute
	maxTaskLogLines          = 40
	maxTaskLogBytes          = 8 << 10
)

var (
	revisionLinePattern = regexp.MustCompile(`(?m)^\[deploy-server\] (?:source revision|deployment completed|failed revision): ([0-9a-f]{7,40})$`)
	restartLinePattern  = regexp.MustCompile(`(?m)^\[deploy-server\] restarting ([^ ]+) \(([1-5])/5\)$`)
	runningLinePattern  = regexp.MustCompile(`(?m)^([^ ]+)\s+RUNNING\s+pid ([0-9]+),`)
)

type StartDeploymentOutput struct {
	DeploymentID string           `json:"deployment_id"`
	Status       DeploymentStatus `json:"status"`
}

type DeploymentIDInput struct {
	DeploymentID string `json:"deployment_id" jsonschema:"Deployment ID returned by start_test_deployment."`
}

type RecentDeploymentsInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum recent deployments to return; zero uses 10 and the maximum is 50."`
}

type ProcessDeploymentResult struct {
	Process         string `json:"process"`
	State           string `json:"state,omitempty"`
	RestartAttempts int    `json:"restart_attempts,omitempty"`
	PID             int    `json:"pid,omitempty"`
}

type DeploymentTask struct {
	DeploymentID string                          `json:"deployment_id"`
	Service      string                          `json:"service"`
	Processes    []string                        `json:"processes"`
	Status       DeploymentStatus                `json:"status"`
	Stage        string                          `json:"stage"`
	Revision     string                          `json:"revision,omitempty"`
	Results      []ProcessDeploymentResult       `json:"results,omitempty"`
	DurationMS   int64                           `json:"duration_ms,omitempty"`
	Error        string                          `json:"error,omitempty"`
	Slack        map[string]NotificationDelivery `json:"slack"`
	StartedAt    time.Time                       `json:"started_at"`
	CompletedAt  *time.Time                      `json:"completed_at,omitempty"`
	Logs         []string                        `json:"logs,omitempty"`
}

type DeploymentStatusOutput struct {
	Deployment DeploymentTask `json:"deployment"`
}

type DeploymentLogsOutput struct {
	DeploymentID string           `json:"deployment_id"`
	Status       DeploymentStatus `json:"status"`
	Lines        []string         `json:"lines"`
}

type RecentDeploymentsOutput struct {
	Deployments []DeploymentTask `json:"deployments"`
}

type CancelDeploymentOutput struct {
	DeploymentID string `json:"deployment_id"`
	Canceled     bool   `json:"canceled"`
}

type managedTask struct {
	task        DeploymentTask
	cancel      context.CancelFunc
	finalStatus DeploymentStatus
}

type Manager struct {
	service *Service
	mu      sync.RWMutex
	wg      sync.WaitGroup
	tasks   map[string]*managedTask
	order   []string
	limit   int
}

func NewManager(service *Service) *Manager {
	return &Manager{service: service, tasks: make(map[string]*managedTask), limit: defaultHistoryLimit}
}

func (m *Manager) Start(_ context.Context, input DeploymentInput) (StartDeploymentOutput, error) {
	if err := validateDeployment(input); err != nil {
		return StartDeploymentOutput{}, err
	}
	id, err := newDeploymentID()
	if err != nil {
		return StartDeploymentOutput{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultDeploymentTimeout)
	task := &managedTask{task: DeploymentTask{
		DeploymentID: id, Service: input.Service, Processes: append([]string(nil), input.Processes...),
		Status: DeploymentCompiling, Stage: "compiling", Slack: make(map[string]NotificationDelivery), StartedAt: time.Now().UTC(),
		Logs: []string{"开始编译"},
	}, cancel: cancel}

	m.mu.Lock()
	for _, existing := range m.tasks {
		if existing.task.Service == input.Service && !isTerminal(existing.task.Status) {
			m.mu.Unlock()
			cancel()
			return StartDeploymentOutput{}, fmt.Errorf("deployment %s is already running for service %s", existing.task.DeploymentID, input.Service)
		}
	}
	m.tasks[id] = task
	m.order = append(m.order, id)
	m.pruneLocked()
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx, id, input)
	}()
	return StartDeploymentOutput{DeploymentID: id, Status: DeploymentCompiling}, nil
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	for _, task := range m.tasks {
		if !isTerminal(task.task.Status) && task.cancel != nil {
			task.cancel()
		}
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Get(_ context.Context, input DeploymentIDInput) (DeploymentStatusOutput, error) {
	task, err := m.snapshot(input.DeploymentID)
	return DeploymentStatusOutput{Deployment: task}, err
}

func (m *Manager) Logs(_ context.Context, input DeploymentIDInput) (DeploymentLogsOutput, error) {
	task, err := m.snapshot(input.DeploymentID)
	if err != nil {
		return DeploymentLogsOutput{}, err
	}
	return DeploymentLogsOutput{DeploymentID: task.DeploymentID, Status: task.Status, Lines: task.Logs}, nil
}

func (m *Manager) Recent(_ context.Context, input RecentDeploymentsInput) (RecentDeploymentsOutput, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > defaultHistoryLimit {
		return RecentDeploymentsOutput{}, errors.New("limit must be between 1 and 50")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]DeploymentTask, 0, limit)
	for index := len(m.order) - 1; index >= 0 && len(result) < limit; index-- {
		result = append(result, cloneTask(m.tasks[m.order[index]].task, false))
	}
	return RecentDeploymentsOutput{Deployments: result}, nil
}

func (m *Manager) Cancel(_ context.Context, input DeploymentIDInput) (CancelDeploymentOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[input.DeploymentID]
	if !ok {
		return CancelDeploymentOutput{}, errors.New("deployment not found")
	}
	if isTerminal(task.task.Status) {
		return CancelDeploymentOutput{DeploymentID: input.DeploymentID, Canceled: false}, nil
	}
	task.cancel()
	return CancelDeploymentOutput{DeploymentID: input.DeploymentID, Canceled: true}, nil
}

func (m *Manager) run(ctx context.Context, id string, input DeploymentInput) {
	startedAt := time.Now()
	output, deployErr := m.service.deploy(ctx, input, func(event DeploymentEvent) {
		m.mu.Lock()
		defer m.mu.Unlock()
		task, ok := m.tasks[id]
		if !ok {
			return
		}
		task.task.Slack[string(event.Notification.Status)] = event.Delivery
		if event.Notification.Status == DeploymentCompiling || event.Notification.Status == DeploymentDeploying {
			task.task.Status = event.Notification.Status
			task.task.Stage = string(event.Notification.Status)
		}
		if event.Notification.Status == DeploymentDeploying {
			appendTaskLog(&task.task, "编译完成，开始部署")
		}
		if isTerminal(event.Notification.Status) {
			task.finalStatus = event.Notification.Status
		}
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return
	}
	completedAt := time.Now().UTC()
	task.task.CompletedAt = &completedAt
	task.task.DurationMS = time.Since(startedAt).Milliseconds()
	task.task.Revision, task.task.Results = parseDeploymentOutput(output.Output)
	if deployErr != nil {
		task.task.Error = conciseDeployError(deployErr)
		appendTaskLog(&task.task, task.task.Error)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		task.task.Status = DeploymentCanceled
		task.task.Stage = "canceled"
		appendTaskLog(&task.task, "部署已取消")
	} else if deployErr == nil {
		task.task.Status = DeploymentSucceeded
		task.task.Stage = "completed"
		appendTaskLog(&task.task, "部署成功")
	} else if task.finalStatus != "" {
		task.task.Status = task.finalStatus
		task.task.Stage = "failed"
	}
	task.cancel = nil
}

func (m *Manager) snapshot(id string) (DeploymentTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return DeploymentTask{}, errors.New("deployment not found")
	}
	return cloneTask(task.task, true), nil
}

func (m *Manager) pruneLocked() {
	for len(m.order) > m.limit {
		id := m.order[0]
		if task := m.tasks[id]; task != nil && !isTerminal(task.task.Status) {
			return
		}
		delete(m.tasks, id)
		m.order = m.order[1:]
	}
}

func newDeploymentID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate deployment ID: %w", err)
	}
	return "dep_" + hex.EncodeToString(raw[:]), nil
}

func parseDeploymentOutput(output string) (string, []ProcessDeploymentResult) {
	revision := ""
	if matches := revisionLinePattern.FindAllStringSubmatch(output, -1); len(matches) > 0 {
		revision = matches[len(matches)-1][1]
	}
	byProcess := make(map[string]*ProcessDeploymentResult)
	var order []string
	for _, match := range restartLinePattern.FindAllStringSubmatch(output, -1) {
		result := byProcess[match[1]]
		if result == nil {
			result = &ProcessDeploymentResult{Process: match[1]}
			byProcess[match[1]] = result
			order = append(order, match[1])
		}
		result.RestartAttempts, _ = strconv.Atoi(match[2])
	}
	for _, match := range runningLinePattern.FindAllStringSubmatch(output, -1) {
		result := byProcess[match[1]]
		if result == nil {
			result = &ProcessDeploymentResult{Process: match[1]}
			byProcess[match[1]] = result
			order = append(order, match[1])
		}
		result.PID, _ = strconv.Atoi(match[2])
		result.State = "RUNNING"
	}
	results := make([]ProcessDeploymentResult, 0, len(order))
	for _, process := range order {
		results = append(results, *byProcess[process])
	}
	return revision, results
}

func conciseDeployError(err error) string {
	lines := strings.Split(err.Error(), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "spawn") || strings.Contains(lower, "did not reach") {
			return truncateText(line, 1024)
		}
	}
	return truncateText(strings.TrimSpace(lines[0]), 1024)
}

func appendTaskLog(task *DeploymentTask, line string) {
	line = truncateText(strings.TrimSpace(line), 1024)
	if line == "" {
		return
	}
	task.Logs = append(task.Logs, line)
	if len(task.Logs) > maxTaskLogLines {
		task.Logs = append([]string(nil), task.Logs[len(task.Logs)-maxTaskLogLines:]...)
	}
	for taskLogBytes(task.Logs) > maxTaskLogBytes && len(task.Logs) > 1 {
		task.Logs = task.Logs[1:]
	}
}

func taskLogBytes(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	return total
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func isTerminal(status DeploymentStatus) bool {
	switch status {
	case DeploymentSucceeded, DeploymentCompileFailed, DeploymentFailed, DeploymentCanceled:
		return true
	default:
		return false
	}
}

func cloneTask(task DeploymentTask, includeLogs bool) DeploymentTask {
	task.Processes = append([]string(nil), task.Processes...)
	task.Results = append([]ProcessDeploymentResult(nil), task.Results...)
	slack := make(map[string]NotificationDelivery, len(task.Slack))
	for status, delivery := range task.Slack {
		slack[status] = delivery
	}
	task.Slack = slack
	if includeLogs {
		task.Logs = append([]string(nil), task.Logs...)
	} else {
		task.Logs = nil
	}
	return task
}

package testlogs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Bridge struct {
	session *mcp.ClientSession
	logger  *log.Logger
	callMu  sync.Mutex
}

func Connect(ctx context.Context, command string, args []string, logger *log.Logger) (*Bridge, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("log MCP command is empty")
	}
	child := exec.Command(command, args...)
	child.Stderr = os.Stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "ps-devtools-log-bridge", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: child}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect log MCP: %w", err)
	}
	return &Bridge{session: session, logger: logger}, nil
}

func (b *Bridge) Close() error { return b.session.Close() }

func (b *Bridge) ListSources(ctx context.Context, input ListSourcesInput) (ListSourcesOutput, error) {
	return call[ListSourcesOutput](ctx, b, "list_test_log_sources", input)
}
func (b *Bridge) Search(ctx context.Context, input SearchInput) (SearchOutput, error) {
	return call[SearchOutput](ctx, b, "search_test_logs", input)
}
func (b *Bridge) Trace(ctx context.Context, input TraceInput) (TraceOutput, error) {
	return call[TraceOutput](ctx, b, "trace_test_logs", input)
}
func (b *Bridge) ListRuntimeSources(ctx context.Context, input ListRuntimeSourcesInput) (ListRuntimeSourcesOutput, error) {
	return call[ListRuntimeSourcesOutput](ctx, b, "list_test_runtime_sources", input)
}
func (b *Bridge) GetRuntime(ctx context.Context, input GetRuntimeInput) (GetRuntimeOutput, error) {
	return call[GetRuntimeOutput](ctx, b, "get_test_service_runtime", input)
}

func call[T any](ctx context.Context, bridge *Bridge, name string, input any) (T, error) {
	var zero T
	// The remote stdio bridge runs through a PTY. Serialize calls so responses
	// cannot be interleaved and parsed as trailing JSON data.
	bridge.callMu.Lock()
	defer bridge.callMu.Unlock()
	argumentsData, err := json.Marshal(input)
	if err != nil {
		return zero, fmt.Errorf("encode %s input: %w", name, err)
	}
	var arguments map[string]any
	if err := json.Unmarshal(argumentsData, &arguments); err != nil {
		return zero, fmt.Errorf("normalize %s input: %w", name, err)
	}
	result, err := bridge.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return zero, fmt.Errorf("call downstream %s: %w", name, err)
	}
	if result.IsError {
		return zero, fmt.Errorf("downstream %s rejected request", name)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return zero, fmt.Errorf("encode downstream %s output: %w", name, err)
	}
	var output T
	if err := json.Unmarshal(encoded, &output); err != nil {
		return zero, fmt.Errorf("decode downstream %s output: %w", name, err)
	}
	if bridge.logger != nil {
		bridge.logger.Printf("tool=%s downstream=psl-test-logs result=success", name)
	}
	return output, nil
}

type logService interface {
	ListSources(context.Context, ListSourcesInput) (ListSourcesOutput, error)
	Search(context.Context, SearchInput) (SearchOutput, error)
	Trace(context.Context, TraceInput) (TraceOutput, error)
	ListRuntimeSources(context.Context, ListRuntimeSourcesInput) (ListRuntimeSourcesOutput, error)
	GetRuntime(context.Context, GetRuntimeInput) (GetRuntimeOutput, error)
}

type closeableLogService interface {
	logService
	Close() error
}

type connector func(context.Context) (closeableLogService, error)

// Lazy connects to the remote log MCP only when a log or runtime tool is first
// used. Database and Redis tools can therefore start immediately, while later
// log calls reuse the same downstream session.
type Lazy struct {
	mu      sync.Mutex
	service closeableLogService
	connect connector
	closed  bool
}

func NewLazy(command string, args []string, logger *log.Logger) *Lazy {
	return newLazy(func(ctx context.Context) (closeableLogService, error) {
		return Connect(ctx, command, args, logger)
	})
}

func newLazy(connect connector) *Lazy { return &Lazy{connect: connect} }

func (l *Lazy) get(ctx context.Context) (closeableLogService, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, fmt.Errorf("test log backend is closed")
	}
	if l.service != nil {
		return l.service, nil
	}
	service, err := l.connect(ctx)
	if err != nil {
		return nil, err
	}
	l.service = service
	return service, nil
}

func (l *Lazy) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.service == nil {
		return nil
	}
	err := l.service.Close()
	l.service = nil
	return err
}

func (l *Lazy) ListSources(ctx context.Context, input ListSourcesInput) (ListSourcesOutput, error) {
	service, err := l.get(ctx)
	if err != nil {
		return ListSourcesOutput{}, err
	}
	return service.ListSources(ctx, input)
}
func (l *Lazy) Search(ctx context.Context, input SearchInput) (SearchOutput, error) {
	service, err := l.get(ctx)
	if err != nil {
		return SearchOutput{}, err
	}
	return service.Search(ctx, input)
}
func (l *Lazy) Trace(ctx context.Context, input TraceInput) (TraceOutput, error) {
	service, err := l.get(ctx)
	if err != nil {
		return TraceOutput{}, err
	}
	return service.Trace(ctx, input)
}
func (l *Lazy) ListRuntimeSources(ctx context.Context, input ListRuntimeSourcesInput) (ListRuntimeSourcesOutput, error) {
	service, err := l.get(ctx)
	if err != nil {
		return ListRuntimeSourcesOutput{}, err
	}
	return service.ListRuntimeSources(ctx, input)
}
func (l *Lazy) GetRuntime(ctx context.Context, input GetRuntimeInput) (GetRuntimeOutput, error) {
	service, err := l.get(ctx)
	if err != nil {
		return GetRuntimeOutput{}, err
	}
	return service.GetRuntime(ctx, input)
}

type Unavailable struct{}

func (Unavailable) error() error {
	return fmt.Errorf("test log backend is not configured; set PS_LOG_MCP_COMMAND and PS_LOG_MCP_ARGS_JSON")
}
func (u Unavailable) ListSources(context.Context, ListSourcesInput) (ListSourcesOutput, error) {
	return ListSourcesOutput{}, u.error()
}
func (u Unavailable) Search(context.Context, SearchInput) (SearchOutput, error) {
	return SearchOutput{}, u.error()
}
func (u Unavailable) Trace(context.Context, TraceInput) (TraceOutput, error) {
	return TraceOutput{}, u.error()
}
func (u Unavailable) ListRuntimeSources(context.Context, ListRuntimeSourcesInput) (ListRuntimeSourcesOutput, error) {
	return ListRuntimeSourcesOutput{}, u.error()
}
func (u Unavailable) GetRuntime(context.Context, GetRuntimeInput) (GetRuntimeOutput, error) {
	return GetRuntimeOutput{}, u.error()
}

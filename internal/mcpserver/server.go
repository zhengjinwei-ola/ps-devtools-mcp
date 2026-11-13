package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/olaola-chat/ps-devtools-mcp/internal/mcpinfo"
	"github.com/olaola-chat/ps-devtools-mcp/internal/processlogs"
	"github.com/olaola-chat/ps-devtools-mcp/internal/redisinspect"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testapi"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testbot"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testdb"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testdeploy"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testlogs"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testredis"
	"github.com/olaola-chat/ps-devtools-mcp/internal/usersnapshot"
)

const (
	Name    = "ps-devtools"
	Version = "v0.4.0"
)

type queryService interface {
	Query(ctx context.Context, input testdb.QueryInput) (testdb.QueryOutput, error)
	ExecDDL(ctx context.Context, input testdb.ExecDDLInput) (testdb.ExecDDLOutput, error)
}

type redisQueryService interface {
	Query(ctx context.Context, input testredis.QueryInput) (testredis.QueryOutput, error)
}

type userSnapshotService interface {
	Get(context.Context, usersnapshot.GetInput) (usersnapshot.GetOutput, error)
}

type redisInspectorService interface {
	Inspect(context.Context, redisinspect.InspectInput) (redisinspect.InspectOutput, error)
}

type readOnlyAPIService interface {
	Call(context.Context, testapi.CallInput) (testapi.CallOutput, error)
}

type testBotService interface {
	List(context.Context, testbot.ListInput) (testbot.ListOutput, error)
	Call(context.Context, testbot.CallInput) (testbot.CallOutput, error)
}

type testLogService interface {
	ListSources(context.Context, testlogs.ListSourcesInput) (testlogs.ListSourcesOutput, error)
	Search(context.Context, testlogs.SearchInput) (testlogs.SearchOutput, error)
	Trace(context.Context, testlogs.TraceInput) (testlogs.TraceOutput, error)
	ListRuntimeSources(context.Context, testlogs.ListRuntimeSourcesInput) (testlogs.ListRuntimeSourcesOutput, error)
	GetRuntime(context.Context, testlogs.GetRuntimeInput) (testlogs.GetRuntimeOutput, error)
}

type testDeployService interface {
	ListServices(context.Context, testdeploy.ListServicesInput) (testdeploy.ListServicesOutput, error)
	ListProcesses(context.Context, testdeploy.ProcessesInput) (testdeploy.ProcessesOutput, error)
	Plan(context.Context, testdeploy.DeploymentInput) (testdeploy.CommandOutput, error)
	Deploy(context.Context, testdeploy.DeploymentInput) (testdeploy.CommandOutput, error)
	Status(context.Context, testdeploy.ProcessActionInput) (testdeploy.ProcessStatusOutput, error)
	Restart(context.Context, testdeploy.ProcessActionInput) (testdeploy.RestartProcessesOutput, error)
}

type testDeploymentJobService interface {
	Start(context.Context, testdeploy.DeploymentInput) (testdeploy.StartDeploymentOutput, error)
	Get(context.Context, testdeploy.DeploymentIDInput) (testdeploy.DeploymentStatusOutput, error)
	Logs(context.Context, testdeploy.DeploymentIDInput) (testdeploy.DeploymentLogsOutput, error)
	Recent(context.Context, testdeploy.RecentDeploymentsInput) (testdeploy.RecentDeploymentsOutput, error)
	Cancel(context.Context, testdeploy.DeploymentIDInput) (testdeploy.CancelDeploymentOutput, error)
}

type mcpInfoService interface {
	Get(context.Context, mcpinfo.Input) (mcpinfo.Info, error)
}

type processLogService interface {
	Tail(context.Context, processlogs.TailInput) (processlogs.TailOutput, error)
}

type Services struct {
	DB             queryService
	Redis          redisQueryService
	UserSnapshot   userSnapshotService
	RedisInspector redisInspectorService
	ReadOnlyAPI    readOnlyAPIService
	TestBot        testBotService
	TestLogs       testLogService
	TestDeploy     testDeployService
	TestDeployJobs testDeploymentJobService
	MCPInfo        mcpInfoService
	ProcessLogs    processLogService
}

func New(services Services) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: Name, Version: Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "query_test_db",
		Description: "Run one bounded read-only SQL query against the approved PSL test xianshi (engine 1) " +
			"or config (engine 3) database. SELECT and WITH queries must include LIMIT.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdb.QueryInput) (*mcp.CallToolResult, testdb.QueryOutput, error) {
		output, err := services.DB.Query(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "exec_test_db_ddl",
		Description: "Execute ONE restricted DDL statement on the PSL test xianshi (engine 1) database only. " +
			"Only CREATE TABLE [IF NOT EXISTS] and ALTER TABLE ... ADD [COLUMN] are allowed; " +
			"DROP/DELETE/UPDATE/INSERT/RENAME/MODIFY/CHANGE and comments are rejected. Intended for applying migration SQL to the test database.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdb.ExecDDLInput) (*mcp.CallToolResult, testdb.ExecDDLOutput, error) {
		output, err := services.DB.ExecDDL(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "start_test_deployment", Description: "Start an asynchronous allowlisted deployment on PSL 004 and return a deployment ID immediately.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.DeploymentInput) (*mcp.CallToolResult, testdeploy.StartDeploymentOutput, error) {
		output, err := services.TestDeployJobs.Start(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_test_deployment_status", Description: "Get structured stage, revision, process PID, restart attempts, duration, error, and Slack delivery status for an asynchronous deployment.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.DeploymentIDInput) (*mcp.CallToolResult, testdeploy.DeploymentStatusOutput, error) {
		output, err := services.TestDeployJobs.Get(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_test_deployment_logs", Description: "Get bounded stage summaries and key errors for an asynchronous deployment without verbose build output.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.DeploymentIDInput) (*mcp.CallToolResult, testdeploy.DeploymentLogsOutput, error) {
		output, err := services.TestDeployJobs.Logs(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_recent_deployments", Description: "List recent in-memory PSL 004 deployment summaries, newest first.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.RecentDeploymentsInput) (*mcp.CallToolResult, testdeploy.RecentDeploymentsOutput, error) {
		output, err := services.TestDeployJobs.Recent(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "cancel_test_deployment", Description: "Cancel one running asynchronous PSL 004 deployment and terminate its deployment process group.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.DeploymentIDInput) (*mcp.CallToolResult, testdeploy.CancelDeploymentOutput, error) {
		output, err := services.TestDeployJobs.Cancel(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_test_process_status", Description: "Get structured Supervisor state and PID for allowlisted PSL 004 processes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.ProcessActionInput) (*mcp.CallToolResult, testdeploy.ProcessStatusOutput, error) {
		output, err := services.TestDeploy.Status(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "restart_test_process", Description: "Restart allowlisted PSL 004 Supervisor processes with bounded retry and health verification, without rebuilding.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.ProcessActionInput) (*mcp.CallToolResult, testdeploy.RestartProcessesOutput, error) {
		output, err := services.TestDeploy.Restart(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_test_deploy_services", Description: "List services allowlisted for deployment on the PSL 004 test host.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.ListServicesInput) (*mcp.CallToolResult, testdeploy.ListServicesOutput, error) {
		output, err := services.TestDeploy.ListServices(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_test_deploy_processes", Description: "List Supervisor processes available for an allowlisted service on the PSL 004 test host.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.ProcessesInput) (*mcp.CallToolResult, testdeploy.ProcessesOutput, error) {
		output, err := services.TestDeploy.ListProcesses(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "plan_test_deployment", Description: "Preview a constrained deployment of selected processes on the PSL 004 test host without changing files or processes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.DeploymentInput) (*mcp.CallToolResult, testdeploy.CommandOutput, error) {
		output, err := services.TestDeploy.Plan(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "deploy_test_service", Description: "Build and deploy selected processes of an allowlisted service on the PSL 004 test host. This is a test-environment write operation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testdeploy.DeploymentInput) (*mcp.CallToolResult, testdeploy.CommandOutput, error) {
		output, err := services.TestDeploy.Deploy(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "query_test_redis",
		Description: "Run one bounded read-only Redis command against the approved PSL test environment. " +
			"Unbounded collection commands and write commands are rejected.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testredis.QueryInput) (*mcp.CallToolResult, testredis.QueryOutput, error) {
		output, err := services.Redis.Query(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_test_user_snapshot",
		Description: "Get a privacy-minimized PSL test user snapshot by UID, including non-sensitive profile state, VIP records, " +
			"and bounded commodity, equipped-item, and prop-card backpack rows. Sections report partial errors independently.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input usersnapshot.GetInput) (*mcp.CallToolResult, usersnapshot.GetOutput, error) {
		output, err := services.UserSnapshot.Get(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:         "inspect_test_redis",
		OutputSchema: inspectRedisOutputSchema(),
		Description: "Inspect one exact PSL test Redis key with TYPE, TTL, and bounded type-aware content. " +
			"Collection reads are capped and write commands are never exposed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input redisinspect.InspectInput) (*mcp.CallToolResult, redisinspect.InspectOutput, error) {
		output, err := services.RedisInspector.Inspect(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_test_log_sources", Description: "List allowlisted PSL test log sources through the configured remote read-only backend.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testlogs.ListSourcesInput) (*mcp.CallToolResult, testlogs.ListSourcesOutput, error) {
		output, err := services.TestLogs.ListSources(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_test_logs", Description: "Search one allowlisted PSL test log source using bounded literal matching and server-side sensitive-value redaction.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testlogs.SearchInput) (*mcp.CallToolResult, testlogs.SearchOutput, error) {
		output, err := services.TestLogs.Search(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "trace_test_logs", Description: "Correlate one literal trace ID across up to eight selected PSL test log sources under a shared scan/output budget.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testlogs.TraceInput) (*mcp.CallToolResult, testlogs.TraceOutput, error) {
		output, err := services.TestLogs.Trace(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:         "call_test_readonly_api",
		OutputSchema: callReadOnlyAPIOutputSchema(),
		Description:  "Call an arbitrary HTTP or HTTPS URL from the PSL test MCP host. The URL, method, query, headers, and body are supplied by the caller; responses are size-bounded and sensitive fields are redacted.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testapi.CallInput) (*mcp.CallToolResult, testapi.CallOutput, error) {
		output, err := services.ReadOnlyAPI.Call(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_testbot_endpoints", Description: "List operator-configured PSL test endpoints available to the authenticated testbot and whether each endpoint has side effects.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testbot.ListInput) (*mcp.CallToolResult, testbot.ListOutput, error) {
		output, err := services.TestBot.List(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:         "call_testbot_api",
		OutputSchema: callTestBotAPIOutputSchema(),
		Description:  "Log in the configured PSL test user when needed, call one allowlisted user API with User-Token, and return a redacted bounded response. Endpoints marked with side effects require confirm_side_effect=true.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testbot.CallInput) (*mcp.CallToolResult, testbot.CallOutput, error) {
		output, err := services.TestBot.Call(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_test_runtime_sources", Description: "List allowlisted PSL test service runtime sources through the configured remote read-only backend.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testlogs.ListRuntimeSourcesInput) (*mcp.CallToolResult, testlogs.ListRuntimeSourcesOutput, error) {
		output, err := services.TestLogs.ListRuntimeSources(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_test_service_runtime", Description: "Inspect allowlisted PSL test service binary build/VCS metadata and non-sensitive configuration values and hashes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input testlogs.GetRuntimeInput) (*mcp.CallToolResult, testlogs.GetRuntimeOutput, error) {
		output, err := services.TestLogs.GetRuntime(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_mcp_info", Description: "Get this MCP server's own version, build revision, transport, listen address, PID and uptime.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpinfo.Input) (*mcp.CallToolResult, mcpinfo.Info, error) {
		output, err := services.MCPInfo.Get(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "tail_test_process_logs", Description: "Tail the last lines of allowlisted PSL 004 Supervisor process stdout/stderr log files. " +
			"Sources and file paths are configured server-side; lines are length-bounded and redacted.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input processlogs.TailInput) (*mcp.CallToolResult, processlogs.TailOutput, error) {
		output, err := services.ProcessLogs.Tail(ctx, input)
		return nil, output, err
	})
	return server
}

// These schemas are explicit because jsonschema-go represents interface{} as the
// boolean schema true. Some MCP clients only accept object-form property schemas,
// even though both forms are valid JSON Schema. An empty object preserves the
// existing "any JSON value" contract while remaining compatible with those clients.
func inspectRedisOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":         map[string]any{"type": "string"},
			"type":        map[string]any{"type": "string"},
			"ttl_seconds": map[string]any{"type": "integer"},
			"value":       map[string]any{},
			"next_cursor": map[string]any{"type": "string"},
			"truncated":   map[string]any{"type": "boolean"},
			"value_error": map[string]any{"type": "string"},
		},
		"required": []string{"key", "type", "ttl_seconds"},
	}
}

func callReadOnlyAPIOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":          map[string]any{"type": "string"},
			"method":       map[string]any{"type": "string"},
			"status_code":  map[string]any{"type": "integer"},
			"content_type": map[string]any{"type": "string"},
			"body":         map[string]any{},
			"elapsed_ms":   map[string]any{"type": "integer"},
			"truncated":    map[string]any{"type": "boolean"},
		},
		"required": []string{"url", "method", "status_code", "elapsed_ms"},
	}
}

func callTestBotAPIOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"endpoint":     map[string]any{"type": "string"},
			"request_id":   map[string]any{"type": "string"},
			"status_code":  map[string]any{"type": "integer"},
			"content_type": map[string]any{"type": "string"},
			"body":         map[string]any{},
			"elapsed_ms":   map[string]any{"type": "integer"},
			"truncated":    map[string]any{"type": "boolean"},
		},
		"required": []string{"endpoint", "request_id", "status_code", "elapsed_ms"},
	}
}

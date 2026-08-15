package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	Version = "v0.2.0"
)

type queryService interface {
	Query(ctx context.Context, input testdb.QueryInput) (testdb.QueryOutput, error)
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
		Name: "inspect_test_redis",
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
		Name: "call_test_readonly_api", Description: "Call one operator-configured PSL test read-only HTTP endpoint. " +
			"The endpoint, method, query keys, body fields, response size, redirects, and output redaction are constrained by server configuration.",
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
		Name: "call_testbot_api", Description: "Log in the configured PSL test user when needed, call one allowlisted user API with User-Token, and return a redacted bounded response. Endpoints marked with side effects require confirm_side_effect=true.",
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
	return server
}

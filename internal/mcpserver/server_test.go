package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/olaola-chat/ps-devtools-mcp/internal/mcpinfo"
	"github.com/olaola-chat/ps-devtools-mcp/internal/processlogs"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testdb"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testdeploy"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testredis"
)

type stubService struct {
	calls int
}

type stubRedisService struct {
	calls int
}

type stubDeployService struct{}
type stubDeploymentJobService struct{}
type stubMCPInfoService struct{}
type stubProcessLogService struct{}

func (stubMCPInfoService) Get(context.Context, mcpinfo.Input) (mcpinfo.Info, error) {
	return mcpinfo.Info{Version: "v0.4.0", Revision: "abc1234", PID: 1234}, nil
}
func (stubProcessLogService) Tail(context.Context, processlogs.TailInput) (processlogs.TailOutput, error) {
	return processlogs.TailOutput{Files: []processlogs.FileTail{{File: "/home/ecs-user/log/x.stdout.log", Lines: []string{"ok"}}}}, nil
}

func (stubProcessLogService) List(context.Context, processlogs.ListInput) (processlogs.ListOutput, error) {
	return processlogs.ListOutput{Root: "/home/ecs-user/log", Files: []processlogs.LogFileInfo{{Name: "x.stdout.log"}}, Total: 1}, nil
}

func (stubDeployService) ListServices(context.Context, testdeploy.ListServicesInput) (testdeploy.ListServicesOutput, error) {
	return testdeploy.ListServicesOutput{}, nil
}
func (stubDeployService) ListProcesses(context.Context, testdeploy.ProcessesInput) (testdeploy.ProcessesOutput, error) {
	return testdeploy.ProcessesOutput{}, nil
}
func (stubDeployService) Plan(context.Context, testdeploy.DeploymentInput) (testdeploy.CommandOutput, error) {
	return testdeploy.CommandOutput{}, nil
}
func (stubDeployService) Deploy(context.Context, testdeploy.DeploymentInput) (testdeploy.CommandOutput, error) {
	return testdeploy.CommandOutput{}, nil
}
func (stubDeployService) Status(context.Context, testdeploy.ProcessActionInput) (testdeploy.ProcessStatusOutput, error) {
	return testdeploy.ProcessStatusOutput{}, nil
}
func (stubDeployService) Restart(context.Context, testdeploy.ProcessActionInput) (testdeploy.RestartProcessesOutput, error) {
	return testdeploy.RestartProcessesOutput{}, nil
}
func (stubDeploymentJobService) Start(context.Context, testdeploy.DeploymentInput) (testdeploy.StartDeploymentOutput, error) {
	return testdeploy.StartDeploymentOutput{}, nil
}
func (stubDeploymentJobService) Get(context.Context, testdeploy.DeploymentIDInput) (testdeploy.DeploymentStatusOutput, error) {
	return testdeploy.DeploymentStatusOutput{}, nil
}
func (stubDeploymentJobService) Logs(context.Context, testdeploy.DeploymentIDInput) (testdeploy.DeploymentLogsOutput, error) {
	return testdeploy.DeploymentLogsOutput{}, nil
}
func (stubDeploymentJobService) Recent(context.Context, testdeploy.RecentDeploymentsInput) (testdeploy.RecentDeploymentsOutput, error) {
	return testdeploy.RecentDeploymentsOutput{}, nil
}
func (stubDeploymentJobService) Cancel(context.Context, testdeploy.DeploymentIDInput) (testdeploy.CancelDeploymentOutput, error) {
	return testdeploy.CancelDeploymentOutput{}, nil
}

func (s *stubRedisService) Query(_ context.Context, _ testredis.QueryInput) (testredis.QueryOutput, error) {
	s.calls++
	return testredis.QueryOutput{ResultJSON: `"value"`}, nil
}

func (s *stubService) Query(_ context.Context, input testdb.QueryInput) (testdb.QueryOutput, error) {
	s.calls++
	return testdb.QueryOutput{Engine: input.Engine, ResultJSON: `{"columns":["id"],"rows":[[1]]}`, RowCount: 1}, nil
}

func (s *stubService) ExecDDL(_ context.Context, input testdb.ExecDDLInput) (testdb.ExecDDLOutput, error) {
	s.calls++
	return testdb.ExecDDLOutput{Engine: input.Engine, Message: "ddl executed"}, nil
}

func TestQueryTestDBToolCanBeCalledOverMCP(t *testing.T) {
	ctx := context.Background()
	service := &stubService{}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(Services{DB: service, Redis: &stubRedisService{}, TestDeploy: stubDeployService{}, TestDeployJobs: stubDeploymentJobService{}}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "query_test_db",
		Arguments: map[string]any{
			"statement": "SELECT id FROM xs_user LIMIT 1",
			"engine":    float64(testdb.EngineXianshiSQL),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || service.calls != 1 {
		t.Fatalf("result error = %v, service calls = %d", result.IsError, service.calls)
	}
	wantTools := map[string]bool{
		"query_test_db": false, "query_test_redis": false, "get_test_user_snapshot": false,
		"inspect_test_redis": false, "list_test_log_sources": false, "search_test_logs": false,
		"trace_test_logs": false, "call_test_readonly_api": false,
		"list_test_runtime_sources": false, "get_test_service_runtime": false,
		"list_testbot_endpoints": false, "call_testbot_api": false,
		"list_test_deploy_services": false, "list_test_deploy_processes": false,
		"plan_test_deployment": false, "deploy_test_service": false,
		"start_test_deployment": false, "get_test_deployment_status": false,
		"get_test_deployment_logs": false, "get_recent_deployments": false,
		"cancel_test_deployment": false, "get_test_process_status": false,
		"restart_test_process": false, "get_mcp_info": false, "tail_test_process_logs": false,
	}
	for tool, toolErr := range clientSession.Tools(ctx, nil) {
		if toolErr != nil {
			t.Fatal(toolErr)
		}
		if _, ok := wantTools[tool.Name]; ok {
			wantTools[tool.Name] = true
		}
	}
	for name, found := range wantTools {
		if !found {
			t.Errorf("tool %q was not registered", name)
		}
	}
}

func TestQueryTestRedisToolCanBeCalledOverMCP(t *testing.T) {
	ctx := context.Background()
	redisService := &stubRedisService{}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(Services{DB: &stubService{}, Redis: redisService, TestDeploy: stubDeployService{}, TestDeployJobs: stubDeploymentJobService{}}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "query_test_redis",
		Arguments: map[string]any{"command": "GET vip:user:1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || redisService.calls != 1 {
		t.Fatalf("result error = %v, service calls = %d", result.IsError, redisService.calls)
	}
}

func TestMCPInfoAndProcessLogToolsCanBeCalledOverMCP(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(Services{
		TestDeploy: stubDeployService{}, TestDeployJobs: stubDeploymentJobService{},
		MCPInfo: stubMCPInfoService{}, ProcessLogs: stubProcessLogService{},
	}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	info, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_mcp_info", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	infoJSON, marshalErr := json.Marshal(info.StructuredContent)
	if info.IsError || marshalErr != nil || !strings.Contains(string(infoJSON), `"revision":"abc1234"`) {
		t.Fatalf("get_mcp_info result = %s (%v)", infoJSON, marshalErr)
	}
	tail, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "tail_test_process_logs",
		Arguments: map[string]any{"source": "ps-devtools-mcp", "lines": float64(5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	tailJSON, marshalErr := json.Marshal(tail.StructuredContent)
	if tail.IsError || marshalErr != nil || !strings.Contains(string(tailJSON), "/home/ecs-user/log/x.stdout.log") {
		t.Fatalf("tail_test_process_logs result = %s (%v)", tailJSON, marshalErr)
	}
}

func TestToolOutputSchemaPropertiesAreObjects(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(Services{TestDeploy: stubDeployService{}, TestDeployJobs: stubDeploymentJobService{}}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	wantFlexibleProperties := map[string]string{
		"inspect_test_redis":     "value",
		"call_test_readonly_api": "body",
		"call_testbot_api":       "body",
	}
	found := make(map[string]bool, len(wantFlexibleProperties))
	for tool, toolErr := range clientSession.Tools(ctx, nil) {
		if toolErr != nil {
			t.Fatal(toolErr)
		}
		propertyName, ok := wantFlexibleProperties[tool.Name]
		if !ok {
			continue
		}
		outputSchema, ok := tool.OutputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %q output schema type = %T, want map", tool.Name, tool.OutputSchema)
		}
		properties, ok := outputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %q properties type = %T, want map", tool.Name, outputSchema["properties"])
		}
		if _, ok := properties[propertyName].(map[string]any); !ok {
			t.Errorf("tool %q property %q schema type = %T, want map", tool.Name, propertyName, properties[propertyName])
		}
		found[tool.Name] = true
	}
	for name := range wantFlexibleProperties {
		if !found[name] {
			t.Errorf("tool %q was not registered", name)
		}
	}
}

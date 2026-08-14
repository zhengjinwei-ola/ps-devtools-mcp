package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func (s *stubRedisService) Query(_ context.Context, _ testredis.QueryInput) (testredis.QueryOutput, error) {
	s.calls++
	return testredis.QueryOutput{ResultJSON: `"value"`}, nil
}

func (s *stubService) Query(_ context.Context, input testdb.QueryInput) (testdb.QueryOutput, error) {
	s.calls++
	return testdb.QueryOutput{Engine: input.Engine, ResultJSON: `{"columns":["id"],"rows":[[1]]}`, RowCount: 1}, nil
}

func TestQueryTestDBToolCanBeCalledOverMCP(t *testing.T) {
	ctx := context.Background()
	service := &stubService{}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(Services{DB: service, Redis: &stubRedisService{}, TestDeploy: stubDeployService{}}).Connect(ctx, serverTransport, nil)
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
		"list_test_deploy_services": false, "list_test_deploy_processes": false,
		"plan_test_deployment": false, "deploy_test_service": false,
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
	serverSession, err := New(Services{DB: &stubService{}, Redis: redisService, TestDeploy: stubDeployService{}}).Connect(ctx, serverTransport, nil)
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

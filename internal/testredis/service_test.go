package testredis

import (
	"context"
	"errors"
	"testing"

	"github.com/olaola-chat/ps-devtools-mcp/internal/testdb"
)

type stubQueryClient struct {
	input  testdb.QueryInput
	output testdb.QueryOutput
	err    error
	calls  int
}

func (s *stubQueryClient) Query(_ context.Context, input testdb.QueryInput) (testdb.QueryOutput, error) {
	s.calls++
	s.input = input
	return s.output, s.err
}

func TestServiceQueriesRedisEngine(t *testing.T) {
	client := &stubQueryClient{output: testdb.QueryOutput{ResultJSON: `"value"`, ElapsedMS: 3}}
	service := NewService(NewHTTPClient(client), nil)

	result, err := service.Query(context.Background(), QueryInput{Command: "GET vip:user:1"})
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || client.input.Engine != testdb.EngineRedis || result.ResultJSON != `"value"` {
		t.Fatalf("input = %+v, result = %+v, calls = %d", client.input, result, client.calls)
	}
}

func TestServiceRejectsWriteBeforeCallingEndpoint(t *testing.T) {
	client := &stubQueryClient{}
	service := NewService(NewHTTPClient(client), nil)

	_, err := service.Query(context.Background(), QueryInput{Command: "DEL vip:user:1"})
	if err == nil || client.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, client.calls)
	}
}

func TestServiceReturnsEndpointError(t *testing.T) {
	client := &stubQueryClient{err: errors.New("endpoint unavailable")}
	service := NewService(NewHTTPClient(client), nil)

	_, err := service.Query(context.Background(), QueryInput{Command: "TTL vip:user:1"})
	if err == nil || err.Error() != "endpoint unavailable" {
		t.Fatalf("error = %v", err)
	}
}

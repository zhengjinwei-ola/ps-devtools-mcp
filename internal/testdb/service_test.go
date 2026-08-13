package testdb

import (
	"context"
	"errors"
	"testing"
)

type stubQueryClient struct {
	output QueryOutput
	err    error
	calls  int
}

func (s *stubQueryClient) Query(_ context.Context, _ QueryInput) (QueryOutput, error) {
	s.calls++
	return s.output, s.err
}

func TestServiceRejectsInvalidInputBeforeCallingEndpoint(t *testing.T) {
	client := &stubQueryClient{}
	service := NewService(client, nil)

	_, err := service.Query(context.Background(), QueryInput{Statement: "DELETE FROM xs_user", Engine: EngineXianshiSQL})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if client.calls != 0 {
		t.Fatalf("client calls = %d, want 0", client.calls)
	}
}

func TestServiceReturnsEndpointError(t *testing.T) {
	client := &stubQueryClient{err: errors.New("endpoint unavailable")}
	service := NewService(client, nil)

	_, err := service.Query(context.Background(), QueryInput{Statement: "SELECT id FROM xs_user LIMIT 1", Engine: EngineXianshiSQL})
	if err == nil || err.Error() != "endpoint unavailable" {
		t.Fatalf("error = %v", err)
	}
}

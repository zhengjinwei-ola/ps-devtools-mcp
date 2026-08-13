package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/olaola-chat/ps-devtools-mcp/internal/mcpserver"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testdb"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testredis"
)

type stubService struct{}

type stubRedisService struct{}

func (stubService) Query(_ context.Context, input testdb.QueryInput) (testdb.QueryOutput, error) {
	return testdb.QueryOutput{Engine: input.Engine, ResultJSON: `{"columns":[],"rows":[]}`}, nil
}

func (stubRedisService) Query(_ context.Context, _ testredis.QueryInput) (testredis.QueryOutput, error) {
	return testredis.QueryOutput{ResultJSON: `null`}, nil
}

func newTestServer() *mcp.Server {
	return mcpserver.New(mcpserver.Services{DB: stubService{}, Redis: stubRedisService{}})
}

func TestHandlerRequiresBearerToken(t *testing.T) {
	handler := NewHandler(newTestServer(), map[string]string{"alice": "secret"}, 1, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestHandlerRejectsBearerTokenWithoutScheme(t *testing.T) {
	handler := NewHandler(newTestServer(), map[string]string{"alice": "secret"}, 1, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "secret")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestHandlerServesAuthorizedMCPRequest(t *testing.T) {
	handler := NewHandler(newTestServer(), map[string]string{"alice": "secret", "bob": "another-secret"}, 1, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
	))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"serverInfo"`) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerAcceptsEachConfiguredBearerToken(t *testing.T) {
	handler := NewHandler(newTestServer(), map[string]string{"alice": "alice-secret", "bob": "bob-secret"}, 1, nil)
	for _, token := range []string{"alice-secret", "bob-secret"} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("token %q status = %d, body = %s", token, recorder.Code, recorder.Body.String())
		}
	}
}

func TestHandlerHealthCheckDoesNotRequireToken(t *testing.T) {
	handler := NewHandler(newTestServer(), map[string]string{"alice": "secret"}, 1, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}

func TestConcurrentLimitRejectsOverflow(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	blocking := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(started) })
		<-release
		w.WriteHeader(http.StatusOK)
	})
	handler := limitConcurrent(1, blocking)

	firstDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
		close(firstDone)
	}()
	<-started

	overflow := httptest.NewRecorder()
	handler.ServeHTTP(overflow, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	close(release)
	<-firstDone

	if overflow.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", overflow.Code, http.StatusTooManyRequests)
	}
}

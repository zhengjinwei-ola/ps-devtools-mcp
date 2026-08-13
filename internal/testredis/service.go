package testredis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/olaola-chat/ps-devtools-mcp/internal/testdb"
)

type Client interface {
	Query(context.Context, QueryInput) (QueryOutput, error)
}

type queryClient interface {
	Query(context.Context, testdb.QueryInput) (testdb.QueryOutput, error)
}

type httpClient struct{ client queryClient }

func NewHTTPClient(client queryClient) Client { return &httpClient{client: client} }

func (c *httpClient) Query(ctx context.Context, input QueryInput) (QueryOutput, error) {
	result, err := c.client.Query(ctx, testdb.QueryInput{Statement: input.Command, Engine: testdb.EngineRedis})
	if err != nil {
		return QueryOutput{}, err
	}
	return QueryOutput{ResultJSON: result.ResultJSON, ElapsedMS: result.ElapsedMS}, nil
}

type Service struct {
	client Client
	logger *log.Logger
}

func NewService(client Client, logger *log.Logger) *Service {
	return &Service{client: client, logger: logger}
}

func (s *Service) Query(ctx context.Context, input QueryInput) (QueryOutput, error) {
	input.Command = strings.TrimSpace(input.Command)
	if err := ValidateReadOnlyCommand(input.Command); err != nil {
		return QueryOutput{}, fmt.Errorf("validate redis command: %w", err)
	}

	started := time.Now()
	result, err := s.client.Query(ctx, input)
	duration := time.Since(started)
	if s.logger != nil {
		sum := sha256.Sum256([]byte(input.Command))
		s.logger.Printf("tool=query_test_redis command_hash=%s duration_ms=%d error=%t",
			hex.EncodeToString(sum[:8]), duration.Milliseconds(), err != nil)
	}
	if err != nil {
		return QueryOutput{}, err
	}
	return result, nil
}

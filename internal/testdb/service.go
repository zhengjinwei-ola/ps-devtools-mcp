package testdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
)

type queryClient interface {
	Query(ctx context.Context, input QueryInput) (QueryOutput, error)
}

type Service struct {
	client queryClient
	logger *log.Logger
}

func NewService(client queryClient, logger *log.Logger) *Service {
	return &Service{client: client, logger: logger}
}

func (s *Service) Query(ctx context.Context, input QueryInput) (QueryOutput, error) {
	input.Statement = strings.TrimSpace(input.Statement)
	if input.Engine != EngineXianshiSQL && input.Engine != EngineConfigSQL {
		return QueryOutput{}, fmt.Errorf("engine must be %d (xianshi) or %d (config)", EngineXianshiSQL, EngineConfigSQL)
	}
	if err := ValidateReadOnlySQL(input.Statement); err != nil {
		return QueryOutput{}, fmt.Errorf("validate sql: %w", err)
	}

	started := time.Now()
	result, err := s.client.Query(ctx, input)
	duration := time.Since(started)
	if s.logger != nil {
		// Log a stable hash rather than the SQL itself so audit records can be
		// correlated without copying queried identifiers or literals to logs.
		sum := sha256.Sum256([]byte(input.Statement))
		s.logger.Printf("tool=query_test_db engine=%d query_hash=%s duration_ms=%d rows=%d error=%t",
			input.Engine, hex.EncodeToString(sum[:8]), duration.Milliseconds(), result.RowCount, err != nil)
	}
	if err != nil {
		return QueryOutput{}, err
	}
	return result, nil
}

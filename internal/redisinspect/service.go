package redisinspect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/olaola-chat/ps-devtools-mcp/internal/testredis"
)

type redisQuery interface {
	Query(context.Context, testredis.QueryInput) (testredis.QueryOutput, error)
}

type Service struct {
	redis  redisQuery
	logger *log.Logger
}

func NewService(redis redisQuery, logger *log.Logger) *Service {
	return &Service{redis: redis, logger: logger}
}

func (s *Service) Inspect(ctx context.Context, input InspectInput) (InspectOutput, error) {
	if err := validateKey(input.Key); err != nil {
		return InspectOutput{}, err
	}
	maxItems := input.MaxItems
	if maxItems == 0 {
		maxItems = 100
	}
	if maxItems < 1 || maxItems > 500 {
		return InspectOutput{}, fmt.Errorf("max_items must be between 1 and 500")
	}
	started := time.Now()
	output := InspectOutput{Key: input.Key}
	typeResult, err := s.redis.Query(ctx, testredis.QueryInput{Command: "TYPE " + input.Key})
	if err != nil {
		return InspectOutput{}, fmt.Errorf("query redis type: %w", err)
	}
	if err := json.Unmarshal([]byte(typeResult.ResultJSON), &output.Type); err != nil || output.Type == "" {
		return InspectOutput{}, fmt.Errorf("decode redis type")
	}
	ttlResult, err := s.redis.Query(ctx, testredis.QueryInput{Command: "TTL " + input.Key})
	if err != nil {
		return InspectOutput{}, fmt.Errorf("query redis ttl: %w", err)
	}
	if err := json.Unmarshal([]byte(ttlResult.ResultJSON), &output.TTLSeconds); err != nil {
		return InspectOutput{}, fmt.Errorf("decode redis ttl")
	}
	command := valueCommand(output.Type, input.Key, maxItems)
	if command != "" {
		valueResult, valueErr := s.redis.Query(ctx, testredis.QueryInput{Command: command})
		if valueErr != nil {
			output.ValueError = valueErr.Error()
		} else if err := decodeValue(valueResult.ResultJSON, &output); err != nil {
			output.ValueError = err.Error()
		}
	} else if output.Type != "none" {
		output.ValueError = "Redis type is not supported by the bounded inspector"
	}
	if s.logger != nil {
		hash := sha256.Sum256([]byte(input.Key))
		s.logger.Printf("tool=inspect_test_redis key_hash=%s type=%q ttl=%d max_items=%d value_error=%t duration_ms=%d",
			hex.EncodeToString(hash[:8]), output.Type, output.TTLSeconds, maxItems, output.ValueError != "", time.Since(started).Milliseconds())
	}
	return output, nil
}

func validateKey(key string) error {
	if key == "" || len(key) > 512 {
		return fmt.Errorf("key must be between 1 and 512 bytes")
	}
	for _, char := range key {
		if unicode.IsSpace(char) || unicode.IsControl(char) || char == ';' {
			return fmt.Errorf("key cannot contain whitespace, control characters, or semicolons")
		}
	}
	return nil
}

func valueCommand(redisType, key string, maxItems int) string {
	switch strings.ToLower(redisType) {
	case "string":
		return "GET " + key
	case "list":
		return "LRANGE " + key + " 0 " + strconv.Itoa(maxItems-1)
	case "hash":
		return "HSCAN " + key + " 0 COUNT " + strconv.Itoa(maxItems)
	case "set":
		return "SSCAN " + key + " 0 COUNT " + strconv.Itoa(maxItems)
	case "zset":
		return "ZSCAN " + key + " 0 COUNT " + strconv.Itoa(maxItems)
	default:
		return ""
	}
}

func decodeValue(raw string, output *InspectOutput) error {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fmt.Errorf("Redis value could not be decoded")
	}
	if output.Type == "hash" || output.Type == "set" || output.Type == "zset" {
		parts, ok := value.([]any)
		if !ok || len(parts) != 2 {
			return fmt.Errorf("Redis scan result has an unexpected shape")
		}
		output.NextCursor = fmt.Sprint(parts[0])
		output.Truncated = output.NextCursor != "0"
		output.Value = parts[1]
		return nil
	}
	output.Value = value
	return nil
}

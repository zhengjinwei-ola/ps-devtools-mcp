package redisinspect

import (
	"context"
	"testing"

	"github.com/olaola-chat/ps-devtools-mcp/internal/testredis"
)

type stubRedis struct {
	commands []string
}

func (s *stubRedis) Query(_ context.Context, input testredis.QueryInput) (testredis.QueryOutput, error) {
	s.commands = append(s.commands, input.Command)
	switch input.Command {
	case "TYPE vip:user:1":
		return testredis.QueryOutput{ResultJSON: `"hash"`}, nil
	case "TTL vip:user:1":
		return testredis.QueryOutput{ResultJSON: `300`}, nil
	default:
		return testredis.QueryOutput{ResultJSON: `["7",["field","value"]]`}, nil
	}
}

func TestInspectReturnsTypeTTLAndBoundedValue(t *testing.T) {
	redis := &stubRedis{}
	service := NewService(redis, nil)
	output, err := service.Inspect(context.Background(), InspectInput{Key: "vip:user:1", MaxItems: 10})
	if err != nil {
		t.Fatal(err)
	}
	if output.Type != "hash" || output.TTLSeconds != 300 || output.NextCursor != "7" || !output.Truncated {
		t.Fatalf("output = %+v", output)
	}
	if len(redis.commands) != 3 || redis.commands[2] != "HSCAN vip:user:1 0 COUNT 10" {
		t.Fatalf("commands = %v", redis.commands)
	}
}

func TestInspectRejectsUnsafeKey(t *testing.T) {
	service := NewService(&stubRedis{}, nil)
	if _, err := service.Inspect(context.Background(), InspectInput{Key: "a b"}); err == nil {
		t.Fatal("key with whitespace was accepted")
	}
}

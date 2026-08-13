package testredis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type DirectConfig struct {
	Address  string
	Password string
	Database int
}

type DirectClient struct{ client *redis.Client }

func OpenDirectClient(ctx context.Context, config DirectConfig) (*DirectClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr: config.Address, Password: config.Password, DB: config.Database,
		DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to test Redis: %w", err)
	}
	return &DirectClient{client: client}, nil
}

func (c *DirectClient) Close() error { return c.client.Close() }

func (c *DirectClient) Query(ctx context.Context, input QueryInput) (QueryOutput, error) {
	started := time.Now()
	parts := strings.Fields(input.Command)
	args := make([]any, len(parts))
	for index, part := range parts {
		args[index] = part
	}
	value, err := c.client.Do(ctx, args...).Result()
	if err == redis.Nil {
		value, err = nil, nil
	}
	if err != nil {
		return QueryOutput{}, fmt.Errorf("query test Redis: %w", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return QueryOutput{}, fmt.Errorf("encode Redis result: %w", err)
	}
	return QueryOutput{ResultJSON: string(encoded), ElapsedMS: time.Since(started).Milliseconds()}, nil
}

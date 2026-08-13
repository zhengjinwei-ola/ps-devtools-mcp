package testredis

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestDirectClientQueriesLocalRedis(t *testing.T) {
	server := miniredis.RunT(t)
	server.Set("profile:1", "active")

	client, err := OpenDirectClient(context.Background(), DirectConfig{Address: server.Addr()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	output, err := client.Query(context.Background(), QueryInput{Command: "GET profile:1"})
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := json.Unmarshal([]byte(output.ResultJSON), &value); err != nil {
		t.Fatal(err)
	}
	if value != "active" {
		t.Fatalf("value = %q", value)
	}
}

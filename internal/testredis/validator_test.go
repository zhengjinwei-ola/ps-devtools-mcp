package testredis

import (
	"strings"
	"testing"
)

func TestValidateReadOnlyCommand(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		wantError bool
	}{
		{name: "get", command: "GET vip:user:1"},
		{name: "bounded range", command: "ZRANGE vip:rank 0 99"},
		{name: "bounded scan", command: "SCAN 0 MATCH vip:* COUNT 100"},
		{name: "hash scan", command: "HSCAN vip:user:1 0 COUNT 50"},
		{name: "write", command: "SET vip:user:1 value", wantError: true},
		{name: "unbounded keys", command: "KEYS vip:*", wantError: true},
		{name: "unbounded collection", command: "HGETALL vip:user:1", wantError: true},
		{name: "range too large", command: "LRANGE vip:list 0 500", wantError: true},
		{name: "negative range", command: "LRANGE vip:list 0 -1", wantError: true},
		{name: "scan without count", command: "SCAN 0 MATCH vip:*", wantError: true},
		{name: "scan count too large", command: "SCAN 0 COUNT 501", wantError: true},
		{name: "multiple commands", command: "GET a; GET b", wantError: true},
		{name: "newline", command: "GET a\nGET b", wantError: true},
		{name: "too long", command: "GET " + strings.Repeat("a", maxCommandLength), wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateReadOnlyCommand(test.command)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateReadOnlyCommand() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

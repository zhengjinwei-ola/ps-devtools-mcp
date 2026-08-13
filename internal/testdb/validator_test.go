package testdb

import (
	"strings"
	"testing"
)

func TestValidateReadOnlySQL(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		wantError bool
	}{
		{name: "bounded select", statement: "SELECT id FROM xs_user WHERE uid = 1 LIMIT 1"},
		{name: "literal containing keyword", statement: "SELECT 'delete' AS value LIMIT 1"},
		{name: "show", statement: "SHOW COLUMNS FROM xs_user"},
		{name: "describe", statement: "DESCRIBE bbc_rank_award"},
		{name: "explain", statement: "EXPLAIN SELECT id FROM xs_user"},
		{name: "empty", wantError: true},
		{name: "select without limit", statement: "SELECT * FROM xs_user", wantError: true},
		{name: "write", statement: "UPDATE xs_user SET name = 'x' WHERE uid = 1", wantError: true},
		{name: "multiple", statement: "SELECT 1 LIMIT 1; SELECT 2 LIMIT 1", wantError: true},
		{name: "comment", statement: "SELECT id FROM xs_user -- bypass\n LIMIT 1", wantError: true},
		{name: "locking read", statement: "SELECT id FROM xs_user LIMIT 1 FOR UPDATE", wantError: true},
		{name: "outfile", statement: "SELECT id INTO OUTFILE '/tmp/x' FROM xs_user LIMIT 1", wantError: true},
		{name: "sleep", statement: "SELECT SLEEP(10) LIMIT 1", wantError: true},
		{name: "unclosed quote", statement: "SELECT 'x LIMIT 1", wantError: true},
		{name: "too long", statement: "SELECT " + strings.Repeat("a", maxSQLLength) + " LIMIT 1", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateReadOnlySQL(test.statement)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateReadOnlySQL() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

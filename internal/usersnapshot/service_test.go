package usersnapshot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/olaola-chat/ps-devtools-mcp/internal/testdb"
)

type stubClient struct {
	statements []string
}

func (s *stubClient) Query(_ context.Context, input testdb.QueryInput) (testdb.QueryOutput, error) {
	s.statements = append(s.statements, input.Statement)
	if strings.Contains(input.Statement, "xs_user_vip") {
		return testdb.QueryOutput{}, errors.New("vip unavailable")
	}
	if strings.Contains(input.Statement, "xs_user_commodity ") {
		return testdb.QueryOutput{ResultJSON: `{"columns":["id"],"rows":[[1],[2],[3]]}`}, nil
	}
	return testdb.QueryOutput{ResultJSON: `{"columns":["uid"],"rows":[[816220425]]}`}, nil
}

func TestGetReturnsStructuredSnapshotAndPartialErrors(t *testing.T) {
	client := &stubClient{}
	service := NewService(client, nil)
	output, err := service.Get(context.Background(), GetInput{UID: 816220425, BackpackLimit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.statements) != 5 || output.VIP.Error == "" || len(output.Backpack.Commodities.Rows) != 2 || !output.Backpack.Commodities.Truncated {
		t.Fatalf("output = %+v, statements = %v", output, client.statements)
	}
	for _, statement := range client.statements {
		if strings.Contains(statement, "name") || strings.Contains(statement, "icon") || strings.Contains(statement, "mobile") {
			t.Fatalf("sensitive profile field selected: %s", statement)
		}
	}
}

func TestGetRejectsInvalidInput(t *testing.T) {
	service := NewService(&stubClient{}, nil)
	if _, err := service.Get(context.Background(), GetInput{}); err == nil {
		t.Fatal("zero uid was accepted")
	}
	if _, err := service.Get(context.Background(), GetInput{UID: 1, BackpackLimit: 101}); err == nil {
		t.Fatal("oversized limit was accepted")
	}
}

package testbot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigValidatesPolicy(t *testing.T) {
	tests := []struct{ name, contents, want string }{
		{name: "valid", contents: `{"base_url":"https://test.invalid","login_path":"/account/passwordLogin","endpoints":[{"name":"profile","path":"/profile","method":"GET"}]}`},
		{name: "absolute endpoint URL", contents: `{"base_url":"https://test.invalid","login_path":"/login","endpoints":[{"name":"bad","path":"https://evil.invalid/path","method":"GET"}]}`, want: "absolute URL path"},
		{name: "credentials in URL", contents: `{"base_url":"https://user:pass@test.invalid","login_path":"/login","endpoints":[{"name":"profile","path":"/profile","method":"GET"}]}`, want: "base_url"},
		{name: "token header", contents: `{"base_url":"https://test.invalid","login_path":"/login","endpoints":[{"name":"bad","path":"/path","method":"GET","headers":{"User-Token":"secret"}}]}`, want: "managed or forbidden"},
		{name: "get body", contents: `{"base_url":"https://test.invalid","login_path":"/login","endpoints":[{"name":"bad","path":"/path","method":"GET","allowed_body_fields":["uid"]}]}`, want: "cannot allow a body"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(path)
			if test.want == "" && err != nil {
				t.Fatal(err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

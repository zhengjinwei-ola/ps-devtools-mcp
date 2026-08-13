package testapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigValidatesEndpointPolicy(t *testing.T) {
	tests := []struct{ name, contents, want string }{
		{name: "valid", contents: `{"endpoints":[{"name":"ping","url":"https://test.invalid/ping","method":"GET"}]}`},
		{name: "write method", contents: `{"endpoints":[{"name":"delete","url":"https://test.invalid/user","method":"DELETE"}]}`, want: "method"},
		{name: "credentials", contents: `{"endpoints":[{"name":"bad","url":"https://user:pass@test.invalid/ping","method":"GET"}]}`, want: "invalid URL"},
		{name: "get body", contents: `{"endpoints":[{"name":"bad","url":"https://test.invalid/ping","method":"GET","allowed_body_fields":["uid"]}]}`, want: "cannot allow a body"},
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

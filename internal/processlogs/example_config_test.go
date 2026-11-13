package processlogs

import "testing"

func TestExample004ConfigLoads(t *testing.T) {
	config, err := LoadConfig("../../config/process-logs.004.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Sources) == 0 || config.MaxLines <= 0 || config.MaxLineBytes <= 0 {
		t.Fatalf("config = %#v", config)
	}
}

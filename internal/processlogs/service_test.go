package processlogs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, config string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "process-logs.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigAppliesDefaultsAndValidates(t *testing.T) {
	path := writeTestConfig(t, `{"sources":[{"name":"a","patterns":["/log/a.stdout.log"]}]}`)
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.MaxLines != defaultMaxLines || config.MaxLineBytes != defaultMaxLineBytes {
		t.Fatalf("defaults not applied: %#v", config)
	}
	path = writeTestConfig(t, `{"sources":[{"name":"a","patterns":["log/relative.log"]}]}`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("relative pattern unexpectedly accepted")
	}
	path = writeTestConfig(t, `{"sources":[{"name":"a","patterns":["/log/a.log"]},{"name":"a","patterns":["/log/b.log"]}]}`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("duplicate source unexpectedly accepted")
	}
	path = writeTestConfig(t, `{"sources":[{"name":"a","patterns":["/log/a.log"]}],"unknown":1}`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("unknown field unexpectedly accepted")
	}
}

func TestTailReturnsLastLinesRedactedAndBounded(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "ps.http.stdout.log")
	if err := os.WriteFile(stdout, []byte("line-1\npassword=abc123\nline-3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr := filepath.Join(dir, "ps.http.stderr.log")
	if err := os.WriteFile(stderr, []byte("err-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(Config{
		Sources: []Source{{Name: "ps.http", Patterns: []string{stdout, stderr}}},
	}, nil)
	output, err := service.Tail(context.Background(), TailInput{Source: "ps.http", Lines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Files) != 2 {
		t.Fatalf("files = %#v", output.Files)
	}
	var stdoutTail FileTail
	for _, file := range output.Files {
		if strings.HasSuffix(file.File, "stdout.log") {
			stdoutTail = file
		}
	}
	if stdoutTail.File == "" {
		t.Fatalf("stdout tail missing: %#v", output.Files)
	}
	if len(stdoutTail.Lines) != 2 || stdoutTail.Lines[0] != "password=[REDACTED]" || stdoutTail.Lines[1] != "line-3" {
		t.Fatalf("lines = %#v", stdoutTail.Lines)
	}
	if strings.Contains(strings.Join(stdoutTail.Lines, "\n"), "abc123") {
		t.Fatalf("secret leaked: %#v", stdoutTail.Lines)
	}
}

func TestTailRejectsUnknownSourceAndBadLines(t *testing.T) {
	service := NewService(Config{Sources: []Source{{Name: "a", Patterns: []string{"/log/a.log"}}}, MaxLines: 10, MaxLineBytes: 4096}, nil)
	if _, err := service.Tail(context.Background(), TailInput{Source: "nope"}); err == nil {
		t.Fatal("unknown source unexpectedly accepted")
	}
	if _, err := service.Tail(context.Background(), TailInput{Source: "a", Lines: 0}); err != nil {
		t.Fatalf("zero lines should default: %v", err)
	}
	if _, err := service.Tail(context.Background(), TailInput{Source: "a", Lines: -1}); err == nil {
		t.Fatal("negative lines unexpectedly accepted")
	}
}

func TestTailGlobExpandsMultiInstanceFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"xs.admin.01.stdout.log", "xs.admin.02.stdout.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(Config{Sources: []Source{{Name: "xs.admin", Patterns: []string{filepath.Join(dir, "xs.admin.*.stdout.log")}}}}, nil)
	output, err := service.Tail(context.Background(), TailInput{Source: "xs.admin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Files) != 2 || output.Files[0].File > output.Files[1].File {
		t.Fatalf("files = %#v", output.Files)
	}
}

func TestTailTruncatesLongLinesWithoutSplittingRunes(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("界", 600)
	path := filepath.Join(dir, "x.log")
	if err := os.WriteFile(path, []byte(long+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(Config{Sources: []Source{{Name: "x", Patterns: []string{path}}}, MaxLineBytes: 1024}, nil)
	output, err := service.Tail(context.Background(), TailInput{Source: "x"})
	if err != nil {
		t.Fatal(err)
	}
	line := output.Files[0].Lines[0]
	if !strings.HasSuffix(line, "…") || strings.Contains(line, "�") {
		t.Fatalf("line not utf-8 safe: %q", line)
	}
}

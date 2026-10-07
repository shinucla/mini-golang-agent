package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, tool Tool, env *Env, input any) (string, error) {
	t.Helper()
	data, _ := json.Marshal(input)
	return tool.Run(context.Background(), env, data)
}

func TestEditRequiresReadAndUniqueMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	os.WriteFile(path, []byte("one\ntwo\ntwo\n"), 0o644)
	env := &Env{Cwd: dir}

	if _, err := run(t, Edit{}, env, map[string]any{"file_path": "a.txt", "old_string": "one", "new_string": "1"}); err == nil {
		t.Fatal("edit before read must fail")
	}
	if _, err := run(t, Read{}, env, map[string]any{"file_path": "a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, Edit{}, env, map[string]any{"file_path": "a.txt", "old_string": "two", "new_string": "2"}); err == nil {
		t.Fatal("ambiguous edit must fail")
	}
	if _, err := run(t, Edit{}, env, map[string]any{"file_path": "a.txt", "old_string": "two", "new_string": "2", "replace_all": true}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "one\n2\n2\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestReadFormatsLineNumbersAndOffset(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f"), []byte("a\nb\nc\n"), 0o644)
	out, err := run(t, Read{}, &Env{Cwd: dir}, map[string]any{"file_path": "f", "offset": 2, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "     2\tb\n") || !strings.Contains(out, "1 more lines") {
		t.Fatalf("out = %q", out)
	}
}

func TestWriteCreatesThenNeedsRead(t *testing.T) {
	dir := t.TempDir()
	env := &Env{Cwd: dir}
	if _, err := run(t, Write{}, env, map[string]any{"file_path": "sub/new.go", "content": "x"}); err != nil {
		t.Fatal(err)
	}
	other := &Env{Cwd: dir}
	if _, err := run(t, Write{}, other, map[string]any{"file_path": "sub/new.go", "content": "y"}); err == nil {
		t.Fatal("overwrite without read must fail")
	}
}

func TestGlobAndGrep(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\nfunc Hello() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello world\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "node_modules", "c.go"), []byte("func Hello() {}\n"), 0o644)
	env := &Env{Cwd: dir}

	out, err := run(t, Glob{}, env, map[string]any{"pattern": "**/*.go"})
	if err != nil || strings.Count(out, "\n") != 1 || !strings.Contains(out, "a.go") {
		t.Fatalf("glob = %q err=%v", out, err)
	}
	out, err = run(t, Grep{}, env, map[string]any{"pattern": "hello", "case_insensitive": true})
	if err != nil || strings.Count(out, "\n") != 1 || strings.Contains(out, "node_modules") {
		t.Fatalf("grep files = %q err=%v", out, err)
	}
	out, err = run(t, Grep{}, env, map[string]any{"pattern": "Hello", "glob": "*.go", "output_mode": "content"})
	if err != nil || !strings.HasSuffix(out, "a.go:2:func Hello() {}") {
		t.Fatalf("grep content = %q err=%v", out, err)
	}
}

func TestBashReportsExitCodeAndTimeout(t *testing.T) {
	env := &Env{Cwd: t.TempDir()}
	out, err := run(t, Bash{}, env, map[string]any{"command": "echo hi; exit 3"})
	if out != "hi" || err == nil || err.Error() != "exit code 3" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	_, err = run(t, Bash{}, env, map[string]any{"command": "sleep 5", "timeout": 200})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestHTMLToText(t *testing.T) {
	got := HTMLToText("<html><script>x()</script><h1>Title</h1><p>A &amp; B</p><ul><li>one</li></ul></html>")
	if got != "Title\nA & B\n\n- one" {
		t.Fatalf("got %q", got)
	}
}

func TestOneLineKeepsWholeCharacters(t *testing.T) {
	if got := OneLine("修复  构建\n错误", 5); got != "修…" {
		t.Fatalf("cut inside a character: got %q", got)
	}
	if got := OneLine("修复  构建\n错误", 7); got != "修复 …" {
		t.Fatalf("cut on a boundary: got %q", got)
	}
	if OneLine("a  b", 10) != "a b" {
		t.Fatal("short text must only collapse spaces")
	}
}

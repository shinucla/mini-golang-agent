package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultReadLimit = 2000
	maxLineLength    = 2000
)

type Read struct{}

func (Read) Name() string   { return "Read" }
func (Read) ReadOnly() bool { return true }

func (Read) Description() string {
	return "Read a text file. Output uses cat -n format with line numbers that start at 1. " +
		"Reads up to 2000 lines by default; use offset and limit for large files. Use an absolute path when you can."
}

func (Read) Schema() map[string]any {
	return schema(map[string]any{
		"file_path": str("Path of the file to read"),
		"offset":    integer("Line number to start from (1-based)"),
		"limit":     integer("Number of lines to read"),
	}, "file_path")
}

func (Read) Summary(input json.RawMessage) string { return field(input, "file_path") }

func (Read) Run(_ context.Context, env *Env, input json.RawMessage) (string, error) {
	in, err := decode[struct {
		FilePath string `json:"file_path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}](input)
	if err != nil {
		return "", err
	}
	path := env.Resolve(in.FilePath)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory; use Glob or Bash ls", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	env.markRead(path)
	if len(data) == 0 {
		return "<file is empty>", nil
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) != -1 {
		return fmt.Sprintf("<binary file, %d bytes>", len(data)), nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	start := max(in.Offset, 1)
	limit := in.Limit
	if limit <= 0 {
		limit = defaultReadLimit
	}
	if len(lines) < start {
		return fmt.Sprintf("<file has %d lines; offset %d is past the end>", len(lines), start), nil
	}
	end := min(start-1+limit, len(lines))
	var b strings.Builder
	for i := start - 1; i < end; i++ {
		line := lines[i]
		if maxLineLength < len(line) {
			line = line[:maxLineLength] + "…"
		}
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, line)
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "... [%d more lines; use offset %d to continue]\n", len(lines)-end, end+1)
	}
	return b.String(), nil
}

type Write struct{}

func (Write) Name() string   { return "Write" }
func (Write) ReadOnly() bool { return false }

func (Write) Description() string {
	return "Write a file and replace any content it has. Read an existing file before you overwrite it. " +
		"Prefer Edit for changes to existing files."
}

func (Write) Schema() map[string]any {
	return schema(map[string]any{
		"file_path": str("Path of the file to write"),
		"content":   str("The full content of the file"),
	}, "file_path", "content")
}

func (Write) Summary(input json.RawMessage) string { return field(input, "file_path") }

func (Write) Run(_ context.Context, env *Env, input json.RawMessage) (string, error) {
	in, err := decode[struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}](input)
	if err != nil {
		return "", err
	}
	path := env.Resolve(in.FilePath)
	if err := env.checkFresh(path); err != nil {
		return "", err
	}
	mode := fs.FileMode(0o644)
	verb := "Created"
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		verb = "Updated"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(in.Content), mode); err != nil {
		return "", err
	}
	env.markRead(path)
	return fmt.Sprintf("%s %s (%d lines)", verb, path, strings.Count(in.Content, "\n")+1), nil
}

type Edit struct{}

func (Edit) Name() string   { return "Edit" }
func (Edit) ReadOnly() bool { return false }

func (Edit) Description() string {
	return "Replace an exact string in a file. Read the file first. old_string must match the file exactly, " +
		"including indentation, and must be unique unless replace_all is true. An empty old_string on a missing file creates it."
}

func (Edit) Schema() map[string]any {
	return schema(map[string]any{
		"file_path":   str("Path of the file to edit"),
		"old_string":  str("The exact text to replace"),
		"new_string":  str("The replacement text"),
		"replace_all": boolean("Replace every match (default false)"),
	}, "file_path", "old_string", "new_string")
}

func (Edit) Summary(input json.RawMessage) string { return field(input, "file_path") }

type EditInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func (Edit) Run(_ context.Context, env *Env, input json.RawMessage) (string, error) {
	in, err := decode[EditInput](input)
	if err != nil {
		return "", err
	}
	path := env.Resolve(in.FilePath)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && in.OldString == "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(in.NewString), 0o644); err != nil {
			return "", err
		}
		env.markRead(path)
		return "Created " + path, nil
	}
	if err != nil {
		return "", err
	}
	if err := env.checkFresh(path); err != nil {
		return "", err
	}
	if in.OldString == in.NewString {
		return "", errors.New("old_string and new_string are the same")
	}
	if in.OldString == "" {
		return "", errors.New("old_string is empty; use Write to replace a whole file")
	}
	content := string(data)
	count := strings.Count(content, in.OldString)
	if count == 0 {
		return "", errors.New("old_string not found in the file")
	}
	if 1 < count && !in.ReplaceAll {
		return "", fmt.Errorf("old_string matches %d places; add more context or set replace_all", count)
	}
	if in.ReplaceAll {
		content = strings.ReplaceAll(content, in.OldString, in.NewString)
	} else {
		content = strings.Replace(content, in.OldString, in.NewString, 1)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), info.Mode().Perm()); err != nil {
		return "", err
	}
	env.markRead(path)
	if in.ReplaceAll {
		return fmt.Sprintf("Edited %s (%d replacements)", path, count), nil
	}
	return "Edited " + path, nil
}

package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	maxGlobResults  = 200
	maxGrepLines    = 500
	maxGrepFileSize = 5 << 20
)

var skippedDirs = []string{".git", "node_modules", ".hg", ".svn", ".idea", ".venv", "__pycache__"}

type Glob struct{}

func (Glob) Name() string   { return "Glob" }
func (Glob) ReadOnly() bool { return true }

func (Glob) Description() string {
	return "Find files by glob pattern, such as \"**/*.go\" or \"src/**/*.{ts,tsx}\". " +
		"Returns matching paths, the most recently modified first."
}

func (Glob) Schema() map[string]any {
	return schema(map[string]any{
		"pattern": str("The glob pattern"),
		"path":    str("Directory to search in (default: working directory)"),
	}, "pattern")
}

func (Glob) Summary(input json.RawMessage) string {
	if p := field(input, "path"); p != "" {
		return field(input, "pattern") + " in " + p
	}
	return field(input, "pattern")
}

func (Glob) Run(ctx context.Context, env *Env, input json.RawMessage) (string, error) {
	in, err := decode[struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}](input)
	if err != nil {
		return "", err
	}
	base := env.Resolve(in.Path)
	pattern := in.Pattern
	if filepath.IsAbs(pattern) {
		var rel string
		base, rel = doublestar.SplitPattern(filepath.ToSlash(pattern))
		pattern = rel
	}
	type hit struct {
		path string
		mod  int64
	}
	var hits []hit
	err = doublestar.GlobWalk(os.DirFS(base), pattern, func(p string, d fs.DirEntry) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || hasSkippedDir(p) {
			return nil
		}
		var mod int64
		if info, err := d.Info(); err == nil {
			mod = info.ModTime().UnixNano()
		}
		hits = append(hits, hit{filepath.Join(base, p), mod})
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "No files found", nil
	}
	slices.SortFunc(hits, func(a, b hit) int {
		switch {
		case a.mod < b.mod:
			return 1
		case b.mod < a.mod:
			return -1
		}
		return strings.Compare(a.path, b.path)
	})
	var b strings.Builder
	for i, h := range hits {
		if i == maxGlobResults {
			fmt.Fprintf(&b, "... [%d more files]\n", len(hits)-maxGlobResults)
			break
		}
		b.WriteString(h.path)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func hasSkippedDir(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if slices.Contains(skippedDirs, part) {
			return true
		}
	}
	return false
}

type Grep struct{}

func (Grep) Name() string   { return "Grep" }
func (Grep) ReadOnly() bool { return true }

func (Grep) Description() string {
	return "Search file contents with a Go regular expression. Skips .git, node_modules, and binary files. " +
		"output_mode is \"files_with_matches\" (default), \"content\" (matching lines with line numbers), or \"count\"."
}

func (Grep) Schema() map[string]any {
	return schema(map[string]any{
		"pattern":          str("Regular expression (Go RE2 syntax)"),
		"path":             str("File or directory to search (default: working directory)"),
		"glob":             str("Only search files that match this glob, such as \"*.go\" or \"**/*.{ts,tsx}\""),
		"output_mode":      map[string]any{"type": "string", "enum": []string{"files_with_matches", "content", "count"}},
		"case_insensitive": boolean("Ignore case"),
		"context":          integer("Lines of context around each match (content mode only)"),
		"head_limit":       integer("Maximum number of output lines (default 500)"),
	}, "pattern")
}

func (Grep) Summary(input json.RawMessage) string {
	s := fmt.Sprintf("%q", field(input, "pattern"))
	if p := field(input, "path"); p != "" {
		s += " in " + p
	}
	if g := field(input, "glob"); g != "" {
		s += " (" + g + ")"
	}
	return s
}

type grepInput struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path"`
	Glob            string `json:"glob"`
	OutputMode      string `json:"output_mode"`
	CaseInsensitive bool   `json:"case_insensitive"`
	Context         int    `json:"context"`
	HeadLimit       int    `json:"head_limit"`
}

func (Grep) Run(ctx context.Context, env *Env, input json.RawMessage) (string, error) {
	in, err := decode[grepInput](input)
	if err != nil {
		return "", err
	}
	expr := in.Pattern
	if in.CaseInsensitive {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return "", fmt.Errorf("bad pattern: %w", err)
	}
	limit := in.HeadLimit
	if limit <= 0 {
		limit = maxGrepLines
	}
	root := env.Resolve(in.Path)
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}

	var out []string
	full := false
	emit := func(line string) {
		if len(out) < limit {
			out = append(out, line)
			return
		}
		full = true
	}
	search := func(path string) {
		lines, ok := readTextLines(path)
		if !ok {
			return
		}
		switch in.OutputMode {
		case "content":
			grepContent(path, lines, re, in.Context, emit)
		case "count":
			n := 0
			for _, l := range lines {
				if re.MatchString(l) {
					n++
				}
			}
			if 0 < n {
				emit(fmt.Sprintf("%s:%d", path, n))
			}
		default:
			if slices.ContainsFunc(lines, re.MatchString) {
				emit(path)
			}
		}
	}

	if !info.IsDir() {
		search(root)
	} else {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if full {
				return filepath.SkipAll
			}
			if d.IsDir() {
				if path != root && (slices.Contains(skippedDirs, d.Name()) || strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if in.Glob != "" && !globMatches(in.Glob, root, path) {
				return nil
			}
			search(path)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	if len(out) == 0 {
		return "No matches found", nil
	}
	result := strings.Join(out, "\n")
	if full {
		result += fmt.Sprintf("\n... [stopped at %d lines; narrow the search or raise head_limit]", limit)
	}
	return result, nil
}

func globMatches(pattern, root, path string) bool {
	if !strings.Contains(pattern, "/") {
		ok, _ := doublestar.Match(pattern, filepath.Base(path))
		return ok
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	ok, _ := doublestar.Match(pattern, filepath.ToSlash(rel))
	return ok
}

func readTextLines(path string) ([]string, bool) {
	info, err := os.Stat(path)
	if err != nil || maxGrepFileSize < info.Size() {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) != -1 {
		return nil, false
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxGrepFileSize)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, true
}

func grepContent(path string, lines []string, re *regexp.Regexp, around int, emit func(string)) {
	last := -1
	for i, l := range lines {
		if !re.MatchString(l) {
			continue
		}
		from := max(i-around, last+1)
		to := min(i+around, len(lines)-1)
		if 0 <= last && last+1 < from && 0 < around {
			emit("--")
		}
		for j := from; j <= to; j++ {
			sep := "-"
			if re.MatchString(lines[j]) {
				sep = ":"
			}
			emit(fmt.Sprintf("%s:%d%s%s", path, j+1, sep, OneLine(lines[j], 500)))
		}
		last = to
	}
}

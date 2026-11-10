package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shinucla/mini-golang-agent/internal/config"
)

const maxImportDepth = 5

var projectInstructionFiles = []string{"MGA.md", "AGENTS.md", "CLAUDE.md", "CLAUDE.local.md", ".mga/MGA.md", ".claude/CLAUDE.md"}

var importRef = regexp.MustCompile(`(?:^|\s)@((?:~/|\.{1,2}/|/)?[A-Za-z0-9_.\-][^\s` + "`" + `]*)`)

type InstructionFile struct {
	Path  string
	Text  string
	Paths []string
}

type instructionLoader struct {
	seenPath map[string]bool
	seenText map[string]bool
	files    []InstructionFile
}

func InstructionFiles(cwd string) []InstructionFile {
	l := &instructionLoader{seenPath: map[string]bool{}, seenText: map[string]bool{}}
	for _, home := range []string{config.Home(), config.ClaudeHome()} {
		l.add(filepath.Join(home, "MGA.md"), 0)
		l.add(filepath.Join(home, "CLAUDE.md"), 0)
		l.addRules(filepath.Join(home, "rules"))
	}
	chain := config.FolderChain(cwd)
	slices.Reverse(chain)
	for _, dir := range chain {
		for _, name := range projectInstructionFiles {
			l.add(filepath.Join(dir, name), 0)
		}
		l.addRules(filepath.Join(dir, ".mga", "rules"))
		l.addRules(filepath.Join(dir, ".claude", "rules"))
	}
	return l.files
}

func (l *instructionLoader) addRules(dir string) {
	var files []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	slices.Sort(files)
	for _, f := range files {
		l.add(f, 0)
	}
}

func (l *instructionLoader) add(path string, depth int) {
	path = filepath.Clean(path)
	if l.seenPath[path] {
		return
	}
	l.seenPath[path] = true
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	paths, text := ruleFrontmatter(string(data))
	text = strings.TrimSpace(text)
	if text == "" || l.seenText[text] {
		return
	}
	l.seenText[text] = true
	if maxMemoryFile < len(text) {
		text = text[:maxMemoryFile]
	}
	l.files = append(l.files, InstructionFile{Path: path, Text: text, Paths: paths})
	if depth < maxImportDepth {
		for _, ref := range imports(text) {
			l.add(resolveImport(ref, filepath.Dir(path)), depth+1)
		}
	}
}

func imports(text string) []string {
	var out []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		for i, part := range strings.Split(line, "`") {
			if i%2 == 1 {
				continue
			}
			for _, m := range importRef.FindAllStringSubmatch(part, -1) {
				out = append(out, strings.TrimRight(m[1], ".,;:)"))
			}
		}
	}
	return out
}

func resolveImport(ref, dir string) string {
	if rest, ok := strings.CutPrefix(ref, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(dir, ref)
}

func ruleFrontmatter(text string) ([]string, string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, text
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return nil, text
	}
	body = strings.TrimPrefix(body, "\n")
	var fm struct {
		Paths any `yaml:"paths"`
	}
	if yaml.Unmarshal([]byte(head), &fm) != nil {
		return nil, body
	}
	switch v := fm.Paths.(type) {
	case string:
		return splitPatterns(v), body
	case []any:
		var out []string
		for _, p := range v {
			if s, ok := p.(string); ok {
				out = append(out, splitPatterns(s)...)
			}
		}
		return out, body
	}
	return nil, body
}

func splitPatterns(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

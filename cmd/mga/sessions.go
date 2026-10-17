package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/config"
	"github.com/shinucla/mini-golang-agent/internal/llm"
	"github.com/shinucla/mini-golang-agent/internal/tools"
)

const (
	formatText   = "text"
	formatJSON   = "json"
	formatStream = "stream-json"
)

type eventWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *eventWriter) emit(event map[string]any) {
	if w == nil {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprintf(w.out, "%s\n", data)
}

type sessionSummary struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Cwd      string    `json:"cwd"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Messages int       `json:"messages"`
	Pinned   bool      `json:"pinned"`
	BusyPID  int       `json:"busy_pid,omitempty"`
}

func summarize(dir string, s *agent.Session) sessionSummary {
	return sessionSummary{
		ID: s.ID, Title: s.Title, Cwd: s.Cwd, Created: s.Created, Updated: s.Updated,
		Provider: s.Provider, Model: s.Model, Messages: len(s.Messages), Pinned: s.Pinned,
		BusyPID: agent.LockedBy(dir, s.ID),
	}
}

func sessionsCommand(cwd string, args []string) error {
	return sessionsTo(os.Stdout, cwd, args)
}

func sessionsTo(out io.Writer, cwd string, args []string) error {
	sub := "list"
	if 0 < len(args) && (args[0] == "list" || args[0] == "show") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("mga sessions", flag.ContinueOnError)
	folder := fs.String("dir", cwd, "list the sessions of this folder")
	all := fs.Bool("all", false, "list the sessions of every folder")
	asJSON := fs.Bool("json", false, "print JSON")
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	dir := config.SessionsDir()
	if sub == "show" {
		if len(positional) != 1 {
			return errors.New("usage: mga sessions show <id> [--json]")
		}
		return showSession(out, dir, positional[0], *asJSON)
	}
	where := ""
	if !*all {
		if where, err = filepath.Abs(*folder); err != nil {
			return err
		}
	}
	sessions, err := agent.ListSessions(dir, where)
	if err != nil {
		return err
	}
	summaries := make([]sessionSummary, 0, len(sessions))
	for _, s := range sessions {
		summaries = append(summaries, summarize(dir, s))
	}
	if *asJSON {
		return writeJSON(out, summaries)
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUPDATED\tMSGS\tMODEL\tSTATE\tTITLE")
	for _, s := range summaries {
		state := "idle"
		if s.BusyPID != 0 {
			state = fmt.Sprintf("working (pid %d)", s.BusyPID)
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s:%s\t%s\t%s\n", s.ID, s.Updated.Format("2006-01-02 15:04"), s.Messages, s.Provider, s.Model, state, tools.OneLine(s.Title, 60))
	}
	return w.Flush()
}

func showSession(out io.Writer, dir, id string, asJSON bool) error {
	s, err := agent.LoadSession(dir, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("session %q does not exist", id)
		}
		return err
	}
	if asJSON {
		return writeJSON(out, struct {
			*agent.Session
			BusyPID int `json:"busy_pid,omitempty"`
		}{s, agent.LockedBy(dir, id)})
	}
	fmt.Fprintf(out, "%s · %s · %s:%s · %s\n", s.ID, s.Title, s.Provider, s.Model, s.Cwd)
	for _, m := range s.Messages {
		switch m.Role {
		case llm.RoleUser:
			fmt.Fprintf(out, "\n> %s\n", m.Content)
		case llm.RoleAssistant:
			if m.Content != "" {
				fmt.Fprintf(out, "\n⏺ %s\n", m.Content)
			}
			for _, c := range m.ToolCalls {
				fmt.Fprintf(out, "  → %s(%s)\n", c.Name, tools.OneLine(c.Arguments, 160))
			}
		case llm.RoleTool:
			fmt.Fprintf(out, "  ⎿ %s\n", tools.OneLine(m.Content, 160))
		}
	}
	return nil
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

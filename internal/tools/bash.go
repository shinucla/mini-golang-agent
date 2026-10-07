package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	defaultBashTimeout = 2 * time.Minute
	maxBashTimeout     = 10 * time.Minute
	maxBashOutput      = 30000
)

type Bash struct{}

func (Bash) Name() string   { return "Bash" }
func (Bash) ReadOnly() bool { return false }

func (Bash) Description() string {
	return "Run a shell command in the working directory with bash. Output is stdout and stderr combined. " +
		"Use it for builds, tests, git, and other terminal operations. Prefer Read, Glob, Grep, Edit, and Write for file work. " +
		"Commands do not get stdin, so do not run interactive programs."
}

func (Bash) Schema() map[string]any {
	return schema(map[string]any{
		"command":     str("The command to run"),
		"timeout":     integer("Optional timeout in milliseconds (max 600000, default 120000)"),
		"description": str("Five to ten words that say what the command does"),
	}, "command")
}

func (Bash) Summary(input json.RawMessage) string { return OneLine(field(input, "command"), 120) }

type capWriter struct {
	buf     []byte
	limit   int
	dropped int
}

func (w *capWriter) Write(p []byte) (int, error) {
	room := w.limit - len(w.buf)
	if room <= 0 {
		w.dropped += len(p)
		return len(p), nil
	}
	if len(p) <= room {
		w.buf = append(w.buf, p...)
		return len(p), nil
	}
	w.buf = append(w.buf, p[:room]...)
	w.dropped += len(p) - room
	return len(p), nil
}

func (Bash) Run(ctx context.Context, env *Env, input json.RawMessage) (string, error) {
	in, err := decode[struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}](input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", errors.New("command is empty")
	}
	timeout := defaultBashTimeout
	if 0 < in.Timeout {
		timeout = min(time.Duration(in.Timeout)*time.Millisecond, maxBashTimeout)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell := "bash"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "sh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", in.Command)
	cmd.Dir = env.Cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	out := &capWriter{limit: maxBashOutput}
	cmd.Stdout = out
	cmd.Stderr = out

	runErr := cmd.Run()
	result := strings.TrimRight(string(out.buf), "\n")
	if 0 < out.dropped {
		result += fmt.Sprintf("\n... [output truncated, %d more bytes]", out.dropped)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("command timed out after %s", timeout)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return result, fmt.Errorf("exit code %d", exitErr.ExitCode())
	}
	if runErr != nil {
		return result, runErr
	}
	if result == "" {
		result = "(no output)"
	}
	return result, nil
}

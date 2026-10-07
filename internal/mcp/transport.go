package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxMessageSize = 32 << 20
	stderrKeep     = 4096
	killDelay      = 2 * time.Second
)

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if stderrKeep < len(t.buf) {
		t.buf = t.buf[len(t.buf)-stderrKeep:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	if 8 < len(lines) {
		lines = lines[len(lines)-8:]
	}
	return strings.Join(lines, "\n")
}

type stdioTransport struct {
	cfg    ServerConfig
	dir    string
	stderr *tailBuffer

	writeMu sync.Mutex
	stdin   io.WriteCloser
	cmd     *exec.Cmd
	done    chan struct{}
}

func (t *stdioTransport) start(deliver func([]byte), closed func(error)) error {
	if t.cfg.Command == "" {
		return errors.New("no command configured")
	}
	cmd := exec.Command(t.cfg.Command, t.cfg.Args...)
	cmd.Dir = t.dir
	cmd.Env = os.Environ()
	for k, v := range t.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = t.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", t.cfg.Command, err)
	}
	t.cmd, t.stdin, t.done = cmd, stdin, make(chan struct{})
	go func() {
		reader := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, err := reader.ReadBytes('\n')
			if 0 < len(line) {
				deliver(line)
			}
			if err != nil {
				break
			}
		}
		waitErr := cmd.Wait()
		close(t.done)
		msg := "the server process exited"
		if waitErr != nil {
			msg += " (" + waitErr.Error() + ")"
		}
		if tail := t.stderr.String(); tail != "" {
			msg += ": " + tail
		}
		closed(errors.New(msg))
	}()
	return nil
}

func (t *stdioTransport) send(_ context.Context, msg []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.stdin == nil {
		return errors.New("the server is not running")
	}
	_, err := t.stdin.Write(append(bytes.TrimSpace(msg), '\n'))
	return err
}

func (t *stdioTransport) setProtocolVersion(string) {}

func (t *stdioTransport) close() error {
	if t.cmd == nil {
		return nil
	}
	t.writeMu.Lock()
	err := t.stdin.Close()
	t.writeMu.Unlock()
	select {
	case <-t.done:
	case <-time.After(killDelay):
		_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
	}
	return err
}

type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
	deliver func([]byte)

	mu        sync.Mutex
	sessionID string
	protocol  string
}

func newHTTPTransport(cfg ServerConfig) *httpTransport {
	return &httpTransport{url: cfg.URL, headers: cfg.Headers, client: &http.Client{}}
}

func (t *httpTransport) start(deliver func([]byte), _ func(error)) error {
	if t.url == "" {
		return errors.New("no url configured")
	}
	t.deliver = deliver
	return nil
}

func (t *httpTransport) setProtocolVersion(version string) {
	t.mu.Lock()
	t.protocol = version
	t.mu.Unlock()
}

func (t *httpTransport) request(ctx context.Context, method string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	if t.protocol != "" {
		req.Header.Set("MCP-Protocol-Version", t.protocol)
	}
	t.mu.Unlock()
	return req, nil
}

func (t *httpTransport) send(ctx context.Context, msg []byte) error {
	req, err := t.request(ctx, http.MethodPost, msg)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil
	}
	if 300 <= resp.StatusCode {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readEvents(resp.Body, t.deliver)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageSize))
	if err != nil {
		return err
	}
	if 0 < len(bytes.TrimSpace(body)) {
		t.deliver(body)
	}
	return nil
}

func (t *httpTransport) close() error {
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()
	if sid == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := t.request(ctx, http.MethodDelete, nil)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil
	}
	resp.Body.Close()
	return nil
}

func readEvents(r io.Reader, deliver func([]byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxMessageSize)
	var data bytes.Buffer
	flush := func() {
		if data.Len() != 0 {
			deliver(bytes.Clone(data.Bytes()))
			data.Reset()
		}
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() != 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(value, " "))
		}
	}
	flush()
	return sc.Err()
}

package mcp

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const connectTimeout = 30 * time.Second

type Status string

const (
	StatusNeedsApproval Status = "needs approval"
	StatusConnecting    Status = "connecting"
	StatusConnected     Status = "connected"
	StatusFailed        Status = "failed"
)

type ToolSummary struct {
	Name        string
	Description string
	ReadOnly    bool
}

type ServerInfo struct {
	Name          string
	Scope         string
	Transport     string
	Target        string
	Status        Status
	Err           string
	ServerName    string
	ServerVersion string
	Instructions  string
	Stderr        string
	Tools         []ToolSummary
}

type server struct {
	name   string
	scope  string
	cfg    ServerConfig
	status Status
	err    string
	client *Client
	tools  []*Tool
	stderr *tailBuffer
}

type Manager struct {
	cwd string

	mu      sync.Mutex
	servers []*server

	OnChange        func()
	PersistApproval func(name string) error
	LoadErrors      []error
}

func Load(cwd string, approved []string) *Manager {
	m := &Manager{cwd: cwd}
	user, err := ReadServers(UserConfigPath())
	if err != nil {
		m.LoadErrors = append(m.LoadErrors, err)
	}
	project, err := ReadServers(ProjectConfigPath(cwd))
	if err != nil {
		m.LoadErrors = append(m.LoadErrors, err)
	}
	byName := map[string]*server{}
	for name, cfg := range user {
		byName[name] = &server{name: name, scope: ScopeUser, cfg: cfg, status: StatusConnecting}
	}
	for name, cfg := range project {
		status := StatusConnecting
		if !slices.Contains(approved, name) {
			status = StatusNeedsApproval
		}
		byName[name] = &server{name: name, scope: ScopeProject, cfg: cfg, status: status}
	}
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		s := byName[name]
		s.stderr = &tailBuffer{}
		m.servers = append(m.servers, s)
	}
	return m
}

func (m *Manager) notify() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

func (m *Manager) find(name string) *server {
	for _, s := range m.servers {
		if s.name == name {
			return s
		}
	}
	return nil
}

func (m *Manager) ConnectAll(ctx context.Context) {
	m.mu.Lock()
	var targets []*server
	for _, s := range m.servers {
		if s.status != StatusNeedsApproval {
			targets = append(targets, s)
		}
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.connect(ctx, s)
		}()
	}
	wg.Wait()
}

func (m *Manager) Reconnect(ctx context.Context, name string) error {
	m.mu.Lock()
	s := m.find(name)
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no MCP server %q", name)
	}
	if s.status == StatusNeedsApproval {
		return fmt.Errorf("MCP server %q needs approval first", name)
	}
	go m.connect(ctx, s)
	return nil
}

func (m *Manager) Approve(ctx context.Context, name string) error {
	m.mu.Lock()
	s := m.find(name)
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no MCP server %q", name)
	}
	if m.PersistApproval != nil {
		if err := m.PersistApproval(name); err != nil {
			return err
		}
	}
	m.mu.Lock()
	s.status = StatusConnecting
	m.mu.Unlock()
	go m.connect(ctx, s)
	return nil
}

func (m *Manager) connect(ctx context.Context, s *server) {
	m.mu.Lock()
	old := s.client
	s.client, s.tools, s.status, s.err = nil, nil, StatusConnecting, ""
	cfg := s.cfg.expanded()
	m.mu.Unlock()
	m.notify()
	if old != nil {
		_ = old.close()
	}

	var t transport
	switch cfg.Transport() {
	case TransportStdio:
		t = &stdioTransport{cfg: cfg, dir: m.cwd, stderr: s.stderr}
	case TransportHTTP:
		t = newHTTPTransport(cfg)
	default:
		m.fail(s, nil, fmt.Errorf("transport %q is not supported; use stdio or http", cfg.Transport()))
		return
	}
	c := newClient(t)
	c.onToolsChanged = func() { m.refreshTools(ctx, s, c) }
	c.onClosed = func(err error) { m.fail(s, c, err) }

	initCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	err := c.start()
	var infos []ToolInfo
	if err == nil {
		err = c.initialize(initCtx)
	}
	if err == nil {
		infos, err = c.listTools(initCtx)
	}
	if err != nil {
		_ = c.close()
		if tail := s.stderr.String(); tail != "" && !strings.Contains(err.Error(), tail) {
			err = fmt.Errorf("%w: %s", err, tail)
		}
		m.fail(s, nil, err)
		return
	}
	m.mu.Lock()
	s.client, s.tools, s.status, s.err = c, m.wrap(s, c, infos), StatusConnected, ""
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) wrap(s *server, c *Client, infos []ToolInfo) []*Tool {
	current := func() *Client {
		m.mu.Lock()
		defer m.mu.Unlock()
		if s.client == c {
			return c
		}
		return nil
	}
	out := make([]*Tool, 0, len(infos))
	for _, info := range infos {
		out = append(out, &Tool{server: s.name, name: ExposedName(s.name, info.Name), info: info, client: current})
	}
	return out
}

func (m *Manager) refreshTools(ctx context.Context, s *server, c *Client) {
	listCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	infos, err := c.listTools(listCtx)
	if err != nil {
		return
	}
	m.mu.Lock()
	if s.client == c {
		s.tools = m.wrap(s, c, infos)
	}
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) fail(s *server, c *Client, err error) {
	m.mu.Lock()
	if c != nil && s.client != c {
		m.mu.Unlock()
		return
	}
	s.client, s.tools, s.status = nil, nil, StatusFailed
	s.err = "unknown error"
	if err != nil {
		s.err = err.Error()
	}
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) Tools() []tools.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []tools.Tool
	for _, s := range m.servers {
		for _, t := range s.tools {
			out = append(out, t)
		}
	}
	return out
}

func (m *Manager) Instructions() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var parts []string
	for _, s := range m.servers {
		if s.client != nil && s.client.Instructions != "" {
			parts = append(parts, "## "+s.name+"\n"+s.client.Instructions)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (m *Manager) Snapshot() []ServerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerInfo, 0, len(m.servers))
	for _, s := range m.servers {
		info := ServerInfo{
			Name: s.name, Scope: s.scope, Transport: s.cfg.Transport(), Target: s.cfg.Target(),
			Status: s.status, Err: s.err, Stderr: s.stderr.String(),
		}
		if s.client != nil {
			info.ServerName, info.ServerVersion, info.Instructions = s.client.ServerName, s.client.ServerVersion, s.client.Instructions
		}
		for _, t := range s.tools {
			info.Tools = append(info.Tools, ToolSummary{Name: t.info.Name, Description: t.info.Description, ReadOnly: t.ReadOnly()})
		}
		out = append(out, info)
	}
	return out
}

func (m *Manager) Close() {
	m.mu.Lock()
	var clients []*Client
	for _, s := range m.servers {
		if s.client != nil {
			clients = append(clients, s.client)
			s.client, s.tools = nil, nil
		}
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.close()
		}()
	}
	wg.Wait()
}

func (m *Manager) Count() (connected, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.status == StatusConnected {
			connected++
		}
	}
	return connected, len(m.servers)
}

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ProtocolVersion = "2025-06-18"
	clientVersion   = "0.1.0"
	maxToolPages    = 100
	replyTimeout    = 10 * time.Second
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message) }

type transport interface {
	start(deliver func([]byte), closed func(error)) error
	send(ctx context.Context, msg []byte) error
	setProtocolVersion(version string)
	close() error
}

type ToolInfo struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Annotations struct {
		Title        string `json:"title,omitempty"`
		ReadOnlyHint *bool  `json:"readOnlyHint,omitempty"`
	} `json:"annotations"`
}

type ContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
	Resource *struct {
		URI      string `json:"uri"`
		MimeType string `json:"mimeType,omitempty"`
		Text     string `json:"text,omitempty"`
		Blob     string `json:"blob,omitempty"`
	} `json:"resource,omitempty"`
}

type CallResult struct {
	Content           []ContentItem   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError"`
}

type Client struct {
	t      transport
	nextID atomic.Int64

	mu        sync.Mutex
	pending   map[int64]chan rpcMessage
	closedErr error

	onToolsChanged func()
	onClosed       func(error)

	ServerName    string
	ServerVersion string
	Instructions  string
}

func newClient(t transport) *Client {
	return &Client{t: t, pending: map[int64]chan rpcMessage{}}
}

func (c *Client) start() error {
	return c.t.start(c.deliver, c.closed)
}

func (c *Client) close() error {
	err := c.t.close()
	c.closed(errors.New("connection closed"))
	return err
}

func (c *Client) closed(err error) {
	if err == nil {
		err = errors.New("connection closed")
	}
	c.mu.Lock()
	if c.closedErr != nil {
		c.mu.Unlock()
		return
	}
	c.closedErr = err
	pending := c.pending
	c.pending = map[int64]chan rpcMessage{}
	onClosed := c.onClosed
	c.mu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- rpcMessage{Error: &RPCError{Code: -32000, Message: err.Error()}}:
		default:
		}
	}
	if onClosed != nil {
		go onClosed(err)
	}
}

func rawJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan rpcMessage, 1)
	c.mu.Lock()
	if c.closedErr != nil {
		err := c.closedErr
		c.mu.Unlock()
		return err
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	msg, err := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: rawJSON(id), Method: method, Params: rawJSON(params)})
	if err != nil {
		return err
	}
	if err := c.t.send(ctx, msg); err != nil {
		return err
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil && len(resp.Result) != 0 {
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	case <-ctx.Done():
		cancelCtx, cancel := context.WithTimeout(context.Background(), replyTimeout)
		defer cancel()
		_ = c.notify(cancelCtx, "notifications/cancelled", map[string]any{"requestId": id, "reason": "cancelled by the user"})
		return ctx.Err()
	}
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	msg, err := json.Marshal(rpcMessage{JSONRPC: "2.0", Method: method, Params: rawJSON(params)})
	if err != nil {
		return err
	}
	return c.t.send(ctx, msg)
}

func (c *Client) deliver(data []byte) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return
	}
	if data[0] == '[' {
		var batch []json.RawMessage
		if json.Unmarshal(data, &batch) == nil {
			for _, m := range batch {
				c.deliver(m)
			}
		}
		return
	}
	var m rpcMessage
	if json.Unmarshal(data, &m) != nil {
		return
	}
	switch {
	case m.Method != "" && len(m.ID) != 0:
		go c.answer(m)
	case m.Method == "notifications/tools/list_changed":
		if c.onToolsChanged != nil {
			go c.onToolsChanged()
		}
	case m.Method == "":
		id, err := strconv.ParseInt(strings.Trim(string(m.ID), `"`), 10, 64)
		if err != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- m:
			default:
			}
		}
	}
}

func (c *Client) answer(req rpcMessage) {
	reply := rpcMessage{JSONRPC: "2.0", ID: req.ID}
	if req.Method == "ping" {
		reply.Result = json.RawMessage("{}")
	} else {
		reply.Error = &RPCError{Code: -32601, Message: "method not supported by mga: " + req.Method}
	}
	msg, err := json.Marshal(reply)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), replyTimeout)
	defer cancel()
	_ = c.t.send(ctx, msg)
}

func (c *Client) initialize(ctx context.Context) error {
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Instructions string `json:"instructions"`
	}
	err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "mga", "version": clientVersion},
	}, &res)
	if err != nil {
		return err
	}
	version := res.ProtocolVersion
	if version == "" {
		version = ProtocolVersion
	}
	c.t.setProtocolVersion(version)
	c.ServerName, c.ServerVersion, c.Instructions = res.ServerInfo.Name, res.ServerInfo.Version, strings.TrimSpace(res.Instructions)
	return c.notify(ctx, "notifications/initialized", nil)
}

func (c *Client) listTools(ctx context.Context) ([]ToolInfo, error) {
	var all []ToolInfo
	cursor := ""
	for range maxToolPages {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		var page struct {
			Tools      []ToolInfo `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		if err := c.call(ctx, "tools/list", params, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Tools...)
		if page.NextCursor == "" {
			return all, nil
		}
		cursor = page.NextCursor
	}
	return all, nil
}

func (c *Client) callTool(ctx context.Context, name string, args json.RawMessage) (*CallResult, error) {
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage("{}")
	}
	var res CallResult
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

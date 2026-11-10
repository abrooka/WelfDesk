package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	protocolVersion = "2025-06-18"
	handshakeLimit  = 20 * time.Second
	maxPages        = 20
)

type ServerConfig struct {
	Command string
	Args    []string
	Env     map[string]string
}

type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
}

type Status struct {
	Name  string `json:"name"`
	Tools int    `json:"tools"`
	Error string `json:"error,omitempty"`
}

type Client struct {
	name string
	cmd  *exec.Cmd

	stdin io.WriteCloser
	write sync.Mutex

	reqMu   sync.Mutex
	nextID  int64
	pending map[int64]chan response

	done     chan struct{}
	readDone chan struct{}
	once     sync.Once
	exitErr  error
	stderr   *tailBuffer
	exitOnce sync.Once
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("MCP error %d", e.Code)
	}
	return e.Message
}

func Start(ctx context.Context, name string, cfg ServerConfig) (*Client, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, errors.New("command is empty")
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	keys := make([]string, 0, len(cfg.Env))
	for key := range cfg.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+cfg.Env[key])
	}
	stderr := &tailBuffer{max: 4000}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	client := &Client{
		name:     name,
		cmd:      cmd,
		stdin:    stdin,
		pending:  map[int64]chan response{},
		done:     make(chan struct{}),
		readDone: make(chan struct{}),
		stderr:   stderr,
	}
	go client.readLoop(stdout)

	initCtx, cancel := context.WithTimeout(ctx, handshakeLimit)
	defer cancel()
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	err = client.request(initCtx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "weft", "version": "0.1.0"},
	}, &initResult)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) Name() string { return c.name }

func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	var tools []Tool
	cursor := ""
	for page := 0; page < maxPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var listed struct {
			Tools []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := c.request(ctx, "tools/list", params, &listed); err != nil {
			return nil, fmt.Errorf("tools/list: %w", err)
		}
		for _, item := range listed.Tools {
			if strings.TrimSpace(item.Name) == "" {
				continue
			}
			schema := item.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, Tool{Name: item.Name, Description: item.Description, Schema: schema})
		}
		if listed.NextCursor == "" {
			break
		}
		cursor = listed.NextCursor
	}
	return tools, nil
}

func (c *Client) Call(ctx context.Context, tool string, args json.RawMessage) (string, bool, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	params := struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{Name: tool, Arguments: args}
	if err := c.request(ctx, "tools/call", params, &result); err != nil {
		return "", true, fmt.Errorf("tools/call %s: %w", tool, err)
	}
	var b strings.Builder
	for _, item := range result.Content {
		switch item.Type {
		case "text":
			b.WriteString(item.Text)
		case "image":
			b.WriteString("[image]")
		case "resource":
			b.WriteString("[resource]")
		default:
			if item.Text != "" {
				b.WriteString(item.Text)
			} else {
				b.WriteString("[" + item.Type + "]")
			}
		}
	}
	return b.String(), result.IsError, nil
}

func (c *Client) request(ctx context.Context, method string, params any, out any) error {
	c.reqMu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.reqMu.Unlock()

	defer func() {
		c.reqMu.Lock()
		delete(c.pending, id)
		c.reqMu.Unlock()
	}()

	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("%s returned invalid JSON: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		err := c.ExitError()
		if err == nil {
			err = errors.New("server stopped")
		}
		return err
	}
}

func (c *Client) notify(method string, params any) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *Client) send(message any) error {
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.write.Lock()
	defer c.write.Unlock()
	if _, err := c.stdin.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("write to %s: %w", c.name, err)
	}
	return nil
}

func (c *Client) readLoop(stdout io.Reader) {
	defer close(c.readDone)
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			c.handle(line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.setExit(err)
			} else {
				c.setExit(nil)
			}
			return
		}
	}
}

func (c *Client) handle(line []byte) {
	var message struct {
		ID     int64           `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(line, &message); err != nil || message.ID == 0 {
		return
	}
	c.reqMu.Lock()
	ch, ok := c.pending[message.ID]
	delete(c.pending, message.ID)
	c.reqMu.Unlock()
	if !ok {
		return
	}
	ch <- response{Result: message.Result, Error: message.Error}
}

func (c *Client) setExit(err error) {
	c.once.Do(func() {
		c.exitErr = err
		close(c.done)
	})
}

func (c *Client) ExitError() error {
	select {
	case <-c.done:
	default:
		return nil
	}
	if c.exitErr != nil {
		return c.exitErr
	}
	tail := strings.TrimSpace(c.stderr.String())
	if tail != "" {
		return fmt.Errorf("server exited: %s", tail)
	}
	return errors.New("server exited")
}

func (c *Client) Close() {
	c.setExit(nil)
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	select {
	case <-c.readDone:
	case <-time.After(2 * time.Second):
	}
	c.exitOnce.Do(func() {
		_ = c.cmd.Wait()
	})
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = b.data[len(b.data)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

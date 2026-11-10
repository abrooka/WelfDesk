package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var message struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		switch message.Method {
		case "initialize":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message.ID,
				"result": map[string]any{
					"protocolVersion": protocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "fake", "version": "1"},
				},
			})
		case "notifications/initialized":
		case "tools/list":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message.ID,
				"result": map[string]any{
					"tools": []any{map[string]any{
						"name":        "echo",
						"description": "Echo text back.",
						"inputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{"text": map[string]any{"type": "string"}},
							"required":   []string{"text"},
						},
					}},
				},
			})
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(message.Params, &params)
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      message.ID,
				"result": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": "echo: " + fmt.Sprint(params.Arguments["text"])}},
				},
			})
		}
	}
	os.Exit(0)
}

func helperServer() ServerConfig {
	return ServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMCPHelperProcess"},
		Env:     map[string]string{"GO_WANT_MCP_HELPER": "1"},
	}
}

func TestClientListsAndCallsTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := Start(ctx, "fake", helperServer())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if tools[0].Schema["type"] != "object" {
		t.Fatalf("schema was not preserved: %+v", tools[0].Schema)
	}

	out, isError, err := client.Call(ctx, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if isError || out != "echo: hello" {
		t.Fatalf("unexpected call result: %q error=%v", out, isError)
	}
}

func TestConnectQualifiesToolsAndReportsStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	set := Connect(ctx, map[string]ServerConfig{"fake": helperServer(), "missing": {Command: "definitely-not-a-command-weft"}})
	defer set.Close()

	tools := set.Tools()
	if len(tools) != 1 || tools[0].Qualified != "mcp__fake__echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	out, _, err := set.Call(ctx, "mcp__fake__echo", json.RawMessage(`{"text":"set"}`))
	if err != nil || out != "echo: set" {
		t.Fatalf("unexpected set call: %q %v", out, err)
	}
	status := map[string]Status{}
	for _, item := range set.Status() {
		status[item.Name] = item
	}
	if status["fake"].Tools != 1 {
		t.Fatalf("unexpected fake status: %+v", status["fake"])
	}
	if status["missing"].Error == "" {
		t.Fatalf("missing server should report an error: %+v", status["missing"])
	}
}

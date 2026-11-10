package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/weftdesk/weft/internal/config"
)

type anthropicProvider struct {
	cfg    config.Provider
	client *http.Client
}

func newAnthropic(cfg config.Provider) *anthropicProvider {
	return &anthropicProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) endpoint() string {
	base := strings.TrimRight(p.cfg.BaseURL, "/")
	if strings.HasSuffix(base, "/messages") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

func (p *anthropicProvider) Stream(ctx context.Context, req Request) (<-chan Delta, error) {
	body := map[string]any{
		"model":      p.cfg.Model,
		"max_tokens": p.cfg.MaxTokens,
		"stream":     true,
		"messages":   anthropicMessages(req.Messages),
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		body["tools"] = anthropicTools(req.Tools)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return nil, fmt.Errorf("anthropic: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	ch := make(chan Delta, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		type toolState struct {
			id   string
			name string
			args strings.Builder
		}
		tools := map[int]*toolState{}
		var usage Usage

		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			var ev struct {
				Type         string `json:"type"`
				Index        int    `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
					StopReason  string `json:"stop_reason"`
				} `json:"delta"`
				Message struct {
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			switch ev.Type {
			case "message_start":
				usage.InputTokens = ev.Message.Usage.InputTokens
			case "content_block_start":
				if ev.ContentBlock.Type == "tool_use" {
					tools[ev.Index] = &toolState{id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
				}
			case "content_block_delta":
				switch ev.Delta.Type {
				case "text_delta":
					if !send(ctx, ch, Delta{Type: "text", Text: ev.Delta.Text}) {
						return
					}
				case "input_json_delta":
					if state := tools[ev.Index]; state != nil {
						state.args.WriteString(ev.Delta.PartialJSON)
					}
				}
			case "content_block_stop":
				if state := tools[ev.Index]; state != nil {
					args := state.args.String()
					if args == "" {
						args = "{}"
					}
					if !send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{ID: state.id, Name: state.name, Arguments: args}}) {
						return
					}
					delete(tools, ev.Index)
				}
			case "message_delta":
				if ev.Usage.OutputTokens > 0 {
					usage.OutputTokens = ev.Usage.OutputTokens
				}
			case "error":
				send(ctx, ch, Delta{Type: "error", Err: errors.New(ev.Error.Message)})
			}
		}
		if usage.InputTokens > 0 || usage.OutputTokens > 0 {
			send(ctx, ch, Delta{Type: "usage", Usage: &usage})
		}
		if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
			send(ctx, ch, Delta{Type: "error", Err: err})
		}
		send(ctx, ch, Delta{Type: "done"})
	}()
	return ch, nil
}

func anthropicMessages(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "assistant":
			content := make([]map[string]any, 0, len(msg.Content))
			for _, block := range msg.Content {
				switch block.Type {
				case BlockText:
					if block.Text != "" {
						content = append(content, map[string]any{"type": "text", "text": block.Text})
					}
				case BlockToolUse:
					if block.ToolUse != nil {
						var input any = map[string]any{}
						if len(block.ToolUse.Input) > 0 {
							_ = json.Unmarshal(block.ToolUse.Input, &input)
						}
						content = append(content, map[string]any{
							"type":  "tool_use",
							"id":    block.ToolUse.ID,
							"name":  block.ToolUse.Name,
							"input": input,
						})
					}
				}
			}
			if len(content) > 0 {
				out = append(out, map[string]any{"role": "assistant", "content": content})
			}
		default:
			content := make([]map[string]any, 0, len(msg.Content))
			for _, block := range msg.Content {
				switch block.Type {
				case BlockText:
					if block.Text != "" {
						content = append(content, map[string]any{"type": "text", "text": block.Text})
					}
				case BlockToolResult:
					if block.ToolResult != nil {
						content = append(content, map[string]any{
							"type":        "tool_result",
							"tool_use_id": block.ToolResult.ToolUseID,
							"content":     block.ToolResult.Content,
							"is_error":    block.ToolResult.IsError,
						})
					}
				}
			}
			if len(content) > 0 {
				out = append(out, map[string]any{"role": "user", "content": content})
			}
		}
	}
	return out
}

func anthropicTools(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.Schema,
		})
	}
	return out
}

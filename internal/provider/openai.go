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

type openAIProvider struct {
	cfg    config.Provider
	client *http.Client
}

func newOpenAI(cfg config.Provider) *openAIProvider {
	return &openAIProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (p *openAIProvider) Name() string { return "openai" }

func (p *openAIProvider) endpoint() string {
	base := strings.TrimRight(p.cfg.BaseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

func (p *openAIProvider) Stream(ctx context.Context, req Request) (<-chan Delta, error) {
	body := map[string]any{
		"model":       p.cfg.Model,
		"messages":    openAIMessages(req.System, req.Messages),
		"stream":      true,
		"max_tokens":  p.cfg.MaxTokens,
		"temperature": req.Temperature,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAITools(req.Tools)
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
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return nil, fmt.Errorf("openai: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	ch := make(chan Delta, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		type callState struct {
			id   string
			name string
			args strings.Builder
		}
		calls := map[int]*callState{}
		var usage *Usage

		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if chunk.Usage != nil {
				usage = &Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					if !send(ctx, ch, Delta{Type: "text", Text: choice.Delta.Content}) {
						return
					}
				}
				for _, tc := range choice.Delta.ToolCalls {
					state := calls[tc.Index]
					if state == nil {
						state = &callState{}
						calls[tc.Index] = state
					}
					if tc.ID != "" {
						state.id = tc.ID
					}
					if tc.Function.Name != "" {
						state.name += tc.Function.Name
					}
					state.args.WriteString(tc.Function.Arguments)
				}
			}
		}
		for i := 0; i < len(calls); i++ {
			state, ok := calls[i]
			if !ok || state.name == "" {
				continue
			}
			args := state.args.String()
			if args == "" {
				args = "{}"
			}
			if !send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{ID: state.id, Name: state.name, Arguments: args}}) {
				return
			}
		}
		if usage != nil {
			send(ctx, ch, Delta{Type: "usage", Usage: usage})
		}
		if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
			send(ctx, ch, Delta{Type: "error", Err: err})
		}
		send(ctx, ch, Delta{Type: "done"})
	}()
	return ch, nil
}

func openAIMessages(system string, messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages)+1)
	if system != "" {
		out = append(out, map[string]any{"role": "system", "content": system})
	}
	for _, msg := range messages {
		switch msg.Role {
		case "assistant":
			text := textOfBlocks(msg.Content)
			var toolCalls []map[string]any
			for _, block := range msg.Content {
				if block.Type == BlockToolUse && block.ToolUse != nil {
					args := string(block.ToolUse.Input)
					if args == "" {
						args = "{}"
					}
					toolCalls = append(toolCalls, map[string]any{
						"id":   block.ToolUse.ID,
						"type": "function",
						"function": map[string]any{
							"name":      block.ToolUse.Name,
							"arguments": args,
						},
					})
				}
			}
			m := map[string]any{"role": "assistant", "content": text}
			if len(toolCalls) > 0 {
				m["tool_calls"] = toolCalls
			}
			out = append(out, m)
		case "tool":
			for _, block := range msg.Content {
				if block.Type == BlockToolResult && block.ToolResult != nil {
					out = append(out, map[string]any{
						"role":         "tool",
						"tool_call_id": block.ToolResult.ToolUseID,
						"content":      block.ToolResult.Content,
					})
				}
			}
		default:
			for _, block := range msg.Content {
				if block.Type == BlockToolResult && block.ToolResult != nil {
					out = append(out, map[string]any{
						"role":         "tool",
						"tool_call_id": block.ToolResult.ToolUseID,
						"content":      block.ToolResult.Content,
					})
				}
			}
			text := textOfBlocks(msg.Content)
			if text != "" {
				out = append(out, map[string]any{"role": "user", "content": text})
			}
		}
	}
	return out
}

func openAITools(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Schema,
			},
		})
	}
	return out
}

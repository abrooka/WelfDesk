package provider

import (
	"context"
	"strings"
	"time"
)

type mockProvider struct{}

func (p *mockProvider) Name() string { return "mock" }

func (p *mockProvider) Stream(ctx context.Context, req Request) (<-chan Delta, error) {
	ch := make(chan Delta, 16)
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			last = textOfBlocks(req.Messages[i].Content)
			break
		}
	}

	// Tiny offline tool demos: "/read <path>", "/bash <command>", "/ask" and "/plan".
	trimmed := strings.TrimSpace(last)
	if strings.HasPrefix(trimmed, "/read ") || strings.HasPrefix(trimmed, "/bash ") || trimmed == "/ask" || trimmed == "/plan" {
		go func() {
			defer close(ch)
			switch {
			case strings.HasPrefix(trimmed, "/read "):
				path := strings.TrimSpace(trimmed[len("/read "):])
				send(ctx, ch, Delta{Type: "text", Text: "Reading " + path + " from the workspace runner."})
				send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{
					ID:        "mock-read",
					Name:      "read_file",
					Arguments: `{"path":"` + escapeJSON(path) + `"}`,
				}})
			case trimmed == "/plan":
				send(ctx, ch, Delta{Type: "text", Text: "Here is the plan I will follow."})
				send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{
					ID:   "mock-plan",
					Name: "todo_write",
					Arguments: `{"todos":[` +
						`{"id":"inspect","content":"Inspect the current implementation","status":"completed"},` +
						`{"id":"change","content":"Make the requested change","status":"in_progress"},` +
						`{"id":"verify","content":"Run tests and verify behavior","status":"pending"}]}`,
				}})
			case trimmed == "/ask":
				send(ctx, ch, Delta{Type: "text", Text: "I need a couple of decisions before continuing."})
				send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{
					ID:   "mock-ask",
					Name: "ask_user",
					Arguments: `{"questions":[` +
						`{"id":"scope","header":"Scope","question":"Which part should change?","options":[` +
						`{"label":"Desktop","description":"The Electron app in apps/desktop"},` +
						`{"label":"Server","description":"The Go daemon in internal"}]},` +
						`{"id":"extras","header":"Extras","question":"What should the change include?","multiSelect":true,"options":[` +
						`{"label":"Tests"},{"label":"Preview"},{"label":"Notes"}]}]}`,
				}})
			default:
				command := strings.TrimSpace(trimmed[len("/bash "):])
				send(ctx, ch, Delta{Type: "text", Text: "Running the command through the workspace runner."})
				send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{
					ID:        "mock-bash",
					Name:      "bash",
					Arguments: `{"command":"` + escapeJSON(command) + `"}`,
				}})
			}
			send(ctx, ch, Delta{Type: "done"})
		}()
		return ch, nil
	}

	text := "Mock provider is active. Configure OpenAI-compatible or Anthropic in Settings to use a real model."
	if strings.TrimSpace(last) != "" {
		text += "\n\nYou said: " + last
	}
	go func() {
		defer close(ch)
		for _, part := range chunkText(text, 18) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(18 * time.Millisecond):
			}
			if !send(ctx, ch, Delta{Type: "text", Text: part}) {
				return
			}
		}
		send(ctx, ch, Delta{Type: "usage", Usage: &Usage{InputTokens: 0, OutputTokens: 0}})
		send(ctx, ch, Delta{Type: "done"})
	}()
	return ch, nil
}

func chunkText(s string, size int) []string {
	if size <= 0 {
		size = 16
	}
	var out []string
	for len(s) > 0 {
		n := size
		if len(s) < n {
			n = len(s)
		}
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

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

	// Tiny offline tool demos: "/read <path>", "/bash <command>", "/ask", "/plan", "/task",
	// "/remember <text>", "/skill [name]" and "/mcp".
	trimmed := strings.TrimSpace(last)
	if strings.HasPrefix(trimmed, "/read ") || strings.HasPrefix(trimmed, "/bash ") ||
		strings.HasPrefix(trimmed, "/remember ") ||
		trimmed == "/ask" || trimmed == "/plan" || trimmed == "/task" || trimmed == "/mcp" ||
		trimmed == "/skill" || strings.HasPrefix(trimmed, "/skill ") {
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
			case trimmed == "/task":
				send(ctx, ch, Delta{Type: "text", Text: "Delegating a closer look to a subagent."})
				send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{
					ID:   "mock-task",
					Name: "task",
					Arguments: `{"description":"Inspect the workspace",` +
						`"prompt":"List the workspace folders and report what this project appears to do."}`,
				}})
			case strings.HasPrefix(trimmed, "/remember "):
				text := strings.TrimSpace(trimmed[len("/remember "):])
				send(ctx, ch, Delta{Type: "text", Text: "Saving that for future sessions."})
				send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{
					ID:        "mock-remember",
					Name:      "remember",
					Arguments: `{"text":"` + escapeJSON(text) + `"}`,
				}})
			case trimmed == "/skill" || strings.HasPrefix(trimmed, "/skill "):
				name := strings.TrimSpace(strings.TrimPrefix(trimmed, "/skill"))
				if name == "" {
					name = firstSkillName(req.System)
				}
				if name == "" {
					send(ctx, ch, Delta{Type: "text", Text: "No skills are available. Add a SKILL.md under .weft/skills in the workspace."})
					break
				}
				send(ctx, ch, Delta{Type: "text", Text: "Loading the " + name + " skill."})
				send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{
					ID:        "mock-skill",
					Name:      "use_skill",
					Arguments: `{"name":"` + escapeJSON(name) + `"}`,
				}})
			case trimmed == "/mcp":
				name := ""
				for _, tool := range req.Tools {
					if strings.HasPrefix(tool.Name, "mcp__") {
						name = tool.Name
						break
					}
				}
				if name == "" {
					send(ctx, ch, Delta{Type: "text", Text: "No MCP tools are connected. Add a server in Settings."})
					break
				}
				send(ctx, ch, Delta{Type: "text", Text: "Calling " + name + " over MCP."})
				send(ctx, ch, Delta{Type: "tool_call", ToolCall: &ToolCall{ID: "mock-mcp", Name: name, Arguments: `{}`}})
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

func firstSkillName(system string) string {
	inSkills := false
	for _, line := range strings.Split(system, "\n") {
		line = strings.TrimSpace(line)
		if line == "# Skills" {
			inSkills = true
			continue
		}
		if !inSkills {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return ""
		}
		if strings.HasPrefix(line, "- ") {
			name := strings.TrimPrefix(line, "- ")
			if i := strings.Index(name, ":"); i > 0 {
				name = name[:i]
			}
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

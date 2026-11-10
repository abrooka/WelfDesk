package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/mcp"
	"github.com/weftdesk/weft/internal/provider"
	"github.com/weftdesk/weft/internal/tools"
)

const maxSubagentTurns = 12

func taskTool() provider.Tool {
	return provider.Tool{
		Name:        "task",
		Description: "Delegate a self-contained research or implementation task to a subagent. The subagent works with the workspace tools in an isolated conversation and returns a concise report. Use it for independent exploration or sizeable side work; the subagent cannot ask the user questions.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string", "description": "Short label shown to the user."},
				"prompt":      map[string]any{"type": "string", "description": "Complete instructions for the subagent, including what to report back."},
			},
			"required": []string{"description", "prompt"},
		},
	}
}

func (a *Agent) runTask(ctx context.Context, sessionID, runID, args string, result *domain.ToolResultData) {
	var in struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		result.IsError = true
		result.Output = "task arguments are invalid: " + err.Error()
		return
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Prompt == "" {
		result.IsError = true
		result.Output = "task requires a prompt"
		return
	}

	sess, err := a.store.Session(sessionID)
	if err != nil {
		result.IsError = true
		result.Output = err.Error()
		return
	}
	ws, err := a.store.Workspace(sess.WorkspaceID)
	if err != nil {
		result.IsError = true
		result.Output = err.Error()
		return
	}
	a.cfgMu.RLock()
	prov := a.prov
	a.cfgMu.RUnlock()
	if prov == nil {
		result.IsError = true
		result.Output = "no model provider configured"
		return
	}
	cfg := a.Config()

	roots := make([]string, 0, len(ws.Folders))
	for _, folder := range ws.Folders {
		roots = append(roots, folder.Path)
	}
	registry := tools.NewRegistry(roots)
	mcpSet := a.mcpClient()
	toolDefs := append(registry.Definitions(), mcpToolDefs(mcpSet)...)
	system := systemPrompt(ws, cfg.DataDir) + memoryPrompt(a.memoryText()) + "\n# Subagent\n\n" +
		"You are a subagent working for another Weft agent. Complete the task on your own and report back with a concise, factual summary: what you found or changed, relevant file paths, and anything the parent agent must know. The user does not see your conversation, so put everything important in the report. Do not ask the user questions.\n"

	history := []provider.Message{{
		Role:    "user",
		Content: []provider.Block{{Type: provider.BlockText, Text: in.Prompt}},
	}}
	lastText := ""
	for turn := 0; turn < maxSubagentTurns; turn++ {
		if ctx.Err() != nil {
			result.IsError = true
			result.Output = "Run cancelled."
			return
		}
		text, calls, streamErr := a.collectStream(ctx, provider.Request{
			Model:       cfg.Provider.Model,
			System:      system,
			Messages:    history,
			Tools:       toolDefs,
			MaxTokens:   cfg.Provider.MaxTokens,
			Temperature: 0.2,
		}, prov)
		if streamErr != nil {
			result.IsError = true
			result.Output = "Subagent failed: " + streamErr.Error()
			return
		}
		lastText = text

		var blocks []provider.Block
		if strings.TrimSpace(text) != "" {
			blocks = append(blocks, provider.Block{Type: provider.BlockText, Text: text})
		}
		for _, call := range calls {
			input := json.RawMessage(call.Arguments)
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			blocks = append(blocks, provider.Block{
				Type:    provider.BlockToolUse,
				ToolUse: &provider.ToolUse{ID: call.ID, Name: call.Name, Input: input},
			})
		}
		if len(blocks) > 0 {
			history = append(history, provider.Message{Role: "assistant", Content: blocks})
		}
		if len(calls) == 0 {
			report := strings.TrimSpace(text)
			if report == "" {
				report = "Subagent finished without a report."
			}
			if len(report) > 100_000 {
				report = report[:100_000] + "\n… report truncated"
			}
			result.Output = report
			return
		}

		for _, call := range calls {
			if ctx.Err() != nil {
				result.IsError = true
				result.Output = "Run cancelled."
				return
			}
			output, isError := a.subagentTool(ctx, sessionID, runID, registry, mcpSet, call)
			history = append(history, provider.Message{
				Role: "user",
				Content: []provider.Block{{
					Type: provider.BlockToolResult,
					ToolResult: &provider.ToolResult{
						ToolUseID: call.ID,
						Content:   output,
						IsError:   isError,
					},
				}},
			})
		}
	}

	report := strings.TrimSpace(lastText)
	if report == "" {
		report = "Subagent stopped at the turn limit without a report."
	} else {
		report += fmt.Sprintf("\n\n(Subagent stopped at the %d-turn limit.)", maxSubagentTurns)
	}
	result.Output = report
}

func (a *Agent) subagentTool(ctx context.Context, sessionID, runID string, registry *tools.Registry, mcpSet *mcp.Set, call provider.ToolCall) (string, bool) {
	switch call.Name {
	case "task", "todo_write", "ask_user", "remember":
		return fmt.Sprintf("%s is not available to a subagent", call.Name), true
	}
	allowed, reason := a.authorize(ctx, sessionID, runID, call)
	if !allowed {
		if reason == "" {
			reason = "Permission denied by the user."
		}
		return reason, true
	}
	var output string
	var isError bool
	if strings.HasPrefix(call.Name, "mcp__") {
		result := domain.ToolResultData{}
		a.callMCP(ctx, mcpSet, call, &result)
		output, isError = result.Output, result.IsError
	} else {
		out, err := registry.Execute(ctx, call.Name, call.Arguments)
		output = out.Output
		isError = out.IsError
		if err != nil {
			isError = true
			if output == "" {
				output = err.Error()
			} else {
				output += "\n" + err.Error()
			}
		}
		if !isError && out.Snapshot != nil {
			if err := a.saveCheckpoint(sessionID, runID, call, out); err != nil {
				output += "\n[checkpoint could not be saved: " + err.Error() + "]"
			}
		}
	}
	if len(output) > 100_000 {
		output = output[:100_000] + "\n… output truncated"
	}
	return output, isError
}

func (a *Agent) collectStream(ctx context.Context, req provider.Request, prov provider.Provider) (string, []provider.ToolCall, error) {
	ch, err := prov.Stream(ctx, req)
	if err != nil {
		return "", nil, err
	}
	var text strings.Builder
	var calls []provider.ToolCall
	for delta := range ch {
		if delta.Err != nil {
			return text.String(), calls, delta.Err
		}
		switch delta.Type {
		case "text":
			text.WriteString(delta.Text)
		case "tool_call":
			if delta.ToolCall != nil {
				calls = append(calls, *delta.ToolCall)
			}
		}
	}
	return text.String(), calls, nil
}

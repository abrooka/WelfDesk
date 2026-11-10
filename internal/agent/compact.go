package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/weftdesk/weft/internal/config"
	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
)

const (
	keepRecentMessages = 8
	maxTranscriptRunes = 200_000
	maxSummaryRunes    = 8_000
)

const compactionSystem = "You compact the conversation history of a coding agent. Keep the user's goals, decisions, constraints, exact file paths, edits made, important tool findings and open problems. Drop pleasantries, repetition and raw file dumps. Reply with the updated summary only."

// maybeCompact summarizes older messages when the estimated context grows past
// the configured threshold and replaces them with a session.compacted event.
func (a *Agent) maybeCompact(ctx context.Context, sessionID, runID string, cfg config.Config, prov provider.Provider, history []provider.Message, summary string) ([]provider.Message, string) {
	if cfg.CompactTokens <= 0 || len(history) <= keepRecentMessages || estimateTokens(history) <= cfg.CompactTokens {
		return history, summary
	}
	events, err := a.store.EventsAfter(sessionID, 0)
	if err != nil {
		return history, summary
	}
	boundary, _ := compactedBoundary(events)
	filtered := make([]domain.Event, 0, len(events))
	for _, ev := range events {
		if ev.Seq > boundary {
			filtered = append(filtered, ev)
		}
	}
	context := buildContext(filtered)
	cut := len(context) - keepRecentMessages
	for cut > 0 && isToolResultMessage(context[cut].msg) {
		cut--
	}
	if cut <= 0 || cut >= len(context) {
		return history, summary
	}

	var prompt strings.Builder
	if strings.TrimSpace(summary) != "" {
		prompt.WriteString("Previous summary:\n\n")
		prompt.WriteString(summary)
		prompt.WriteString("\n\n")
	}
	prompt.WriteString("Conversation to compact:\n\n")
	prompt.WriteString(renderTranscript(context[:cut]))

	maxTokens := cfg.Provider.MaxTokens
	if maxTokens > 2048 {
		maxTokens = 2048
	}
	compacted, _, err := a.collectStream(ctx, provider.Request{
		Model:       cfg.Provider.Model,
		System:      compactionSystem,
		Messages:    []provider.Message{{Role: "user", Content: []provider.Block{{Type: provider.BlockText, Text: prompt.String()}}}},
		MaxTokens:   maxTokens,
		Temperature: 0.2,
	}, prov)
	if err != nil || strings.TrimSpace(compacted) == "" {
		return history, summary
	}

	if _, err := a.store.Append(sessionID, domain.EventSessionCompacted, runID, domain.CompactedData{
		Summary:  clipRunes(strings.TrimSpace(compacted), maxSummaryRunes),
		UpToSeq:  context[cut-1].seq,
		Messages: cut,
	}); err != nil {
		return history, summary
	}
	newHistory, newSummary, err := a.history(sessionID)
	if err != nil {
		return history, summary
	}
	return newHistory, newSummary
}

func summaryPrompt(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	return "\n# Session summary\n\nEarlier messages in this conversation were compacted. Summary:\n\n" + summary + "\n"
}

func estimateTokens(messages []provider.Message) int {
	chars := 0
	for _, message := range messages {
		for _, block := range message.Content {
			switch block.Type {
			case provider.BlockText:
				chars += len(block.Text)
			case provider.BlockToolUse:
				if block.ToolUse != nil {
					chars += len(block.ToolUse.Name) + len(block.ToolUse.Input)
				}
			case provider.BlockToolResult:
				if block.ToolResult != nil {
					chars += len(block.ToolResult.Content)
				}
			}
		}
	}
	return chars/4 + len(messages)*3
}

func renderTranscript(messages []contextMessage) string {
	var b strings.Builder
	for _, item := range messages {
		fmt.Fprintf(&b, "\n## %s\n", item.msg.Role)
		for _, block := range item.msg.Content {
			switch block.Type {
			case provider.BlockText:
				b.WriteString(block.Text)
				b.WriteString("\n")
			case provider.BlockToolUse:
				if block.ToolUse != nil {
					fmt.Fprintf(&b, "[tool call %s] %s\n", block.ToolUse.Name, string(block.ToolUse.Input))
				}
			case provider.BlockToolResult:
				if block.ToolResult != nil {
					status := ""
					if block.ToolResult.IsError {
						status = " (error)"
					}
					fmt.Fprintf(&b, "[tool result%s] %s\n", status, block.ToolResult.Content)
				}
			}
		}
	}
	return tailRunes(strings.TrimSpace(b.String()), maxTranscriptRunes)
}

func clipRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func tailRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return "… earlier content omitted …\n" + string(runes[len(runes)-max:])
}

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/weftdesk/weft/internal/config"
)

const (
	BlockText       = "text"
	BlockToolUse    = "tool_use"
	BlockToolResult = "tool_result"
)

type ToolUse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ToolResult struct {
	ToolUseID string `json:"toolUseId"`
	Content   string `json:"content"`
	IsError   bool   `json:"isError"`
}

type Block struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	ToolUse    *ToolUse    `json:"toolUse,omitempty"`
	ToolResult *ToolResult `json:"toolResult,omitempty"`
}

type Message struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}

type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []Tool
	MaxTokens   int
	Temperature float64
}

type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Delta struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ToolCall *ToolCall `json:"toolCall,omitempty"`
	Usage    *Usage    `json:"usage,omitempty"`
	Err      error     `json:"-"`
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) (<-chan Delta, error)
}

func New(cfg config.Provider) (Provider, error) {
	switch strings.ToLower(cfg.Kind) {
	case "", "mock":
		return &mockProvider{}, nil
	case "openai", "openai-compatible":
		if cfg.APIKey == "" {
			return nil, errors.New("provider apiKey is not set")
		}
		return newOpenAI(cfg), nil
	case "anthropic":
		if cfg.APIKey == "" {
			return nil, errors.New("provider apiKey is not set")
		}
		return newAnthropic(cfg), nil
	default:
		return nil, errors.New("unknown provider kind: " + cfg.Kind)
	}
}

func send(ctx context.Context, ch chan<- Delta, d Delta) bool {
	select {
	case ch <- d:
		return true
	case <-ctx.Done():
		return false
	}
}

func textOfBlocks(blocks []Block) string {
	var b strings.Builder
	for _, block := range blocks {
		if block.Type == BlockText {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

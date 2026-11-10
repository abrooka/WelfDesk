package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/weftdesk/weft/internal/config"
	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
	"github.com/weftdesk/weft/internal/store"
	"github.com/weftdesk/weft/internal/tools"
)

var ErrBusy = errors.New("session is already running")

type requestSnapshot struct {
	RequestID string             `json:"requestId"`
	Provider  string             `json:"provider"`
	Model     string             `json:"model"`
	System    string             `json:"system"`
	Messages  []provider.Message `json:"messages"`
	Tools     []provider.Tool    `json:"tools"`
	StartedAt time.Time          `json:"startedAt"`
}

type requestResult struct {
	RequestID  string                `json:"requestId"`
	DurationMs int64                 `json:"durationMs"`
	Usage      provider.Usage        `json:"usage"`
	Text       string                `json:"text"`
	ToolCalls  []domain.ToolCallData `json:"toolCalls,omitempty"`
	Error      string                `json:"error,omitempty"`
}

type Agent struct {
	store *store.Store

	cfgMu sync.RWMutex
	cfg   config.Config
	prov  provider.Provider

	runMu sync.Mutex
	runs  map[string]context.CancelFunc

	permMu  sync.Mutex
	pending map[string]chan string
}

func New(st *store.Store, cfg config.Config, prov provider.Provider) *Agent {
	return &Agent{
		store:   st,
		cfg:     cfg,
		prov:    prov,
		runs:    map[string]context.CancelFunc{},
		pending: map[string]chan string{},
	}
}

func (a *Agent) Config() config.Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg
}

func (a *Agent) UpdateConfig(cfg config.Config, prov provider.Provider) {
	a.cfgMu.Lock()
	a.cfg = cfg
	a.prov = prov
	a.cfgMu.Unlock()
}

func (a *Agent) Running(sessionID string) bool {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	_, ok := a.runs[sessionID]
	return ok
}

func (a *Agent) Send(ctx context.Context, sessionID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("message is empty")
	}
	sess, err := a.store.Session(sessionID)
	if err != nil {
		return err
	}
	if _, err := a.store.Workspace(sess.WorkspaceID); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	a.runMu.Lock()
	if _, busy := a.runs[sessionID]; busy {
		a.runMu.Unlock()
		return ErrBusy
	}
	runCtx, cancel := context.WithCancel(context.Background())
	a.runs[sessionID] = cancel
	a.runMu.Unlock()

	if _, err := a.store.Append(sessionID, domain.EventUserMessage, "", domain.UserMessageData{Text: text}); err != nil {
		a.finishRun(sessionID)
		return err
	}
	if sess.Title == "" || sess.Title == "New session" {
		title := text
		if len([]rune(title)) > 40 {
			title = string([]rune(title)[:40]) + "…"
		}
		_ = a.store.SetSessionTitle(sessionID, title)
	}
	if err := a.store.SetSessionStatus(sessionID, domain.SessionRunning); err != nil {
		a.finishRun(sessionID)
		return err
	}
	go a.loop(runCtx, sessionID)
	return nil
}

func (a *Agent) Cancel(sessionID string) bool {
	a.runMu.Lock()
	cancel, ok := a.runs[sessionID]
	a.runMu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func (a *Agent) CancelAll() {
	a.runMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(a.runs))
	for _, cancel := range a.runs {
		cancels = append(cancels, cancel)
	}
	a.runMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (a *Agent) finishRun(sessionID string) {
	a.runMu.Lock()
	delete(a.runs, sessionID)
	a.runMu.Unlock()
}

func (a *Agent) loop(ctx context.Context, sessionID string) {
	runID := newID()
	defer a.finishRun(sessionID)

	fail := func(message string) {
		_, _ = a.store.Append(sessionID, domain.EventError, runID, domain.ErrorData{Message: message})
		_ = a.store.SetSessionStatus(sessionID, domain.SessionError)
	}
	stop := func(message string) {
		_, _ = a.store.Append(sessionID, domain.EventError, runID, domain.ErrorData{Message: message})
		_ = a.store.SetSessionStatus(sessionID, domain.SessionIdle)
	}

	if _, err := a.store.Append(sessionID, domain.EventTurnStarted, runID, domain.TurnData{RunID: runID}); err != nil {
		fail(err.Error())
		return
	}

	cfg := a.Config()
	a.cfgMu.RLock()
	prov := a.prov
	a.cfgMu.RUnlock()
	if prov == nil {
		fail("no model provider configured")
		return
	}

	sess, err := a.store.Session(sessionID)
	if err != nil {
		fail(err.Error())
		return
	}
	ws, err := a.store.Workspace(sess.WorkspaceID)
	if err != nil {
		fail(err.Error())
		return
	}
	roots := make([]string, 0, len(ws.Folders))
	for _, folder := range ws.Folders {
		roots = append(roots, folder.Path)
	}
	registry := tools.NewRegistry(roots)
	system := systemPrompt(ws)
	toolDefs := registry.Definitions()

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		history, err := a.history(sessionID)
		if err != nil {
			fail(err.Error())
			return
		}
		reqID := newID()
		snapshot := requestSnapshot{
			RequestID: reqID,
			Provider:  prov.Name(),
			Model:     cfg.Provider.Model,
			System:    system,
			Messages:  history,
			Tools:     toolDefs,
			StartedAt: time.Now().UTC(),
		}
		if _, err := a.store.Append(sessionID, domain.EventRequestStarted, runID, snapshot); err != nil {
			fail(err.Error())
			return
		}

		started := time.Now()
		text, calls, usage, streamErr := a.stream(ctx, sessionID, runID, provider.Request{
			Model:       cfg.Provider.Model,
			System:      system,
			Messages:    history,
			Tools:       toolDefs,
			MaxTokens:   cfg.Provider.MaxTokens,
			Temperature: 0.2,
		}, prov)

		result := requestResult{
			RequestID:  reqID,
			DurationMs: time.Since(started).Milliseconds(),
			Usage:      usage,
			Text:       text,
		}
		for _, call := range calls {
			result.ToolCalls = append(result.ToolCalls, domain.ToolCallData{
				ID: call.ID, Name: call.Name, Arguments: call.Arguments,
			})
		}
		if streamErr != nil {
			result.Error = streamErr.Error()
		}
		_, _ = a.store.Append(sessionID, domain.EventRequestCompleted, runID, result)

		if streamErr != nil {
			if ctx.Err() != nil {
				stop("Run cancelled")
			} else {
				fail(streamErr.Error())
			}
			return
		}

		assistant := domain.AssistantMessageData{Text: text}
		for _, call := range calls {
			assistant.ToolCalls = append(assistant.ToolCalls, domain.ToolCallData{
				ID: call.ID, Name: call.Name, Arguments: call.Arguments,
			})
		}
		if _, err := a.store.Append(sessionID, domain.EventAssistantMessage, runID, assistant); err != nil {
			fail(err.Error())
			return
		}

		if len(calls) == 0 {
			_, _ = a.store.Append(sessionID, domain.EventTurnCompleted, runID, domain.TurnData{RunID: runID})
			_ = a.store.SetSessionStatus(sessionID, domain.SessionIdle)
			return
		}

		for _, call := range calls {
			if ctx.Err() != nil {
				stop("Run cancelled")
				return
			}
			a.executeTool(ctx, sessionID, runID, registry, call)
		}
	}

	stop(fmt.Sprintf("Turn limit reached (%d). Send another message to continue.", cfg.MaxTurns))
}

func (a *Agent) stream(ctx context.Context, sessionID, runID string, req provider.Request, prov provider.Provider) (string, []provider.ToolCall, provider.Usage, error) {
	ch, err := prov.Stream(ctx, req)
	if err != nil {
		return "", nil, provider.Usage{}, err
	}
	var text strings.Builder
	var calls []provider.ToolCall
	var usage provider.Usage
	for delta := range ch {
		if delta.Err != nil {
			return text.String(), calls, usage, delta.Err
		}
		switch delta.Type {
		case "text":
			if delta.Text == "" {
				continue
			}
			text.WriteString(delta.Text)
			a.store.Publish(sessionID, domain.EventAssistantDelta, runID, map[string]string{
				"text": delta.Text,
			})
		case "tool_call":
			if delta.ToolCall != nil {
				calls = append(calls, *delta.ToolCall)
			}
		case "usage":
			if delta.Usage != nil {
				usage = *delta.Usage
			}
		}
	}
	return text.String(), calls, usage, nil
}

func (a *Agent) executeTool(ctx context.Context, sessionID, runID string, registry *tools.Registry, call provider.ToolCall) {
	callData := domain.ToolCallData{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
	_, _ = a.store.Append(sessionID, domain.EventToolCall, runID, callData)

	result := domain.ToolResultData{ToolCallID: call.ID, Name: call.Name}
	started := time.Now()

	allowed, reason := a.authorize(ctx, sessionID, runID, call)
	if !allowed {
		result.IsError = true
		result.Output = "Permission denied by the user."
		if reason != "" {
			result.Output = reason
		}
	} else {
		out, err := registry.Execute(ctx, call.Name, call.Arguments)
		result.Output = out.Output
		result.Diff = out.Diff
		result.Path = out.Path
		result.IsError = out.IsError
		if err != nil {
			result.IsError = true
			if result.Output == "" {
				result.Output = err.Error()
			} else {
				result.Output = result.Output + "\n" + err.Error()
			}
		}
	}
	if len(result.Output) > 100_000 {
		result.Output = result.Output[:100_000] + "\n… output truncated"
	}
	result.DurationMs = time.Since(started).Milliseconds()
	_, _ = a.store.Append(sessionID, domain.EventToolResult, runID, result)
}

func (a *Agent) authorize(ctx context.Context, sessionID, runID string, call provider.ToolCall) (bool, string) {
	mode := a.Config().PermissionMode
	if mode == "yolo" {
		return true, ""
	}
	if readOnlyTool(call.Name) {
		return true, ""
	}
	if mode == "acceptEdits" && (call.Name == "write_file" || call.Name == "edit_file") {
		return true, ""
	}

	reqID := newID()
	ch := make(chan string, 1)
	a.permMu.Lock()
	a.pending[reqID] = ch
	a.permMu.Unlock()
	defer func() {
		a.permMu.Lock()
		delete(a.pending, reqID)
		a.permMu.Unlock()
	}()

	_, _ = a.store.Append(sessionID, domain.EventPermissionRequest, runID, domain.PermissionRequestData{
		RequestID: reqID,
		Tool:      call.Name,
		Input:     call.Arguments,
		Reason:    "This tool can modify your workspace.",
	})

	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()
	select {
	case decision := <-ch:
		return decision == "allow", ""
	case <-ctx.Done():
		return false, "Run cancelled."
	case <-timeout.C:
		return false, "Permission request timed out."
	}
}

func (a *Agent) ResolvePermission(sessionID, requestID, decision string) error {
	if decision != "allow" && decision != "deny" {
		return errors.New("decision must be allow or deny")
	}
	a.permMu.Lock()
	ch, ok := a.pending[requestID]
	a.permMu.Unlock()
	if !ok {
		return errors.New("permission request is no longer pending")
	}
	select {
	case ch <- decision:
	default:
		return errors.New("permission request already resolved")
	}
	_, err := a.store.Append(sessionID, domain.EventPermissionResolved, "", domain.PermissionResolvedData{
		RequestID: requestID,
		Decision:  decision,
	})
	return err
}

func (a *Agent) history(sessionID string) ([]provider.Message, error) {
	events, err := a.store.EventsAfter(sessionID, 0)
	if err != nil {
		return nil, err
	}
	var messages []provider.Message
	for _, ev := range events {
		switch ev.Type {
		case domain.EventUserMessage:
			var data domain.UserMessageData
			if decode(ev.Data, &data) != nil || data.Text == "" {
				continue
			}
			messages = append(messages, provider.Message{
				Role:    "user",
				Content: []provider.Block{{Type: provider.BlockText, Text: data.Text}},
			})
		case domain.EventAssistantMessage:
			var data domain.AssistantMessageData
			if decode(ev.Data, &data) != nil {
				continue
			}
			var blocks []provider.Block
			if data.Text != "" {
				blocks = append(blocks, provider.Block{Type: provider.BlockText, Text: data.Text})
			}
			for _, call := range data.ToolCalls {
				input := json.RawMessage(call.Arguments)
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, provider.Block{
					Type: provider.BlockToolUse,
					ToolUse: &provider.ToolUse{
						ID:    call.ID,
						Name:  call.Name,
						Input: input,
					},
				})
			}
			if len(blocks) == 0 {
				continue
			}
			messages = append(messages, provider.Message{Role: "assistant", Content: blocks})
		case domain.EventToolResult:
			var data domain.ToolResultData
			if decode(ev.Data, &data) != nil {
				continue
			}
			block := provider.Block{
				Type: provider.BlockToolResult,
				ToolResult: &provider.ToolResult{
					ToolUseID: data.ToolCallID,
					Content:   data.Output,
					IsError:   data.IsError,
				},
			}
			if n := len(messages); n > 0 && messages[n-1].Role == "user" && isToolResultMessage(messages[n-1]) {
				messages[n-1].Content = append(messages[n-1].Content, block)
			} else {
				messages = append(messages, provider.Message{
					Role:    "user",
					Content: []provider.Block{block},
				})
			}
		}
	}
	return messages, nil
}

func isToolResultMessage(message provider.Message) bool {
	for _, block := range message.Content {
		if block.Type != provider.BlockToolResult {
			return false
		}
	}
	return len(message.Content) > 0
}

func systemPrompt(ws domain.Workspace) string {
	var b strings.Builder
	b.WriteString("You are Weft, an agentic coding assistant running on the user's machine.\n")
	b.WriteString("You can inspect and modify the workspace with the provided tools. ")
	b.WriteString("Read a file before editing it, keep changes focused, and verify your work when possible.\n\n")
	b.WriteString("Workspace folders:\n")
	for _, folder := range ws.Folders {
		fmt.Fprintf(&b, "- %s: %s\n", folder.Name, folder.Path)
	}
	for _, folder := range ws.Folders {
		path := filepath.Join(folder.Path, "AGENTS.md")
		raw, err := os.ReadFile(path)
		if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		text := string(raw)
		if len(text) > 20_000 {
			text = text[:20_000] + "\n… truncated"
		}
		fmt.Fprintf(&b, "\n# Instructions from %s\n\n%s\n", path, text)
	}
	return b.String()
}

func readOnlyTool(name string) bool {
	switch name {
	case "read_file", "glob", "grep":
		return true
	}
	return false
}

func decode(data any, out any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

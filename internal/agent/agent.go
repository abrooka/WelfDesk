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

	qMu       sync.Mutex
	questions map[string]pendingQuestion

	memMu sync.Mutex
}

type pendingQuestion struct {
	sessionID string
	ch        chan []domain.QuestionAnswer
}

func New(st *store.Store, cfg config.Config, prov provider.Provider) *Agent {
	return &Agent{
		store:     st,
		cfg:       cfg,
		prov:      prov,
		runs:      map[string]context.CancelFunc{},
		pending:   map[string]chan string{},
		questions: map[string]pendingQuestion{},
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
	baseSystem := systemPrompt(ws, cfg.DataDir)
	toolDefs := append(registry.Definitions(), askUserTool(), todoTool(), taskTool(), rememberTool())

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		history, summary, err := a.history(sessionID)
		if err != nil {
			fail(err.Error())
			return
		}
		history, summary = a.maybeCompact(ctx, sessionID, runID, cfg, prov, history, summary)
		system := baseSystem + memoryPrompt(a.memoryText()) + planPrompt(a.Todos(sessionID)) + summaryPrompt(summary)
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

	switch {
	case call.Name == "todo_write":
		a.updateTodos(sessionID, runID, call.Arguments, &result)
	case call.Name == "task":
		a.runTask(ctx, sessionID, runID, call.Arguments, &result)
	case call.Name == "remember":
		a.remember(call.Arguments, &result)
	case call.Name == "ask_user":
		a.askUser(ctx, sessionID, runID, call, &result, started)
		return
	default:
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
			if !result.IsError && out.Snapshot != nil {
				if err := a.saveCheckpoint(sessionID, runID, call, out); err != nil {
					result.Output += "\n[checkpoint could not be saved: " + err.Error() + "]"
				}
			}
		}
	}
	if len(result.Output) > 100_000 {
		result.Output = result.Output[:100_000] + "\n… output truncated"
	}
	result.DurationMs = time.Since(started).Milliseconds()
	_, _ = a.store.Append(sessionID, domain.EventToolResult, runID, result)
}

func (a *Agent) askUser(ctx context.Context, sessionID, runID string, call provider.ToolCall, result *domain.ToolResultData, started time.Time) {
	finish := func() {
		if len(result.Output) > 100_000 {
			result.Output = result.Output[:100_000] + "\n… output truncated"
		}
		result.DurationMs = time.Since(started).Milliseconds()
		_, _ = a.store.Append(sessionID, domain.EventToolResult, runID, *result)
	}
	fail := func(message string) {
		result.IsError = true
		result.Output = message
		finish()
	}

	var in struct {
		Questions []domain.Question `json:"questions"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &in); err != nil {
		fail("ask_user arguments are invalid: " + err.Error())
		return
	}
	if len(in.Questions) == 0 {
		fail("ask_user requires at least one question")
		return
	}
	if len(in.Questions) > 8 {
		in.Questions = in.Questions[:8]
	}
	for i := range in.Questions {
		if strings.TrimSpace(in.Questions[i].ID) == "" {
			in.Questions[i].ID = fmt.Sprintf("q%d", i+1)
		}
	}

	reqID := newID()
	ch := make(chan []domain.QuestionAnswer, 1)
	a.qMu.Lock()
	a.questions[reqID] = pendingQuestion{sessionID: sessionID, ch: ch}
	a.qMu.Unlock()
	defer func() {
		a.qMu.Lock()
		delete(a.questions, reqID)
		a.qMu.Unlock()
	}()

	_, _ = a.store.Append(sessionID, domain.EventQuestionRequest, runID, domain.QuestionRequestData{
		RequestID:  reqID,
		ToolCallID: call.ID,
		Questions:  in.Questions,
	})

	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()

	var answers []domain.QuestionAnswer
	reason := ""
	select {
	case answers = <-ch:
	case <-ctx.Done():
		reason = "Run cancelled."
	case <-timeout.C:
		reason = "Question request timed out."
	}
	if reason != "" {
		_, _ = a.store.Append(sessionID, domain.EventQuestionResolved, runID, domain.QuestionResolvedData{
			RequestID: reqID,
			Reason:    reason,
		})
		fail(reason)
		return
	}
	_, _ = a.store.Append(sessionID, domain.EventQuestionResolved, runID, domain.QuestionResolvedData{
		RequestID: reqID,
		Answers:   answers,
	})
	result.Output = formatAnswers(in.Questions, answers)
	finish()
}

func (a *Agent) AnswerQuestion(sessionID, requestID string, answers []domain.QuestionAnswer) error {
	a.qMu.Lock()
	pending, ok := a.questions[requestID]
	a.qMu.Unlock()
	if !ok || pending.sessionID != sessionID {
		return errors.New("question request is no longer pending")
	}
	select {
	case pending.ch <- answers:
		return nil
	default:
		return errors.New("question request already answered")
	}
}

func formatAnswers(questions []domain.Question, answers []domain.QuestionAnswer) string {
	byID := make(map[string]domain.QuestionAnswer, len(answers))
	for _, answer := range answers {
		byID[answer.QuestionID] = answer
	}
	var b strings.Builder
	b.WriteString("User answers:")
	for _, question := range questions {
		label := strings.TrimSpace(question.Question)
		if label == "" {
			label = question.ID
		}
		var parts []string
		if answer, ok := byID[question.ID]; ok {
			for _, selected := range answer.Selected {
				if strings.TrimSpace(selected) != "" {
					parts = append(parts, selected)
				}
			}
			if custom := strings.TrimSpace(answer.Custom); custom != "" {
				parts = append(parts, "custom: "+custom)
			}
		}
		if len(parts) == 0 {
			parts = []string{"(no answer)"}
		}
		fmt.Fprintf(&b, "\n- %s: %s", label, strings.Join(parts, "; "))
	}
	return b.String()
}

func askUserTool() provider.Tool {
	return provider.Tool{
		Name:        "ask_user",
		Description: "Ask the user one or more questions when a decision, preference or missing detail blocks progress. Offer concrete options and let the user pick or type their own answer.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type":     "array",
					"minItems": 1,
					"maxItems": 4,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":          map[string]any{"type": "string", "description": "Stable identifier used in the answers."},
							"header":      map[string]any{"type": "string", "description": "Very short label, at most 12 characters."},
							"question":    map[string]any{"type": "string", "description": "The question to ask."},
							"multiSelect": map[string]any{"type": "boolean", "description": "Allow selecting more than one option."},
							"options": map[string]any{
								"type":     "array",
								"minItems": 2,
								"maxItems": 4,
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"label":       map[string]any{"type": "string"},
										"description": map[string]any{"type": "string"},
									},
									"required": []string{"label"},
								},
							},
						},
						"required": []string{"question", "options"},
					},
				},
			},
			"required": []string{"questions"},
		},
	}
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

type contextMessage struct {
	seq int64
	msg provider.Message
}

func (a *Agent) history(sessionID string) ([]provider.Message, string, error) {
	events, err := a.store.EventsAfter(sessionID, 0)
	if err != nil {
		return nil, "", err
	}
	boundary, summary := compactedBoundary(events)
	filtered := make([]domain.Event, 0, len(events))
	for _, ev := range events {
		if ev.Seq > boundary {
			filtered = append(filtered, ev)
		}
	}
	context := buildContext(filtered)
	messages := make([]provider.Message, 0, len(context))
	for _, item := range context {
		messages = append(messages, item.msg)
	}
	return messages, summary, nil
}

func buildContext(events []domain.Event) []contextMessage {
	var out []contextMessage
	for _, ev := range events {
		switch ev.Type {
		case domain.EventUserMessage:
			var data domain.UserMessageData
			if decode(ev.Data, &data) != nil || data.Text == "" {
				continue
			}
			out = append(out, contextMessage{
				seq: ev.Seq,
				msg: provider.Message{
					Role:    "user",
					Content: []provider.Block{{Type: provider.BlockText, Text: data.Text}},
				},
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
			out = append(out, contextMessage{seq: ev.Seq, msg: provider.Message{Role: "assistant", Content: blocks}})
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
			if n := len(out); n > 0 && out[n-1].msg.Role == "user" && isToolResultMessage(out[n-1].msg) {
				out[n-1].msg.Content = append(out[n-1].msg.Content, block)
				out[n-1].seq = ev.Seq
			} else {
				out = append(out, contextMessage{
					seq: ev.Seq,
					msg: provider.Message{
						Role:    "user",
						Content: []provider.Block{block},
					},
				})
			}
		}
	}
	return out
}

func compactedBoundary(events []domain.Event) (int64, string) {
	boundary := int64(0)
	summary := ""
	for _, ev := range events {
		if ev.Type != domain.EventSessionCompacted {
			continue
		}
		var data domain.CompactedData
		if decode(ev.Data, &data) != nil || data.UpToSeq <= boundary {
			continue
		}
		boundary = data.UpToSeq
		summary = data.Summary
	}
	return boundary, summary
}

func isToolResultMessage(message provider.Message) bool {
	for _, block := range message.Content {
		if block.Type != provider.BlockToolResult {
			return false
		}
	}
	return len(message.Content) > 0
}

const (
	maxInstructionChars = 20_000
	maxRulesChars       = 30_000
)

func systemPrompt(ws domain.Workspace, dataDir string) string {
	var b strings.Builder
	b.WriteString("You are Weft, an agentic coding assistant running on the user's machine.\n")
	b.WriteString("You can inspect and modify the workspace with the provided tools. ")
	b.WriteString("Read a file before editing it, keep changes focused, and verify your work when possible. ")
	b.WriteString("For multi-step tasks, keep a short plan with todo_write and update it as you make progress. ")
	b.WriteString("Delegate self-contained exploration or sizeable side work to a subagent with task. ")
	b.WriteString("Use remember for lasting preferences or facts the user asks you to keep across sessions.\n\n")
	b.WriteString("Workspace folders:\n")
	for _, folder := range ws.Folders {
		fmt.Fprintf(&b, "- %s: %s\n", folder.Name, folder.Path)
	}
	if dataDir != "" {
		appendInstructions(&b, "User instructions from "+filepath.Join(dataDir, "AGENTS.md"), filepath.Join(dataDir, "AGENTS.md"), maxInstructionChars)
	}
	for _, folder := range ws.Folders {
		appendInstructions(&b, "Instructions from "+filepath.Join(folder.Path, "AGENTS.md"), filepath.Join(folder.Path, "AGENTS.md"), maxInstructionChars)
	}
	rulesBudget := maxRulesChars
	for _, folder := range ws.Folders {
		paths, _ := filepath.Glob(filepath.Join(folder.Path, ".weft", "rules", "*.md"))
		for _, path := range paths {
			if rulesBudget <= 0 {
				b.WriteString("\n(More project rules were omitted because the rules budget is full.)\n")
				return b.String()
			}
			used := appendInstructions(&b, "Rule from "+path, path, min(rulesBudget, maxInstructionChars))
			rulesBudget -= used
		}
	}
	return b.String()
}

func appendInstructions(b *strings.Builder, heading, path string, limit int) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return 0
	}
	if len(text) > limit {
		text = text[:limit] + "\n… truncated"
	}
	fmt.Fprintf(b, "\n# %s\n\n%s\n", heading, text)
	return len(text)
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

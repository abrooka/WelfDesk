package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weftdesk/weft/internal/config"
	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
	"github.com/weftdesk/weft/internal/store"
	"github.com/weftdesk/weft/internal/tools"
)

func TestLoopReadsFile(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "hello.txt"), []byte("hello weft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ws, err := st.CreateWorkspace("demo", []domain.Folder{{Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.CreateSession(ws.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults(t.TempDir())
	cfg.PermissionMode = "yolo"
	prov, err := provider.New(cfg.Provider)
	if err != nil {
		t.Fatal(err)
	}
	ag := New(st, cfg, prov)

	if err := ag.Send(context.Background(), sess.ID, "/read hello.txt"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, sess.ID)

	events, err := st.EventsAfter(sess.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sawCall, sawResult, sawFinal bool
	for _, ev := range events {
		switch ev.Type {
		case domain.EventToolCall:
			sawCall = true
		case domain.EventToolResult:
			var data domain.ToolResultData
			if err := decode(ev.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.IsError || !strings.Contains(data.Output, "hello weft") {
				t.Fatalf("unexpected tool result: %+v", data)
			}
			sawResult = true
		case domain.EventAssistantMessage:
			var data domain.AssistantMessageData
			if err := decode(ev.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Text != "" && len(data.ToolCalls) == 0 {
				sawFinal = true
			}
		}
	}
	if !sawCall || !sawResult || !sawFinal {
		t.Fatalf("incomplete loop: call=%v result=%v final=%v", sawCall, sawResult, sawFinal)
	}

	if err := ag.Send(context.Background(), sess.ID, "thanks"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, sess.ID)
}

func TestPermissionDenyBlocksTool(t *testing.T) {
	ag, st, project, sessionID := permissionFixture(t, "ask")
	if err := ag.Send(context.Background(), sessionID, "please write"); err != nil {
		t.Fatal(err)
	}
	request := waitEvent(t, st, sessionID, domain.EventPermissionRequest)
	var ask domain.PermissionRequestData
	if err := decode(request.Data, &ask); err != nil {
		t.Fatal(err)
	}
	if ask.Tool != "bash" {
		t.Fatalf("unexpected permission tool: %s", ask.Tool)
	}
	if err := ag.ResolvePermission(sessionID, ask.RequestID, "deny"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, sessionID)

	if _, err := os.Stat(filepath.Join(project, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied command still wrote a file: %v", err)
	}
	result := waitEvent(t, st, sessionID, domain.EventToolResult)
	var data domain.ToolResultData
	if err := decode(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if !data.IsError || !strings.Contains(data.Output, "Permission denied") {
		t.Fatalf("unexpected denied result: %+v", data)
	}
	waitEvent(t, st, sessionID, domain.EventPermissionResolved)
}

func TestPermissionAllowRunsTool(t *testing.T) {
	ag, st, project, sessionID := permissionFixture(t, "ask")
	if err := ag.Send(context.Background(), sessionID, "please write"); err != nil {
		t.Fatal(err)
	}
	request := waitEvent(t, st, sessionID, domain.EventPermissionRequest)
	var ask domain.PermissionRequestData
	if err := decode(request.Data, &ask); err != nil {
		t.Fatal(err)
	}
	if err := ag.ResolvePermission(sessionID, ask.RequestID, "allow"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, sessionID)

	if _, err := os.Stat(filepath.Join(project, "approved.txt")); err != nil {
		t.Fatalf("approved command did not write a file: %v", err)
	}
}

func permissionFixture(t *testing.T, mode string) (*Agent, *store.Store, string, string) {
	t.Helper()
	project := t.TempDir()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ws, err := st.CreateWorkspace("demo", []domain.Folder{{Path: project}})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.CreateSession(ws.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults(t.TempDir())
	cfg.PermissionMode = mode
	return New(st, cfg, &toolProvider{}), st, project, sess.ID
}

type toolProvider struct{}

func (p *toolProvider) Name() string { return "fake" }

func (p *toolProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Delta, error) {
	hasResult := false
	for _, message := range req.Messages {
		for _, block := range message.Content {
			if block.Type == provider.BlockToolResult {
				hasResult = true
			}
		}
	}
	ch := make(chan provider.Delta, 4)
	go func() {
		defer close(ch)
		if hasResult {
			emit(ctx, ch, provider.Delta{Type: "text", Text: "done"})
		} else {
			emit(ctx, ch, provider.Delta{Type: "tool_call", ToolCall: &provider.ToolCall{
				ID:        "call-1",
				Name:      "bash",
				Arguments: `{"command":"echo approved > approved.txt"}`,
			}})
		}
		emit(ctx, ch, provider.Delta{Type: "done"})
	}()
	return ch, nil
}

func emit(ctx context.Context, ch chan<- provider.Delta, delta provider.Delta) {
	select {
	case ch <- delta:
	case <-ctx.Done():
	}
}

func TestAskUserReceivesAnswer(t *testing.T) {
	ag, st, _, sessionID := permissionFixture(t, "ask")
	ag.UpdateConfig(ag.Config(), &askProvider{})
	if err := ag.Send(context.Background(), sessionID, "decide"); err != nil {
		t.Fatal(err)
	}
	request := waitEvent(t, st, sessionID, domain.EventQuestionRequest)
	var ask domain.QuestionRequestData
	if err := decode(request.Data, &ask); err != nil {
		t.Fatal(err)
	}
	if len(ask.Questions) != 2 || ask.Questions[0].ID != "scope" {
		t.Fatalf("unexpected questions: %+v", ask.Questions)
	}
	answers := []domain.QuestionAnswer{
		{QuestionID: "scope", Selected: []string{"Desktop"}},
		{QuestionID: "extras", Custom: "One focused test"},
	}
	if err := ag.AnswerQuestion(sessionID, ask.RequestID, answers); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, sessionID)

	resolved := waitEvent(t, st, sessionID, domain.EventQuestionResolved)
	var resolution domain.QuestionResolvedData
	if err := decode(resolved.Data, &resolution); err != nil {
		t.Fatal(err)
	}
	if len(resolution.Answers) != 2 {
		t.Fatalf("unexpected resolved answers: %+v", resolution.Answers)
	}
	result := waitEvent(t, st, sessionID, domain.EventToolResult)
	var data domain.ToolResultData
	if err := decode(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.IsError || !strings.Contains(data.Output, "Desktop") || !strings.Contains(data.Output, "custom: One focused test") {
		t.Fatalf("unexpected ask_user result: %+v", data)
	}
}

func TestAnswerQuestionUnknownRequest(t *testing.T) {
	ag, _, _, sessionID := permissionFixture(t, "ask")
	if err := ag.AnswerQuestion(sessionID, "missing", nil); err == nil {
		t.Fatal("expected an error for an unknown question request")
	}
}

type askProvider struct{}

func (p *askProvider) Name() string { return "fake" }

func (p *askProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Delta, error) {
	hasResult := false
	for _, message := range req.Messages {
		for _, block := range message.Content {
			if block.Type == provider.BlockToolResult {
				hasResult = true
			}
		}
	}
	ch := make(chan provider.Delta, 2)
	go func() {
		defer close(ch)
		if hasResult {
			emit(ctx, ch, provider.Delta{Type: "text", Text: "thanks"})
		} else {
			emit(ctx, ch, provider.Delta{Type: "tool_call", ToolCall: &provider.ToolCall{
				ID:   "ask-1",
				Name: "ask_user",
				Arguments: `{"questions":[
					{"id":"scope","header":"Scope","question":"Which part?","options":[{"label":"Desktop"},{"label":"Server"}]},
					{"id":"extras","header":"Extras","question":"Anything else?","multiSelect":true,"options":[{"label":"Tests"},{"label":"Docs"}]}
				]}`,
			}})
		}
		emit(ctx, ch, provider.Delta{Type: "done"})
	}()
	return ch, nil
}

func TestTodoWriteStoresPlanAndInjectsPrompt(t *testing.T) {
	ag, st, _, sessionID := permissionFixture(t, "ask")
	ag.UpdateConfig(ag.Config(), &todoProvider{})
	if err := ag.Send(context.Background(), sessionID, "plan"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, sessionID)

	var plan domain.TodosData
	sawPlan, sawOutput := false, false
	events, err := st.EventsAfter(sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		switch ev.Type {
		case domain.EventTodosUpdated:
			if err := decode(ev.Data, &plan); err != nil {
				t.Fatal(err)
			}
			sawPlan = true
		case domain.EventToolResult:
			var data domain.ToolResultData
			if err := decode(ev.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.IsError || !strings.Contains(data.Output, "Plan updated: 1/2 done") || !strings.Contains(data.Output, "[~] Apply the fix") {
				t.Fatalf("unexpected todo_write result: %+v", data)
			}
			sawOutput = true
		}
	}
	if !sawPlan || !sawOutput {
		t.Fatalf("incomplete todo flow: plan=%v output=%v", sawPlan, sawOutput)
	}
	if len(plan.Todos) != 2 || plan.Todos[0].ID != "todo-1" || plan.Todos[1].ID != "edit" {
		t.Fatalf("unexpected stored plan: %+v", plan.Todos)
	}
	todos := ag.Todos(sessionID)
	if len(todos) != 2 || todos[1].Status != domain.TodoInProgress {
		t.Fatalf("unexpected current plan: %+v", todos)
	}

	if err := ag.Send(context.Background(), sessionID, "continue"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, sessionID)

	events, err = st.EventsAfter(sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	sawPrompt := false
	for _, ev := range events {
		if ev.Type != domain.EventRequestStarted {
			continue
		}
		var snapshot requestSnapshot
		if err := decode(ev.Data, &snapshot); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(snapshot.System, "# Current plan") && strings.Contains(snapshot.System, "[~] Apply the fix") {
			sawPrompt = true
		}
	}
	if !sawPrompt {
		t.Fatal("the plan was not injected into the system prompt")
	}
}

func TestSubagentReturnsReportAndKeepsHistoryIsolated(t *testing.T) {
	ag, st, project, sessionID := permissionFixture(t, "yolo")
	if err := os.WriteFile(filepath.Join(project, "marker.txt"), []byte("the marker is 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ag.UpdateConfig(ag.Config(), &subagentProvider{})
	if err := ag.Send(context.Background(), sessionID, "delegate"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, sessionID)

	events, err := st.EventsAfter(sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	toolCalls := 0
	var report domain.ToolResultData
	sawFinal := false
	for _, ev := range events {
		switch ev.Type {
		case domain.EventToolCall:
			toolCalls++
		case domain.EventToolResult:
			var data domain.ToolResultData
			if err := decode(ev.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Name == "task" {
				report = data
			}
		case domain.EventAssistantMessage:
			var data domain.AssistantMessageData
			if err := decode(ev.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Text == "done" && len(data.ToolCalls) == 0 {
				sawFinal = true
			}
		}
	}
	if toolCalls != 1 {
		t.Fatalf("subagent tool calls leaked into the session log: %d tool calls", toolCalls)
	}
	if report.IsError || !strings.Contains(report.Output, "Found the marker.") {
		t.Fatalf("unexpected task report: %+v", report)
	}
	if !sawFinal {
		t.Fatal("the parent agent did not finish after the subagent report")
	}
}

type subagentProvider struct{}

func (p *subagentProvider) Name() string { return "fake" }

func (p *subagentProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Delta, error) {
	results := ""
	for _, message := range req.Messages {
		for _, block := range message.Content {
			if block.Type == provider.BlockToolResult && block.ToolResult != nil {
				results += block.ToolResult.Content
			}
		}
	}
	ch := make(chan provider.Delta, 4)
	go func() {
		defer close(ch)
		if strings.Contains(req.System, "# Subagent") {
			if strings.Contains(results, "the marker is 42") {
				emit(ctx, ch, provider.Delta{Type: "text", Text: "Found the marker."})
			} else {
				emit(ctx, ch, provider.Delta{Type: "tool_call", ToolCall: &provider.ToolCall{
					ID:        "sub-1",
					Name:      "read_file",
					Arguments: `{"path":"marker.txt"}`,
				}})
			}
		} else if strings.Contains(results, "Found the marker.") {
			emit(ctx, ch, provider.Delta{Type: "text", Text: "done"})
		} else {
			emit(ctx, ch, provider.Delta{Type: "tool_call", ToolCall: &provider.ToolCall{
				ID:        "task-1",
				Name:      "task",
				Arguments: `{"description":"Inspect marker","prompt":"Read marker.txt and report the value."}`,
			}})
		}
		emit(ctx, ch, provider.Delta{Type: "done"})
	}()
	return ch, nil
}

func TestNormalizeTodosValidatesItems(t *testing.T) {
	cases := []struct {
		name  string
		items []domain.TodoItem
	}{
		{
			name: "two in progress",
			items: []domain.TodoItem{
				{Content: "a", Status: domain.TodoInProgress},
				{Content: "b", Status: domain.TodoInProgress},
			},
		},
		{name: "empty content", items: []domain.TodoItem{{Content: "  ", Status: domain.TodoPending}}},
		{name: "bad status", items: []domain.TodoItem{{Content: "a", Status: "doing"}}},
		{
			name: "duplicate ids",
			items: []domain.TodoItem{
				{ID: "same", Content: "a", Status: domain.TodoPending},
				{ID: "same", Content: "b", Status: domain.TodoPending},
			},
		},
	}
	for _, testCase := range cases {
		if _, err := normalizeTodos(testCase.items); err == nil {
			t.Fatalf("%s: expected an error", testCase.name)
		}
	}
	if _, err := normalizeTodos(nil); err != nil {
		t.Fatalf("empty plan should clear the list: %v", err)
	}
}

type todoProvider struct{}

func (p *todoProvider) Name() string { return "fake" }

func (p *todoProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Delta, error) {
	hasResult := false
	for _, message := range req.Messages {
		for _, block := range message.Content {
			if block.Type == provider.BlockToolResult {
				hasResult = true
			}
		}
	}
	ch := make(chan provider.Delta, 4)
	go func() {
		defer close(ch)
		if hasResult {
			emit(ctx, ch, provider.Delta{Type: "text", Text: "done"})
		} else {
			emit(ctx, ch, provider.Delta{Type: "tool_call", ToolCall: &provider.ToolCall{
				ID:   "plan-1",
				Name: "todo_write",
				Arguments: `{"todos":[
					{"content":"Read the code","status":"completed"},
					{"id":"edit","content":"Apply the fix","status":"in_progress"}]}`,
			}})
		}
		emit(ctx, ch, provider.Delta{Type: "done"})
	}()
	return ch, nil
}

func TestCheckpointRevertRestoresFile(t *testing.T) {
	ag, st, project, sessionID := permissionFixture(t, "yolo")
	path := filepath.Join(project, "notes.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("after\n")
	if err := ag.saveCheckpoint(sessionID, "run-1", provider.ToolCall{ID: "call-1", Name: "edit_file"}, tools.Result{
		Path:     path,
		Diff:     "diff",
		Snapshot: &tools.Snapshot{Existed: true, Before: "before\n", AfterHash: hashBytes([]byte("after\n"))},
	}); err != nil {
		t.Fatal(err)
	}
	list, err := ag.Checkpoints(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Path != path {
		t.Fatalf("unexpected checkpoints: %+v", list)
	}

	if _, err := ag.RevertCheckpoint(sessionID, list[0].ID, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before\n" {
		t.Fatalf("revert restored %q", data)
	}
	waitEvent(t, st, sessionID, domain.EventCheckpointReverted)

	list, err = ag.Checkpoints(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].RevertedAt == nil {
		t.Fatal("checkpoint was not marked reverted")
	}
	if _, err := ag.RevertCheckpoint(sessionID, list[0].ID, false); err == nil {
		t.Fatal("expected an error when reverting twice")
	}
}

func TestCheckpointRevertNeedsForceAfterExternalEdit(t *testing.T) {
	ag, _, project, sessionID := permissionFixture(t, "yolo")
	path := filepath.Join(project, "config.txt")
	if err := os.WriteFile(path, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ag.saveCheckpoint(sessionID, "run-1", provider.ToolCall{ID: "call-1", Name: "write_file"}, tools.Result{
		Path:     path,
		Snapshot: &tools.Snapshot{Existed: true, Before: "v1\n", AfterHash: hashBytes([]byte("v2\n"))},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("manual\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := ag.Checkpoints(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ag.RevertCheckpoint(sessionID, list[0].ID, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if _, err := ag.RevertCheckpoint(sessionID, list[0].ID, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v1\n" {
		t.Fatalf("forced revert restored %q", data)
	}
}

func TestRevertRunRestoresEarliestStateAndDeletesCreatedFiles(t *testing.T) {
	ag, _, project, sessionID := permissionFixture(t, "yolo")
	edited := filepath.Join(project, "edited.txt")
	created := filepath.Join(project, "created.txt")
	if err := os.WriteFile(edited, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot := func(path, before, after string, existed bool) {
		t.Helper()
		if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ag.saveCheckpoint(sessionID, "run-1", provider.ToolCall{ID: newID(), Name: "edit_file"}, tools.Result{
			Path:     path,
			Snapshot: &tools.Snapshot{Existed: existed, Before: before, AfterHash: hashBytes([]byte(after))},
		}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot(edited, "a\n", "b\n", true)
	snapshot(edited, "b\n", "c\n", true)
	snapshot(created, "", "new\n", false)

	if _, err := ag.RevertRun(sessionID, "run-1", false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\n" {
		t.Fatalf("run revert restored %q", data)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created file still exists: %v", err)
	}
	list, err := ag.Checkpoints(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, cp := range list {
		if cp.RevertedAt == nil {
			t.Fatalf("checkpoint %s was not marked reverted", cp.Path)
		}
	}
}

func waitEvent(t *testing.T, st *store.Store, sessionID, eventType string) domain.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events, err := st.EventsAfter(sessionID, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range events {
			if ev.Type == eventType {
				return ev
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("event %s was not recorded", eventType)
	return domain.Event{}
}

func waitIdle(t *testing.T, st *store.Store, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sess, err := st.Session(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if sess.Status != domain.SessionRunning {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("session did not become idle")
}

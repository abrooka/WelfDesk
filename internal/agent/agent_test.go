package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weftdesk/weft/internal/config"
	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
	"github.com/weftdesk/weft/internal/store"
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

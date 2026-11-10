package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
)

const maxTodos = 50

func todoTool() provider.Tool {
	return provider.Tool{
		Name:        "todo_write",
		Description: "Replace the session plan with an updated checklist. Use it to track multi-step work: keep at most one item in_progress while you work on it, and mark items completed as soon as they are done.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "The full plan. Replaces any previous plan; an empty list clears it.",
					"maxItems":    maxTodos,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":      map[string]any{"type": "string", "description": "Stable identifier. Omit to generate one."},
							"content": map[string]any{"type": "string", "description": "One short, concrete step."},
							"status":  map[string]any{"type": "string", "enum": []string{domain.TodoPending, domain.TodoInProgress, domain.TodoCompleted}},
						},
						"required": []string{"content", "status"},
					},
				},
			},
			"required": []string{"todos"},
		},
	}
}

func (a *Agent) updateTodos(sessionID, runID, args string, result *domain.ToolResultData) {
	var in struct {
		Todos []domain.TodoItem `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		result.IsError = true
		result.Output = "todo_write arguments are invalid: " + err.Error()
		return
	}
	todos, err := normalizeTodos(in.Todos)
	if err != nil {
		result.IsError = true
		result.Output = err.Error()
		return
	}
	if _, err := a.store.Append(sessionID, domain.EventTodosUpdated, runID, domain.TodosData{Todos: todos}); err != nil {
		result.IsError = true
		result.Output = "todo_write could not save the plan: " + err.Error()
		return
	}
	result.Output = formatTodos(todos)
}

func normalizeTodos(items []domain.TodoItem) ([]domain.TodoItem, error) {
	if len(items) > maxTodos {
		return nil, fmt.Errorf("todo_write accepts at most %d items", maxTodos)
	}
	out := make([]domain.TodoItem, 0, len(items))
	seen := make(map[string]bool, len(items))
	inProgress := 0
	for i, item := range items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			return nil, fmt.Errorf("todo item %d needs content", i+1)
		}
		status := strings.TrimSpace(item.Status)
		switch status {
		case domain.TodoPending, domain.TodoInProgress, domain.TodoCompleted:
		default:
			return nil, fmt.Errorf("todo item %q has an invalid status %q", content, item.Status)
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = fmt.Sprintf("todo-%d", i+1)
		}
		if seen[id] {
			return nil, fmt.Errorf("todo id %q is used more than once", id)
		}
		seen[id] = true
		if status == domain.TodoInProgress {
			inProgress++
		}
		out = append(out, domain.TodoItem{ID: id, Content: content, Status: status})
	}
	if inProgress > 1 {
		return nil, fmt.Errorf("keep at most one todo item in_progress")
	}
	return out, nil
}

func formatTodos(todos []domain.TodoItem) string {
	if len(todos) == 0 {
		return "Plan cleared."
	}
	done := 0
	for _, todo := range todos {
		if todo.Status == domain.TodoCompleted {
			done++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Plan updated: %d/%d done", done, len(todos))
	for _, todo := range todos {
		fmt.Fprintf(&b, "\n%s %s", todoMark(todo.Status), todo.Content)
	}
	return b.String()
}

func todoMark(status string) string {
	switch status {
	case domain.TodoCompleted:
		return "[x]"
	case domain.TodoInProgress:
		return "[~]"
	default:
		return "[ ]"
	}
}

func (a *Agent) Todos(sessionID string) []domain.TodoItem {
	events, err := a.store.EventsAfter(sessionID, 0)
	if err != nil {
		return nil
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != domain.EventTodosUpdated {
			continue
		}
		var data domain.TodosData
		if decode(events[i].Data, &data) != nil {
			continue
		}
		return data.Todos
	}
	return nil
}

func planPrompt(todos []domain.TodoItem) string {
	if len(todos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n# Current plan\n\n")
	b.WriteString("Keep this plan current with todo_write: mark the step you are working on in_progress and finished steps completed.\n")
	for _, todo := range todos {
		fmt.Fprintf(&b, "- %s %s\n", todoMark(todo.Status), todo.Content)
	}
	return b.String()
}

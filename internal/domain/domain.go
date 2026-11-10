package domain

import "time"

type RunnerKind string

const (
	RunnerLocal RunnerKind = "local"
	RunnerSSH   RunnerKind = "ssh"
	RunnerCloud RunnerKind = "cloud"
)

type Folder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Folders   []Folder  `json:"folders"`
	CreatedAt time.Time `json:"createdAt"`
}

type Session struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	LastSeq     int64     `json:"lastSeq"`
}

const (
	SessionIdle    = "idle"
	SessionRunning = "running"
	SessionError   = "error"
)

type Runner struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Kind     RunnerKind `json:"kind"`
	Platform string     `json:"platform"`
	Labels   []string   `json:"labels,omitempty"`
	Online   bool       `json:"online"`
	LastSeen time.Time  `json:"lastSeen"`
}

type Event struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Seq       int64     `json:"seq"`
	Type      string    `json:"type"`
	RunID     string    `json:"runId,omitempty"`
	At        time.Time `json:"at"`
	Data      any       `json:"data,omitempty"`
}

const (
	EventSessionCreated     = "session.created"
	EventUserMessage        = "user.message"
	EventTurnStarted        = "turn.started"
	EventTurnCompleted      = "turn.completed"
	EventAssistantDelta     = "assistant.delta"
	EventAssistantMessage   = "assistant.message"
	EventToolCall           = "tool.call"
	EventToolResult         = "tool.result"
	EventPermissionRequest  = "permission.request"
	EventPermissionResolved = "permission.resolved"
	EventRequestStarted     = "request.started"
	EventRequestCompleted   = "request.completed"
	EventError              = "error"
)

type UserMessageData struct {
	Text string `json:"text"`
}

type ToolCallData struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type AssistantMessageData struct {
	Text      string         `json:"text"`
	ToolCalls []ToolCallData `json:"toolCalls,omitempty"`
}

type ToolResultData struct {
	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name"`
	Output     string `json:"output"`
	Diff       string `json:"diff,omitempty"`
	Path       string `json:"path,omitempty"`
	IsError    bool   `json:"isError"`
	DurationMs int64  `json:"durationMs"`
}

type PermissionRequestData struct {
	RequestID string `json:"requestId"`
	Tool      string `json:"tool"`
	Input     string `json:"input"`
	Reason    string `json:"reason"`
}

type PermissionResolvedData struct {
	RequestID string `json:"requestId"`
	Decision  string `json:"decision"`
}

type ErrorData struct {
	Message string `json:"message"`
}

type TurnData struct {
	RunID string `json:"runId"`
}

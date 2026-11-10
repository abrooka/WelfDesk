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
	EventQuestionRequest    = "question.request"
	EventQuestionResolved   = "question.resolved"
	EventRequestStarted     = "request.started"
	EventRequestCompleted   = "request.completed"
	EventCheckpointReverted = "checkpoint.reverted"
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

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	ID          string           `json:"id"`
	Header      string           `json:"header,omitempty"`
	Question    string           `json:"question"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
	Options     []QuestionOption `json:"options"`
}

type QuestionRequestData struct {
	RequestID  string     `json:"requestId"`
	ToolCallID string     `json:"toolCallId"`
	Questions  []Question `json:"questions"`
}

type QuestionAnswer struct {
	QuestionID string   `json:"questionId"`
	Selected   []string `json:"selected,omitempty"`
	Custom     string   `json:"custom,omitempty"`
}

type QuestionResolvedData struct {
	RequestID string           `json:"requestId"`
	Answers   []QuestionAnswer `json:"answers,omitempty"`
	Reason    string           `json:"reason,omitempty"`
}

type ErrorData struct {
	Message string `json:"message"`
}

type TurnData struct {
	RunID string `json:"runId"`
}

// Checkpoint is the pre-edit state of a file written by a mutating tool.
type Checkpoint struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"sessionId"`
	RunID      string     `json:"runId"`
	ToolCallID string     `json:"toolCallId"`
	Tool       string     `json:"tool"`
	Path       string     `json:"path"`
	Existed    bool       `json:"existed"`
	Before     string     `json:"before,omitempty"`
	AfterHash  string     `json:"afterHash"`
	Diff       string     `json:"diff,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	RevertedAt *time.Time `json:"revertedAt,omitempty"`
}

type CheckpointRevertedData struct {
	Scope         string   `json:"scope"`
	RunID         string   `json:"runId,omitempty"`
	CheckpointIDs []string `json:"checkpointIds"`
	Paths         []string `json:"paths"`
}

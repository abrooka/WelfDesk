package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
)

const (
	maxMemoryEntryChars = 400
	maxMemoryFileChars  = 32_000
	maxMemoryPrompt     = 8_000
)

func rememberTool() provider.Tool {
	return provider.Tool{
		Name:        "remember",
		Description: "Save a durable fact about the user, their preferences, or this project to long-term memory that is available in future sessions. Use it when the user asks you to remember something or states a lasting preference. Do not store secrets, credentials, or one-off task details.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "One concise sentence stating what to remember."},
			},
			"required": []string{"text"},
		},
	}
}

func (a *Agent) remember(args string, result *domain.ToolResultData) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		result.IsError = true
		result.Output = "remember arguments are invalid: " + err.Error()
		return
	}
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" {
		result.IsError = true
		result.Output = "remember requires non-empty text"
		return
	}
	if runes := []rune(text); len(runes) > maxMemoryEntryChars {
		text = string(runes[:maxMemoryEntryChars]) + "…"
	}

	a.memMu.Lock()
	defer a.memMu.Unlock()

	path := a.memoryPath()
	entries := readMemoryEntries(path)
	for _, entry := range entries {
		if strings.EqualFold(entry, text) {
			result.Output = "Already remembered: " + text
			return
		}
	}
	entries = append(entries, text)
	total := 0
	for _, entry := range entries {
		total += len(entry) + 3
	}
	for total > maxMemoryFileChars && len(entries) > 1 {
		total -= len(entries[0]) + 3
		entries = entries[1:]
	}
	if err := writeMemoryEntries(path, entries); err != nil {
		result.IsError = true
		result.Output = "remember could not save the note: " + err.Error()
		return
	}
	result.Output = "Remembered: " + text
}

func (a *Agent) memoryPath() string {
	dataDir := a.Config().DataDir
	if dataDir == "" {
		dataDir = "."
	}
	return filepath.Join(dataDir, "memory.md")
}

func (a *Agent) memoryText() string {
	a.memMu.Lock()
	defer a.memMu.Unlock()
	entries := readMemoryEntries(a.memoryPath())
	budget := maxMemoryPrompt
	start := len(entries)
	for start > 0 {
		size := len(entries[start-1]) + 3
		if size > budget {
			break
		}
		budget -= size
		start--
	}
	if start == len(entries) {
		return ""
	}
	var b strings.Builder
	for _, entry := range entries[start:] {
		b.WriteString("- " + entry + "\n")
	}
	return b.String()
}

func readMemoryEntries(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if entry := strings.TrimSpace(line[2:]); entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func writeMemoryEntries(path string, entries []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Memory\n\n")
	for _, entry := range entries {
		b.WriteString("- " + entry + "\n")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func memoryPrompt(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return "\n# Memory\n\nDurable notes saved in earlier sessions. They may be stale: verify a note against the current workspace before relying on it.\n\n" + text
}

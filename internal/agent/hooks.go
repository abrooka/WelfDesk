package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/weftdesk/weft/internal/domain"
)

const (
	hookTimeout   = 60 * time.Second
	maxHookOutput = 4_000
)

var hookEventNames = map[string]bool{
	"run_start":     true,
	"pre_tool_use":  true,
	"post_tool_use": true,
	"run_end":       true,
}

type hookEntry struct {
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
}

type hookSet struct {
	entries map[string][]hookEntry
	cwd     string
}

func loadHooks(ws domain.Workspace, dataDir string) hookSet {
	set := hookSet{entries: map[string][]hookEntry{}}
	if len(ws.Folders) > 0 {
		set.cwd = ws.Folders[0].Path
	} else {
		set.cwd = dataDir
	}
	if dataDir == "" {
		return set
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "hooks.json"))
	if err != nil {
		return set
	}
	var cfg struct {
		Hooks map[string][]hookEntry `json:"hooks"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return set
	}
	for name, entries := range cfg.Hooks {
		event := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(name, "-", "_")))
		if !hookEventNames[event] {
			continue
		}
		for _, entry := range entries {
			entry.Command = strings.TrimSpace(entry.Command)
			entry.Matcher = strings.TrimSpace(entry.Matcher)
			if entry.Command == "" {
				continue
			}
			set.entries[event] = append(set.entries[event], entry)
		}
	}
	return set
}

func (h hookSet) empty() bool {
	return len(h.entries) == 0
}

func (a *Agent) runHooks(ctx context.Context, hooks hookSet, event, sessionID, runID, tool, arguments string) (string, bool) {
	entries := hooks.entries[event]
	if len(entries) == 0 {
		return "", false
	}
	payload, _ := json.Marshal(map[string]string{
		"event":     event,
		"sessionId": sessionID,
		"runId":     runID,
		"tool":      tool,
		"arguments": arguments,
	})
	var parts []string
	blocked := false
	for index, entry := range entries {
		if entry.Matcher != "" {
			matched, err := regexp.MatchString(entry.Matcher, tool)
			if err != nil || !matched {
				continue
			}
		}
		label := "hook #" + strconv.Itoa(index+1)
		output, code, err := runHookCommand(ctx, hooks.cwd, entry.Command, event, tool, payload)
		switch {
		case err != nil:
			parts = append(parts, label+": "+err.Error())
		case code == 2 && event == "pre_tool_use":
			blocked = true
			if output == "" {
				output = label + " exited with code 2"
			}
			parts = append(parts, output)
		case code != 0:
			if output == "" {
				output = label + " exited with code " + strconv.Itoa(code)
			}
			parts = append(parts, output)
		case output != "":
			parts = append(parts, output)
		}
	}
	return strings.Join(parts, "\n"), blocked
}

func appendNote(output, note string) string {
	if output == "" {
		return note
	}
	return output + "\n\n" + note
}

func runHookCommand(ctx context.Context, cwd, command, event, tool string, payload []byte) (string, int, error) {
	runCtx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	cmd := hookShellCommand(runCtx, command)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(os.Environ(), "WEFT_EVENT="+event, "WEFT_TOOL="+tool)
	cmd.Stdin = bytes.NewReader(payload)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			return out.String(), -1, err
		}
	}
	text := strings.TrimSpace(out.String())
	if runes := []rune(text); len(runes) > maxHookOutput {
		text = string(runes[:maxHookOutput]) + "…"
	}
	return text, code, nil
}

func hookShellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		if comspec := os.Getenv("COMSPEC"); comspec != "" {
			return exec.CommandContext(ctx, comspec, "/d", "/s", "/c", command)
		}
		return exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return exec.CommandContext(ctx, shell, "-lc", command)
}

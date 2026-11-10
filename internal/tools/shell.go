package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	defaultCommandTimeout = 120 * time.Second
	maxCommandTimeout     = 10 * time.Minute
	maxCommandOutput      = 100 * 1024
)

type runCommand struct{ base *base }

func (t *runCommand) Name() string { return "bash" }
func (t *runCommand) Description() string {
	return "Run a shell command in the workspace. Use it for builds, tests, git, and inspection. Returns combined stdout/stderr and the exit code."
}
func (t *runCommand) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":    map[string]any{"type": "string", "description": "Shell command to run."},
			"cwd":        map[string]any{"type": "string", "description": "Optional working directory inside the workspace."},
			"timeout_ms": map[string]any{"type": "integer", "description": "Timeout in milliseconds (default 120000)."},
		},
		"required": []string{"command"},
	}
}

func (t *runCommand) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var in struct {
		Command   string `json:"command"`
		Cwd       string `json:"cwd"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{IsError: true}, err
	}
	if strings.TrimSpace(in.Command) == "" {
		return Result{IsError: true}, fmt.Errorf("command is required")
	}
	cwd, err := t.base.defaultRoot()
	if in.Cwd != "" {
		cwd, err = t.base.resolve(in.Cwd)
	}
	if err != nil {
		return Result{IsError: true, Path: in.Cwd}, err
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return Result{IsError: true, Path: cwd}, fmt.Errorf("working directory does not exist: %s", cwd)
	}
	timeout := defaultCommandTimeout
	if in.TimeoutMS > 0 {
		timeout = time.Duration(in.TimeoutMS) * time.Millisecond
		if timeout > maxCommandTimeout {
			timeout = maxCommandTimeout
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shellCommand(runCtx, in.Command)
	cmd.Dir = cwd
	start := time.Now()
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)
	text := string(out)
	if len(text) > maxCommandOutput {
		text = text[:maxCommandOutput] + fmt.Sprintf("\n[truncated at %d bytes]", maxCommandOutput)
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if runCtx.Err() == context.DeadlineExceeded {
			return Result{
				Output:  fmt.Sprintf("%s\n[command timed out after %s]", text, timeout),
				Path:    cwd,
				IsError: true,
				Meta:    map[string]any{"durationMs": duration.Milliseconds(), "timeout": true},
			}, nil
		} else {
			return Result{IsError: true, Path: cwd, Output: text}, err
		}
	}
	if text == "" {
		text = "(no output)"
	}
	return Result{
		Output:  fmt.Sprintf("%s\n[exit code %d, %dms]", text, exitCode, duration.Milliseconds()),
		Path:    cwd,
		IsError: exitCode != 0,
		Meta:    map[string]any{"exitCode": exitCode, "durationMs": duration.Milliseconds()},
	}, nil
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
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

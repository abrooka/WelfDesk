package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWriteEdit(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry([]string{dir})
	ctx := context.Background()

	res, err := r.Execute(ctx, "write_file", `{"path":"notes.txt","content":"alpha\nbeta\n"}`)
	if err != nil || res.IsError {
		t.Fatalf("write_file: %v %+v", err, res)
	}

	res, err = r.Execute(ctx, "read_file", `{"path":"notes.txt"}`)
	if err != nil || res.IsError {
		t.Fatalf("read_file: %v %+v", err, res)
	}
	if !strings.Contains(res.Output, "alpha") || !strings.Contains(res.Output, "beta") {
		t.Fatalf("unexpected read output: %q", res.Output)
	}

	res, err = r.Execute(ctx, "edit_file", `{"path":"notes.txt","old_string":"beta","new_string":"gamma"}`)
	if err != nil || res.IsError {
		t.Fatalf("edit_file: %v %+v", err, res)
	}
	if !strings.Contains(res.Diff, "-beta") || !strings.Contains(res.Diff, "+gamma") {
		t.Fatalf("unexpected diff: %q", res.Diff)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "alpha\ngamma\n" {
		t.Fatalf("unexpected content: %q", raw)
	}
}

func TestEditAmbiguous(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry([]string{dir})
	res, err := r.Execute(context.Background(), "edit_file", `{"path":"a.txt","old_string":"x","new_string":"y"}`)
	if err == nil || !res.IsError || !strings.Contains(err.Error(), "2") {
		t.Fatalf("expected ambiguity error, got %+v %v", res, err)
	}
}

func TestPathEscapeRejected(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry([]string{dir})
	res, err := r.Execute(context.Background(), "read_file", `{"path":"../../etc/passwd"}`)
	if err == nil || !res.IsError {
		t.Fatalf("expected path escape rejection, got %+v %v", res, err)
	}
}

func TestGlobAndGrep(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "main.go"), []byte("package main\nfunc hello() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "skip.txt"), []byte("hello there\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry([]string{dir})

	res, err := r.Execute(context.Background(), "glob", `{"pattern":"**/*.go"}`)
	if err != nil || res.IsError || !strings.Contains(res.Output, "main.go") {
		t.Fatalf("glob: %v %+v", err, res)
	}
	res, err = r.Execute(context.Background(), "grep", `{"pattern":"hello"}`)
	if err != nil || res.IsError || !strings.Contains(res.Output, "main.go") || !strings.Contains(res.Output, "skip.txt") {
		t.Fatalf("grep: %v %+v", err, res)
	}
}

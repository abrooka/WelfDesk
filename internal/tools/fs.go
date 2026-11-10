package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxReadLines   = 5000
	defaultRead    = 2000
	maxFileBytes   = 4 * 1024 * 1024
	maxGrepMatches = 300
	maxGlobResults = 300
)

type readFile struct{ base *base }

func (t *readFile) Name() string { return "read_file" }
func (t *readFile) Description() string {
	return "Read a text file from the workspace. Returns line-numbered content. Always read a file before editing it."
}
func (t *readFile) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Absolute path or path relative to the first workspace folder."},
			"offset": map[string]any{"type": "integer", "description": "1-based first line to read."},
			"limit":  map[string]any{"type": "integer", "description": "Maximum lines to read (default 2000, max 5000)."},
		},
		"required": []string{"path"},
	}
}

func (t *readFile) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var in struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{IsError: true}, err
	}
	path, err := t.base.resolve(in.Path)
	if err != nil {
		return Result{IsError: true, Path: in.Path}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Result{IsError: true, Path: path}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Result{IsError: true, Path: path}, err
	}
	if info.IsDir() {
		return Result{IsError: true, Path: path}, fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > maxFileBytes {
		return Result{IsError: true, Path: path}, fmt.Errorf("file is too large to read (%d bytes)", info.Size())
	}
	offset := in.Offset
	if offset <= 0 {
		offset = 1
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultRead
	}
	if limit > maxReadLines {
		limit = maxReadLines
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	line := 0
	var b strings.Builder
	written := 0
	truncated := false
	for sc.Scan() {
		line++
		if line < offset {
			continue
		}
		if written >= limit {
			truncated = true
			break
		}
		if len(sc.Bytes()) == 0 {
			fmt.Fprintf(&b, "%6d\t\n", line)
		} else {
			fmt.Fprintf(&b, "%6d\t%s\n", line, sc.Text())
		}
		written++
	}
	if err := sc.Err(); err != nil {
		return Result{IsError: true, Path: path}, err
	}
	if written == 0 {
		return Result{Output: fmt.Sprintf("%s has no content at line %d.", path, offset), Path: path}, nil
	}
	out := b.String()
	if truncated {
		out += fmt.Sprintf("\n[truncated after %d lines; file has at least %d lines]", written, line)
	}
	return Result{
		Output: out,
		Path:   path,
		Meta:   map[string]any{"lines": written, "bytes": info.Size()},
	}, nil
}

type writeFile struct{ base *base }

func (t *writeFile) Name() string { return "write_file" }
func (t *writeFile) Description() string {
	return "Create or overwrite a text file. Prefer edit_file for small changes to existing files."
}
func (t *writeFile) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Absolute path or path relative to the first workspace folder."},
			"content": map[string]any{"type": "string", "description": "Full file content."},
		},
		"required": []string{"path", "content"},
	}
}

func (t *writeFile) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{IsError: true}, err
	}
	path, err := t.base.resolve(in.Path)
	if err != nil {
		return Result{IsError: true, Path: in.Path}, err
	}
	var old string
	if data, err := os.ReadFile(path); err == nil {
		old = string(data)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{IsError: true, Path: path}, err
	}
	if err := os.WriteFile(path, []byte(in.Content), 0o644); err != nil {
		return Result{IsError: true, Path: path}, err
	}
	diff := unifiedDiff(path, old, in.Content)
	return Result{
		Output: fmt.Sprintf("Wrote %d bytes to %s", len(in.Content), path),
		Diff:   diff,
		Path:   path,
		Meta:   map[string]any{"bytes": len(in.Content)},
	}, nil
}

type editFile struct{ base *base }

func (t *editFile) Name() string { return "edit_file" }
func (t *editFile) Description() string {
	return "Replace an exact string in an existing file. Read the file first. If old_string appears more than once, provide more context or set replace_all."
}
func (t *editFile) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "Absolute path or path relative to the first workspace folder."},
			"old_string":  map[string]any{"type": "string", "description": "Exact text to replace, including indentation."},
			"new_string":  map[string]any{"type": "string", "description": "Replacement text."},
			"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence. Default false."},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (t *editFile) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var in struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{IsError: true}, err
	}
	if in.OldString == "" {
		return Result{IsError: true, Path: in.Path}, fmt.Errorf("old_string must not be empty")
	}
	path, err := t.base.resolve(in.Path)
	if err != nil {
		return Result{IsError: true, Path: in.Path}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{IsError: true, Path: path}, err
	}
	old := string(data)
	count := strings.Count(old, in.OldString)
	if count == 0 {
		return Result{IsError: true, Path: path}, fmt.Errorf("old_string was not found in %s; re-read the file and match the exact text", path)
	}
	if count > 1 && !in.ReplaceAll {
		return Result{IsError: true, Path: path}, fmt.Errorf("old_string appears %d times in %s; add more context or set replace_all", count, path)
	}
	var updated string
	if in.ReplaceAll {
		updated = strings.ReplaceAll(old, in.OldString, in.NewString)
	} else {
		updated = strings.Replace(old, in.OldString, in.NewString, 1)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return Result{IsError: true, Path: path}, err
	}
	diff := unifiedDiff(path, old, updated)
	return Result{
		Output: fmt.Sprintf("Replaced %d occurrence(s) in %s", count, path),
		Diff:   diff,
		Path:   path,
		Meta:   map[string]any{"replacements": count},
	}, nil
}

type globFiles struct{ base *base }

func (t *globFiles) Name() string { return "glob" }
func (t *globFiles) Description() string {
	return "Find files by glob pattern, for example **/*.go or src/**/*.tsx. Returns absolute paths."
}
func (t *globFiles) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{"type": "string", "description": "Glob pattern using * and **."},
			"path":    map[string]any{"type": "string", "description": "Optional directory to search."},
		},
		"required": []string{"pattern"},
	}
}

func (t *globFiles) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var in struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{IsError: true}, err
	}
	if in.Pattern == "" {
		return Result{IsError: true}, fmt.Errorf("pattern is required")
	}
	re, err := globToRegexp(in.Pattern)
	if err != nil {
		return Result{IsError: true}, err
	}
	matchBase := !strings.Contains(filepath.ToSlash(in.Pattern), "/")
	var starts []string
	if in.Path != "" {
		start, err := t.base.resolve(in.Path)
		if err != nil {
			return Result{IsError: true, Path: in.Path}, err
		}
		starts = []string{start}
	} else {
		starts = t.base.roots
	}
	var matches []string
	for _, start := range starts {
		_ = t.base.walk(start, func(path string, info os.FileInfo) error {
			if len(matches) >= maxGlobResults {
				return filepath.SkipAll
			}
			rel, err := filepath.Rel(start, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if re.MatchString(rel) || (matchBase && re.MatchString(filepath.Base(rel))) {
				matches = append(matches, path)
			}
			return nil
		})
		if len(matches) >= maxGlobResults {
			break
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return Result{Output: "No files matched " + in.Pattern, Meta: map[string]any{"count": 0}}, nil
	}
	out := strings.Join(matches, "\n")
	if len(matches) >= maxGlobResults {
		out += fmt.Sprintf("\n[truncated at %d results]", maxGlobResults)
	}
	return Result{Output: out, Meta: map[string]any{"count": len(matches)}}, nil
}

type grepFiles struct{ base *base }

func (t *grepFiles) Name() string { return "grep" }
func (t *grepFiles) Description() string {
	return "Search file contents with a Go regular expression. Skips binaries and common build/vendor directories."
}
func (t *grepFiles) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":     map[string]any{"type": "string", "description": "Go regular expression."},
			"path":        map[string]any{"type": "string", "description": "Optional directory to search."},
			"glob":        map[string]any{"type": "string", "description": "Optional file glob filter, for example **/*.go."},
			"max_results": map[string]any{"type": "integer", "description": "Maximum matches (default 300)."},
		},
		"required": []string{"pattern"},
	}
}

func (t *grepFiles) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var in struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{IsError: true}, err
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return Result{IsError: true}, err
	}
	limit := in.MaxResults
	if limit <= 0 || limit > maxGrepMatches {
		limit = maxGrepMatches
	}
	var fileRe *regexp.Regexp
	if in.Glob != "" {
		fileRe, err = globToRegexp(in.Glob)
		if err != nil {
			return Result{IsError: true}, err
		}
	}
	var starts []string
	if in.Path != "" {
		start, err := t.base.resolve(in.Path)
		if err != nil {
			return Result{IsError: true, Path: in.Path}, err
		}
		starts = []string{start}
	} else {
		starts = t.base.roots
	}
	var b strings.Builder
	matches := 0
	for _, start := range starts {
		_ = t.base.walk(start, func(path string, info os.FileInfo) error {
			if matches >= limit {
				return filepath.SkipAll
			}
			if fileRe != nil {
				rel, _ := filepath.Rel(start, path)
				rel = filepath.ToSlash(rel)
				if !fileRe.MatchString(rel) && !fileRe.MatchString(filepath.Base(rel)) {
					return nil
				}
			}
			data, err := os.ReadFile(path)
			if err != nil || len(data) > maxFileBytes || bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0 {
				return nil
			}
			sc := bufio.NewScanner(bytes.NewReader(data))
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			line := 0
			for sc.Scan() {
				line++
				text := sc.Text()
				if re.MatchString(text) {
					fmt.Fprintf(&b, "%s:%d: %s\n", path, line, text)
					matches++
					if matches >= limit {
						return filepath.SkipAll
					}
				}
			}
			return nil
		})
		if matches >= limit {
			break
		}
	}
	if matches == 0 {
		return Result{Output: "No matches.", Meta: map[string]any{"count": 0}}, nil
	}
	out := b.String()
	if matches >= limit {
		out += fmt.Sprintf("[truncated at %d matches]", limit)
	}
	return Result{Output: out, Meta: map[string]any{"count": matches}}, nil
}

func globToRegexp(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '[', ']', '{', '}', '^', '$', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

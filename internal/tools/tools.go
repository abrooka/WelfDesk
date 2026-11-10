package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/weftdesk/weft/internal/provider"
)

type Result struct {
	Output  string         `json:"output"`
	Diff    string         `json:"diff,omitempty"`
	Path    string         `json:"path,omitempty"`
	IsError bool           `json:"isError"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Run(ctx context.Context, input json.RawMessage) (Result, error)
}

type Registry struct {
	order []string
	tools map[string]Tool
}

func NewRegistry(roots []string) *Registry {
	b := &base{roots: normalizeRoots(roots)}
	r := &Registry{tools: map[string]Tool{}}
	for _, tool := range []Tool{
		&readFile{base: b},
		&writeFile{base: b},
		&editFile{base: b},
		&globFiles{base: b},
		&grepFiles{base: b},
		&runCommand{base: b},
	} {
		r.register(tool)
	}
	return r
}

func (r *Registry) register(tool Tool) {
	r.order = append(r.order, tool.Name())
	r.tools[tool.Name()] = tool
}

func (r *Registry) Definitions() []provider.Tool {
	out := make([]provider.Tool, 0, len(r.order))
	for _, name := range r.order {
		tool := r.tools[name]
		out = append(out, provider.Tool{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      tool.Schema(),
		})
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, name, args string) (result Result, err error) {
	tool, ok := r.tools[name]
	if !ok {
		return Result{IsError: true}, fmt.Errorf("unknown tool %q", name)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("tool %s panicked: %v", name, recovered)
		}
	}()
	raw := json.RawMessage(args)
	if strings.TrimSpace(args) == "" {
		raw = json.RawMessage("{}")
	}
	return tool.Run(ctx, raw)
}

func (r *Registry) Names() []string {
	out := append([]string(nil), r.order...)
	sort.Strings(out)
	return out
}

type base struct {
	roots []string
}

func normalizeRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		key := abs
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, abs)
	}
	return out
}

func (b *base) defaultRoot() (string, error) {
	if len(b.roots) == 0 {
		return "", fmt.Errorf("workspace has no folders")
	}
	return b.roots[0], nil
}

func (b *base) resolve(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		root, err := b.defaultRoot()
		if err != nil {
			return "", err
		}
		path = filepath.Join(root, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	for _, root := range b.roots {
		if within(root, abs) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("path is outside the workspace: %s", path)
}

func within(root, path string) bool {
	rootCmp, pathCmp := root, path
	if runtime.GOOS == "windows" {
		rootCmp = strings.ToLower(rootCmp)
		pathCmp = strings.ToLower(pathCmp)
	}
	rel, err := filepath.Rel(rootCmp, pathCmp)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var ignoredDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".svn":         true,
	".weft":        true,
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
	"out":          true,
	".next":        true,
	".nuxt":        true,
	".turbo":       true,
	".cache":       true,
	"coverage":     true,
	"__pycache__":  true,
	".venv":        true,
}

func (b *base) walk(start string, fn func(path string, info os.FileInfo) error) error {
	return filepath.Walk(start, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != start && ignoredDirs[strings.ToLower(info.Name())] {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return fn(path, info)
	})
}

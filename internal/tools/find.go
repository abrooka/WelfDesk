package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/weftdesk/weft/internal/index"
)

const (
	defaultFindLimit = 50
	maxFindLimit     = 200
)

type findFiles struct{ base *base }

func (t *findFiles) Name() string { return "find_files" }
func (t *findFiles) Description() string {
	return "Find workspace files by fuzzy name or path match, for example auth middleware or app.tsx. Prefer this over glob when you know part of a file name."
}
func (t *findFiles) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Part of a file name or path."},
			"limit": map[string]any{"type": "integer", "description": "Maximum matches (default 50, max 200)."},
		},
		"required": []string{"query"},
	}
}

func (t *findFiles) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{IsError: true}, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return Result{IsError: true}, fmt.Errorf("query is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultFindLimit
	}
	if limit > maxFindLimit {
		limit = maxFindLimit
	}
	hits := index.Match(index.Walk(t.base.roots), in.Query, limit)
	if len(hits) == 0 {
		return Result{Output: "No files matched " + in.Query + "."}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d matching files:\n", len(hits))
	for _, file := range hits {
		if len(t.base.roots) == 1 {
			b.WriteString(file.Rel)
		} else {
			b.WriteString(filepath.ToSlash(filepath.Join(filepath.Base(file.Root), file.Rel)))
		}
		b.WriteString("\n")
	}
	return Result{Output: b.String(), Meta: map[string]any{"matches": len(hits)}}, nil
}

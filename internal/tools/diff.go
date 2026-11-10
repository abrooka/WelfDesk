package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maxDiffLines = 500

func unifiedDiff(path, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	oldStart := prefix
	oldEnd := len(oldLines) - suffix
	newStart := prefix
	newEnd := len(newLines) - suffix

	var b strings.Builder
	name := filepath.Base(path)
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", name, name)
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart+1, oldEnd-oldStart, newStart+1, newEnd-newStart)
	written := 0
	for i := oldStart; i < oldEnd; i++ {
		if written >= maxDiffLines {
			b.WriteString("... diff truncated ...\n")
			return b.String()
		}
		b.WriteString("-")
		b.WriteString(oldLines[i])
		b.WriteString("\n")
		written++
	}
	for i := newStart; i < newEnd; i++ {
		if written >= maxDiffLines {
			b.WriteString("... diff truncated ...\n")
			return b.String()
		}
		b.WriteString("+")
		b.WriteString(newLines[i])
		b.WriteString("\n")
		written++
	}
	return b.String()
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

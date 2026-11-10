package index

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// File is a workspace file found by the index.
type File struct {
	Root string `json:"root"`
	Path string `json:"path"`
	Rel  string `json:"rel"`
}

const (
	maxFiles    = 20_000
	cacheTTL    = 3 * time.Second
	defaultHits = 50
)

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

// Ignored reports whether a directory is excluded from workspace traversal.
func Ignored(name string) bool {
	return ignoredDirs[strings.ToLower(name)]
}

// Walk lists regular files under the roots, at most maxFiles entries.
func Walk(roots []string) []File {
	var files []File
	for _, root := range roots {
		if len(files) >= maxFiles {
			break
		}
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if path != root && Ignored(info.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			files = append(files, File{Root: root, Path: path, Rel: filepath.ToSlash(rel)})
			if len(files) >= maxFiles {
				return filepath.SkipAll
			}
			return nil
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Root != files[j].Root {
			return files[i].Root < files[j].Root
		}
		return strings.ToLower(files[i].Rel) < strings.ToLower(files[j].Rel)
	})
	return files
}

// Match ranks files for a fuzzy query over their relative paths. An empty
// query returns the first limit files.
func Match(files []File, query string, limit int) []File {
	if limit <= 0 {
		limit = defaultHits
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		if len(files) > limit {
			return files[:limit]
		}
		return files
	}
	type hit struct {
		file  File
		score int
	}
	hits := make([]hit, 0, len(files))
	for _, file := range files {
		score, ok := scorePath(query, strings.ToLower(file.Rel))
		if !ok {
			continue
		}
		hits = append(hits, hit{file: file, score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if len(hits[i].file.Rel) != len(hits[j].file.Rel) {
			return len(hits[i].file.Rel) < len(hits[j].file.Rel)
		}
		return hits[i].file.Rel < hits[j].file.Rel
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]File, len(hits))
	for i, item := range hits {
		out[i] = item.file
	}
	return out
}

// scorePath rewards matches in the base name, consecutive characters, and
// early positions.
func scorePath(query, path string) (int, bool) {
	base := path
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		base = path[index+1:]
	}
	baseScore, inBase := subsequence(query, base)
	pathScore, inPath := subsequence(query, path)
	if !inBase && !inPath {
		return 0, false
	}
	score := pathScore
	if inBase {
		score += baseScore + 40
	}
	score -= len(path) / 4
	return score, true
}

func subsequence(query, text string) (int, bool) {
	q := []rune(query)
	t := []rune(text)
	score := 0
	previous := -1
	position := 0
	for _, char := range q {
		for position < len(t) && t[position] != char {
			position++
		}
		if position >= len(t) {
			return 0, false
		}
		if position == previous+1 {
			score += 6
		}
		if position == 0 || t[position-1] == '/' || t[position-1] == '.' || t[position-1] == '_' {
			score += 8
		}
		score += 10
		previous = position
		position++
	}
	return score, true
}

// Cache keeps a short-lived file list for repeated searches.
type Cache struct {
	mu    sync.Mutex
	key   string
	at    time.Time
	files []File
}

// Files returns the cached index for roots, rebuilding it when the roots
// change or the entry is older than cacheTTL.
func (c *Cache) Files(roots []string) []File {
	key := strings.Join(roots, "\n")
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key && time.Since(c.at) < cacheTTL {
		return c.files
	}
	files := Walk(roots)
	c.key, c.at, c.files = key, time.Now(), files
	return files
}

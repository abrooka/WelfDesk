package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWalkSkipsIgnoredAndMatchRanksBaseNames(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"main.go", "internal/agent/agent.go", "node_modules/pkg/index.js", "README.md"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := Walk([]string{root})
	for _, file := range files {
		if file.Rel == "node_modules/pkg/index.js" {
			t.Fatal("Walk included node_modules")
		}
	}
	if len(files) != 3 {
		t.Fatalf("Walk returned %d files, want 3: %+v", len(files), files)
	}

	hits := Match(files, "agent", 10)
	if len(hits) == 0 || hits[0].Rel != "internal/agent/agent.go" {
		t.Fatalf("unexpected matches: %+v", hits)
	}
	base := Match(files, "main", 10)
	if len(base) == 0 || base[0].Rel != "main.go" {
		t.Fatalf("base name did not rank first: %+v", base)
	}
	if len(Match(files, "zzz", 10)) != 0 {
		t.Fatal("Match returned hits for a missing query")
	}
	all := Match(files, "", 2)
	if len(all) != 2 {
		t.Fatalf("empty query returned %d files, want 2", len(all))
	}
}

func TestCacheRebuildsWhenRootsChange(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	var cache Cache
	if files := cache.Files([]string{first}); len(files) != 1 || files[0].Rel != "a.txt" {
		t.Fatalf("unexpected first index: %+v", files)
	}
	if files := cache.Files([]string{second}); len(files) != 1 || files[0].Rel != "b.txt" {
		t.Fatalf("cache did not follow roots: %+v", files)
	}
	if err := os.WriteFile(filepath.Join(second, "c.txt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	if files := cache.Files([]string{second}); len(files) != 1 {
		t.Fatalf("cache refreshed before TTL: %+v", files)
	}
	cache.at = time.Now().Add(-cacheTTL)
	if files := cache.Files([]string{second}); len(files) != 2 {
		t.Fatalf("cache did not refresh after TTL: %+v", files)
	}
}

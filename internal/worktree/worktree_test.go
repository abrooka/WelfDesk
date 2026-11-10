package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weftdesk/weft/internal/domain"
)

func TestDetectCreateApplyAndRemove(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.email", "weft@example.com")
	git(t, repo, "config", "user.name", "Weft")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "init")

	ws := domain.Workspace{Folders: []domain.Folder{{ID: "f1", Name: "repo", Path: repo}}}
	repos := Detect(ws)
	if len(repos) != 1 {
		t.Fatalf("Detect returned %d repos, want 1", len(repos))
	}
	if repos[0].FolderID != "f1" || repos[0].Branch != "main" {
		t.Fatalf("unexpected repo: %+v", repos[0])
	}

	target := filepath.Join(t.TempDir(), "checkout")
	if err := Create(repo, target, "weft/test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Fatalf("worktree checkout missing README: %v", err)
	}
	if err := Create(repo, filepath.Join(t.TempDir(), "other"), "weft/test"); err == nil {
		t.Fatal("Create accepted an existing branch")
	}

	effective := Apply(ws, &domain.Worktree{FolderID: "f1", Path: target})
	if effective.Folders[0].Path != target {
		t.Fatalf("Apply path = %s, want %s", effective.Folders[0].Path, target)
	}
	if ws.Folders[0].Path != repo {
		t.Fatal("Apply mutated the original workspace")
	}

	if err := Remove(repo, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after Remove: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/weftdesk/weft/internal/domain"
)

// Repo describes a git repository found inside a workspace folder.
type Repo struct {
	FolderID string `json:"folderId"`
	Root     string `json:"root"`
	Branch   string `json:"branch"`
}

const gitTimeout = 30 * time.Second

// Detect lists the git repositories behind the workspace folders.
func Detect(ws domain.Workspace) []Repo {
	seen := map[string]bool{}
	var repos []Repo
	for _, folder := range ws.Folders {
		root, err := run(folder.Path, "rev-parse", "--show-toplevel")
		if err != nil {
			continue
		}
		root = filepath.Clean(root)
		if seen[strings.ToLower(root)] {
			continue
		}
		seen[strings.ToLower(root)] = true
		branch, err := run(root, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			branch = ""
		}
		repos = append(repos, Repo{FolderID: folder.ID, Root: root, Branch: branch})
	}
	return repos
}

// Apply returns a workspace where the worktree folder points at the
// session checkout instead of the main repository.
func Apply(ws domain.Workspace, wt *domain.Worktree) domain.Workspace {
	if wt == nil {
		return ws
	}
	folders := make([]domain.Folder, len(ws.Folders))
	copy(folders, ws.Folders)
	for i := range folders {
		if folders[i].ID == wt.FolderID {
			folders[i].Path = wt.Path
		}
	}
	ws.Folders = folders
	return ws
}

// Create adds a worktree at dir on a new branch based on the repository HEAD.
func Create(repoRoot, dir, branch string) error {
	if branch == "" {
		return errors.New("worktree branch is required")
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("worktree path already exists: %s", dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if _, err := run(repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return fmt.Errorf("branch %s already exists", branch)
	}
	_, err := run(repoRoot, "worktree", "add", "-b", branch, dir, "HEAD")
	return err
}

// Remove deletes a worktree checkout created by Create.
func Remove(repoRoot, dir string) error {
	_, err := run(repoRoot, "worktree", "remove", "--force", dir)
	return err
}

func run(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return "", fmt.Errorf("git %s: %s", args[0], text)
		}
		return "", err
	}
	return text, nil
}

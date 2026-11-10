package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
	"github.com/weftdesk/weft/internal/store"
	"github.com/weftdesk/weft/internal/tools"
)

// ErrConflict means the file no longer matches the checkpoint, usually because
// it was edited outside Weft; callers may retry with force.
var ErrConflict = errors.New("checkpoint conflict")

type conflictError struct{ message string }

func (e *conflictError) Error() string { return e.message }
func (e *conflictError) Is(target error) bool {
	return target == ErrConflict
}

func (a *Agent) saveCheckpoint(sessionID, runID string, call provider.ToolCall, out tools.Result) error {
	if out.Snapshot == nil {
		return nil
	}
	return a.store.SaveCheckpoint(domain.Checkpoint{
		ID:         newID(),
		SessionID:  sessionID,
		RunID:      runID,
		ToolCallID: call.ID,
		Tool:       call.Name,
		Path:       out.Path,
		Existed:    out.Snapshot.Existed,
		Before:     out.Snapshot.Before,
		AfterHash:  out.Snapshot.AfterHash,
		Diff:       out.Diff,
		CreatedAt:  time.Now().UTC(),
	})
}

func (a *Agent) Checkpoints(sessionID string) ([]domain.Checkpoint, error) {
	return a.store.Checkpoints(sessionID)
}

func (a *Agent) RevertCheckpoint(sessionID, checkpointID string, force bool) ([]domain.Checkpoint, error) {
	release, err := a.beginRevert(sessionID)
	if err != nil {
		return nil, err
	}
	defer release()
	list, err := a.Checkpoints(sessionID)
	if err != nil {
		return nil, err
	}
	var target domain.Checkpoint
	found := false
	for _, cp := range list {
		if cp.ID == checkpointID {
			target = cp
			found = true
			break
		}
	}
	if !found {
		return nil, store.ErrNotFound
	}
	if target.RevertedAt != nil {
		return nil, errors.New("this checkpoint was already reverted")
	}
	if err := verifyCheckpoint(target, force); err != nil {
		return nil, err
	}
	if err := applyCheckpoint(target); err != nil {
		return nil, err
	}
	stamp, err := a.markReverted([]domain.Checkpoint{target})
	if err != nil {
		return nil, err
	}
	target.RevertedAt = stamp
	a.recordRevert(sessionID, target.RunID, "checkpoint", []domain.Checkpoint{target})
	return []domain.Checkpoint{target}, nil
}

func (a *Agent) RevertRun(sessionID, runID string, force bool) ([]domain.Checkpoint, error) {
	release, err := a.beginRevert(sessionID)
	if err != nil {
		return nil, err
	}
	defer release()
	list, err := a.Checkpoints(sessionID)
	if err != nil {
		return nil, err
	}
	var active []domain.Checkpoint
	for _, cp := range list {
		if cp.RunID == runID && cp.RevertedAt == nil {
			active = append(active, cp)
		}
	}
	if len(active) == 0 {
		return nil, errors.New("this run has no changes to revert")
	}

	groups := map[string][]domain.Checkpoint{}
	var paths []string
	for _, cp := range active {
		if _, seen := groups[cp.Path]; !seen {
			paths = append(paths, cp.Path)
		}
		groups[cp.Path] = append(groups[cp.Path], cp)
	}

	for _, path := range paths {
		cps := groups[path]
		if err := verifyCheckpoint(cps[len(cps)-1], force); err != nil {
			return nil, err
		}
	}
	for _, path := range paths {
		if err := applyCheckpoint(groups[path][0]); err != nil {
			return nil, err
		}
	}
	stamp, err := a.markReverted(active)
	if err != nil {
		return nil, err
	}
	for i := range active {
		active[i].RevertedAt = stamp
	}
	a.recordRevert(sessionID, runID, "run", active)
	return active, nil
}

// beginRevert reserves the session so a new run cannot start while files are
// being restored; the returned function releases the reservation.
func (a *Agent) beginRevert(sessionID string) (func(), error) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if _, busy := a.runs[sessionID]; busy {
		return nil, ErrBusy
	}
	a.runs[sessionID] = func() {}
	return func() { a.finishRun(sessionID) }, nil
}

func (a *Agent) markReverted(list []domain.Checkpoint) (*time.Time, error) {
	stamp := time.Now().UTC()
	for _, cp := range list {
		cp.RevertedAt = &stamp
		if err := a.store.SaveCheckpoint(cp); err != nil {
			return nil, err
		}
	}
	return &stamp, nil
}

func (a *Agent) recordRevert(sessionID, runID, scope string, reverted []domain.Checkpoint) {
	ids := make([]string, 0, len(reverted))
	var paths []string
	seen := map[string]bool{}
	for _, cp := range reverted {
		ids = append(ids, cp.ID)
		if !seen[cp.Path] {
			seen[cp.Path] = true
			paths = append(paths, cp.Path)
		}
	}
	_, _ = a.store.Append(sessionID, domain.EventCheckpointReverted, runID, domain.CheckpointRevertedData{
		Scope:         scope,
		RunID:         runID,
		CheckpointIDs: ids,
		Paths:         paths,
	})
}

func verifyCheckpoint(cp domain.Checkpoint, force bool) error {
	if force {
		return nil
	}
	current, err := os.ReadFile(cp.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &conflictError{message: fmt.Sprintf("%s no longer exists. Turn on \"Overwrite newer changes\" to restore it anyway.", cp.Path)}
		}
		return err
	}
	if hashBytes(current) != cp.AfterHash {
		return &conflictError{message: fmt.Sprintf("%s changed since this checkpoint. Turn on \"Overwrite newer changes\" to revert anyway.", cp.Path)}
	}
	return nil
}

func applyCheckpoint(cp domain.Checkpoint) error {
	if cp.Existed {
		if err := os.MkdirAll(filepath.Dir(cp.Path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(cp.Path, []byte(cp.Before), 0o644)
	}
	if err := os.Remove(cp.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

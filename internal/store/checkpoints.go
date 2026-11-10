package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/weftdesk/weft/internal/domain"
)

func (s *Store) checkpointDir(sessionID string) string {
	return filepath.Join(s.sessionDir(sessionID), "checkpoints")
}

func (s *Store) SaveCheckpoint(cp domain.Checkpoint) error {
	if strings.TrimSpace(cp.ID) == "" {
		cp.ID = newID()
	}
	dir := s.checkpointDir(cp.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, cp.ID+".json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func (s *Store) Checkpoints(sessionID string) ([]domain.Checkpoint, error) {
	s.mu.Lock()
	_, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	dir := s.checkpointDir(sessionID)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]domain.Checkpoint, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var cp domain.Checkpoint
		if err := json.Unmarshal(raw, &cp); err != nil || cp.ID == "" {
			continue
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

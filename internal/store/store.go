package store

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/weftdesk/weft/internal/domain"
)

var ErrNotFound = errors.New("not found")

type sessionState struct {
	meta domain.Session
	file *os.File
}

type Store struct {
	dir string

	mu         sync.Mutex
	workspaces map[string]domain.Workspace
	sessions   map[string]*sessionState
	subs       map[string]map[chan domain.Event]struct{}
}

func Open(dir string) (*Store, error) {
	s := &Store{
		dir:        dir,
		workspaces: map[string]domain.Workspace{},
		sessions:   map[string]*sessionState{},
		subs:       map[string]map[chan domain.Event]struct{}{},
	}
	for _, p := range []string{dir, filepath.Join(dir, "sessions")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
	}
	if err := s.loadWorkspaces(); err != nil {
		return nil, err
	}
	if err := s.loadSessions(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first error
	for _, st := range s.sessions {
		if st.file != nil {
			if err := st.file.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (s *Store) Workspaces() []domain.Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Workspace, 0, len(s.workspaces))
	for _, w := range s.workspaces {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) Workspace(id string) (domain.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workspaces[id]
	if !ok {
		return domain.Workspace{}, ErrNotFound
	}
	return w, nil
}

func (s *Store) CreateWorkspace(name string, folders []domain.Folder) (domain.Workspace, error) {
	if name == "" {
		name = "Workspace"
	}
	if len(folders) == 0 {
		return domain.Workspace{}, errors.New("workspace needs at least one folder")
	}
	for i := range folders {
		if folders[i].ID == "" {
			folders[i].ID = newID()
		}
		if folders[i].Name == "" {
			folders[i].Name = filepath.Base(folders[i].Path)
		}
		abs, err := filepath.Abs(folders[i].Path)
		if err != nil {
			return domain.Workspace{}, err
		}
		folders[i].Path = filepath.Clean(abs)
	}
	w := domain.Workspace{ID: newID(), Name: name, Folders: folders, CreatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.workspaces[w.ID] = w
	err := s.saveWorkspacesLocked()
	s.mu.Unlock()
	if err != nil {
		return domain.Workspace{}, err
	}
	return w, nil
}

func (s *Store) Sessions() []domain.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Session, 0, len(s.sessions))
	for _, st := range s.sessions {
		out = append(out, st.meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (s *Store) Session(id string) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.sessions[id]
	if !ok {
		return domain.Session{}, ErrNotFound
	}
	return st.meta, nil
}

func (s *Store) CreateSession(workspaceID, title string) (domain.Session, error) {
	if _, err := s.Workspace(workspaceID); err != nil {
		return domain.Session{}, fmt.Errorf("workspace: %w", err)
	}
	if title == "" {
		title = "New session"
	}
	now := time.Now().UTC()
	sess := domain.Session{
		ID:          newID(),
		WorkspaceID: workspaceID,
		Title:       title,
		Status:      domain.SessionIdle,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	dir := s.sessionDir(sess.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return domain.Session{}, err
	}
	s.mu.Lock()
	s.sessions[sess.ID] = &sessionState{meta: sess}
	s.mu.Unlock()
	if err := s.writeMeta(sess); err != nil {
		return domain.Session{}, err
	}
	if _, err := s.Append(sess.ID, domain.EventSessionCreated, "", map[string]string{
		"workspaceId": workspaceID,
		"title":       title,
	}); err != nil {
		return domain.Session{}, err
	}
	sess, _ = s.Session(sess.ID)
	return sess, nil
}

func (s *Store) SetSessionTitle(id, title string) error {
	s.mu.Lock()
	st, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	st.meta.Title = title
	st.meta.UpdatedAt = time.Now().UTC()
	meta := st.meta
	s.mu.Unlock()
	return s.writeMeta(meta)
}

func (s *Store) SetSessionStatus(id, status string) error {
	s.mu.Lock()
	st, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	st.meta.Status = status
	st.meta.UpdatedAt = time.Now().UTC()
	meta := st.meta
	s.mu.Unlock()
	return s.writeMeta(meta)
}

func (s *Store) Append(sessionID, typ, runID string, data any) (domain.Event, error) {
	s.mu.Lock()
	st, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return domain.Event{}, ErrNotFound
	}
	if st.file == nil {
		f, err := os.OpenFile(filepath.Join(s.sessionDir(sessionID), "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			s.mu.Unlock()
			return domain.Event{}, err
		}
		st.file = f
	}
	ev := domain.Event{
		ID:        newID(),
		SessionID: sessionID,
		Seq:       st.meta.LastSeq + 1,
		Type:      typ,
		RunID:     runID,
		At:        time.Now().UTC(),
		Data:      data,
	}
	line, err := json.Marshal(ev)
	if err != nil {
		s.mu.Unlock()
		return domain.Event{}, err
	}
	if _, err := st.file.Write(append(line, '\n')); err != nil {
		s.mu.Unlock()
		return domain.Event{}, err
	}
	st.meta.LastSeq = ev.Seq
	st.meta.UpdatedAt = ev.At
	s.mu.Unlock()
	s.broadcast(sessionID, ev)
	return ev, nil
}

// Publish sends a live-only event (streaming deltas). It is not persisted.
func (s *Store) Publish(sessionID, typ, runID string, data any) {
	ev := domain.Event{
		ID:        newID(),
		SessionID: sessionID,
		Type:      typ,
		RunID:     runID,
		At:        time.Now().UTC(),
		Data:      data,
	}
	s.broadcast(sessionID, ev)
}

func (s *Store) EventsAfter(sessionID string, after int64) ([]domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return nil, ErrNotFound
	}
	path := filepath.Join(s.sessionDir(sessionID), "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []domain.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev domain.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Seq > after {
			out = append(out, ev)
		}
	}
	return out, sc.Err()
}

func (s *Store) Subscribe(sessionID string) (<-chan domain.Event, func()) {
	ch := make(chan domain.Event, 1024)
	s.mu.Lock()
	if s.subs[sessionID] == nil {
		s.subs[sessionID] = map[chan domain.Event]struct{}{}
	}
	s.subs[sessionID][ch] = struct{}{}
	s.mu.Unlock()
	cancel := func() {
		s.mu.Lock()
		if set, ok := s.subs[sessionID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(s.subs, sessionID)
			}
		}
		s.mu.Unlock()
	}
	return ch, cancel
}

func (s *Store) broadcast(sessionID string, ev domain.Event) {
	s.mu.Lock()
	subs := make([]chan domain.Event, 0, len(s.subs[sessionID]))
	for ch := range s.subs[sessionID] {
		subs = append(subs, ch)
	}
	s.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// Slow consumers can resume from their last seen sequence.
		}
	}
}

func (s *Store) sessionDir(id string) string {
	return filepath.Join(s.dir, "sessions", id)
}

func (s *Store) writeMeta(sess domain.Session) error {
	dir := s.sessionDir(sess.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "session.json.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "session.json"))
}

func (s *Store) saveWorkspacesLocked() error {
	raw, err := json.MarshalIndent(struct {
		Workspaces []domain.Workspace `json:"workspaces"`
	}{Workspaces: values(s.workspaces)}, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, "workspaces.json.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, "workspaces.json"))
}

func (s *Store) loadWorkspaces() error {
	raw, err := os.ReadFile(filepath.Join(s.dir, "workspaces.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file struct {
		Workspaces []domain.Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return err
	}
	for _, w := range file.Workspaces {
		s.workspaces[w.ID] = w
	}
	return nil
}

func (s *Store) loadSessions() error {
	root := filepath.Join(s.dir, "sessions")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "session.json"))
		if err != nil {
			continue
		}
		var sess domain.Session
		if err := json.Unmarshal(raw, &sess); err != nil || sess.ID == "" {
			continue
		}
		sess.LastSeq = lastSeq(filepath.Join(dir, "events.jsonl"))
		s.sessions[sess.ID] = &sessionState{meta: sess}
	}
	return nil
}

func lastSeq(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var max int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev struct {
			Seq int64 `json:"seq"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Seq > max {
			max = ev.Seq
		}
	}
	return max
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func values(m map[string]domain.Workspace) []domain.Workspace {
	out := make([]domain.Workspace, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

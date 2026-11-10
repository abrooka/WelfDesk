package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weftdesk/weft/internal/agent"
	"github.com/weftdesk/weft/internal/config"
	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
	"github.com/weftdesk/weft/internal/store"
	"github.com/weftdesk/weft/internal/tools"
	"github.com/weftdesk/weft/internal/worktree"
)

type Server struct {
	store  *store.Store
	agent  *agent.Agent
	webDir string

	cfgMu sync.RWMutex
	cfg   config.Config
	srv   *http.Server
}

func New(st *store.Store, ag *agent.Agent, cfg config.Config, webDir string) *Server {
	s := &Server{store: st, agent: ag, cfg: cfg, webDir: webDir}
	s.srv = &http.Server{
		Addr:              cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

func (s *Server) Addr() string { return s.srv.Addr }

func (s *Server) Start() error {
	err := s.srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("PUT /api/config", s.putConfig)
	mux.HandleFunc("GET /api/tools", s.listTools)
	mux.HandleFunc("GET /api/workspaces", s.listWorkspaces)
	mux.HandleFunc("POST /api/workspaces", s.createWorkspace)
	mux.HandleFunc("GET /api/workspaces/{id}/git", s.workspaceGit)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("POST /api/sessions", s.createSession)
	mux.HandleFunc("GET /api/sessions/{id}", s.getSession)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.getEvents)
	mux.HandleFunc("GET /api/sessions/{id}/stream", s.streamEvents)
	mux.HandleFunc("GET /api/sessions/{id}/requests", s.getRequests)
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.postMessage)
	mux.HandleFunc("POST /api/sessions/{id}/cancel", s.cancelRun)
	mux.HandleFunc("POST /api/sessions/{id}/permissions/{requestId}", s.resolvePermission)
	mux.HandleFunc("POST /api/sessions/{id}/questions/{requestId}", s.answerQuestion)
	mux.HandleFunc("GET /api/sessions/{id}/checkpoints", s.listCheckpoints)
	mux.HandleFunc("POST /api/sessions/{id}/checkpoints/{checkpointId}/revert", s.revertCheckpoint)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{runId}/revert", s.revertRun)
	mux.HandleFunc("GET /api/sessions/{id}/files", s.listFiles)
	mux.HandleFunc("GET /api/sessions/{id}/file", s.readWorkspaceFile)
	mux.HandleFunc("GET /", s.serveWeb)
	return withCORS(mux)
}

func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	if s.webDir == "" || strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	target := filepath.Join(s.webDir, filepath.FromSlash(rel))
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, target)
		return
	}
	if rel != "" && strings.Contains(path.Base(rel), ".") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	cfg := s.agent.Config()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"name":     "weft",
		"version":  config.Version,
		"provider": cfg.Provider.Kind,
		"model":    cfg.Provider.Model,
		"mcp":      s.agent.MCPStatus(),
	})
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agent.Config().Masked())
}

type providerUpdate struct {
	Kind      *string `json:"kind"`
	BaseURL   *string `json:"baseUrl"`
	APIKey    *string `json:"apiKey"`
	Model     *string `json:"model"`
	MaxTokens *int    `json:"maxTokens"`
}

type configUpdate struct {
	PermissionMode *string                      `json:"permissionMode"`
	MaxTurns       *int                         `json:"maxTurns"`
	Provider       *providerUpdate              `json:"provider"`
	MCPServers     *map[string]config.MCPServer `json:"mcpServers"`
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var update configUpdate
	if err := readJSON(r, &update); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.cfgMu.Lock()

	cfg := s.cfg
	if update.PermissionMode != nil {
		cfg.PermissionMode = *update.PermissionMode
	}
	if update.MaxTurns != nil && *update.MaxTurns > 0 {
		cfg.MaxTurns = *update.MaxTurns
	}
	if update.MCPServers != nil {
		servers := make(map[string]config.MCPServer, len(*update.MCPServers))
		for name, server := range *update.MCPServers {
			if len(server.Env) == 0 {
				server.Env = cfg.MCPServers[name].Env
			}
			servers[name] = server
		}
		cfg.MCPServers = servers
	}
	if p := update.Provider; p != nil {
		if p.Kind != nil {
			cfg.Provider.Kind = *p.Kind
		}
		if p.BaseURL != nil {
			cfg.Provider.BaseURL = *p.BaseURL
		}
		if p.APIKey != nil && *p.APIKey != "" {
			cfg.Provider.APIKey = *p.APIKey
		}
		if p.Model != nil {
			cfg.Provider.Model = *p.Model
		}
		if p.MaxTokens != nil && *p.MaxTokens > 0 {
			cfg.Provider.MaxTokens = *p.MaxTokens
		}
	}
	config.Normalize(&cfg)
	prov, err := provider.New(cfg.Provider)
	if err != nil {
		s.cfgMu.Unlock()
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := cfg.Save(); err != nil {
		s.cfgMu.Unlock()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.cfg = cfg
	s.agent.UpdateConfig(cfg, prov)
	s.cfgMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	s.agent.StartMCP(ctx)
	writeJSON(w, http.StatusOK, cfg.Masked())
}

func (s *Server) listTools(w http.ResponseWriter, r *http.Request) {
	workspaces := s.store.Workspaces()
	roots := []string{}
	if len(workspaces) > 0 {
		for _, folder := range workspaces[0].Folders {
			roots = append(roots, folder.Path)
		}
	}
	registry := tools.NewRegistry(roots)
	names := registry.Names()
	names = append(names, s.agent.MCPToolNames()...)
	writeJSON(w, http.StatusOK, map[string]any{"tools": names})
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": s.store.Workspaces()})
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Folders []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"folders"`
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var folders []domain.Folder
	for _, folder := range body.Folders {
		folders = append(folders, domain.Folder{Name: folder.Name, Path: folder.Path})
	}
	for _, path := range body.Paths {
		folders = append(folders, domain.Folder{Path: path})
	}
	if len(folders) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one folder is required"))
		return
	}
	for _, folder := range folders {
		info, err := os.Stat(folder.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("folder %s: %w", folder.Path, err))
			return
		}
		if !info.IsDir() {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%s is not a directory", folder.Path))
			return
		}
	}
	ws, err := s.store.CreateWorkspace(body.Name, folders)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	all := s.store.Sessions()
	if workspaceID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": all})
		return
	}
	out := make([]domain.Session, 0, len(all))
	for _, sess := range all {
		if sess.WorkspaceID == workspaceID {
			out = append(out, sess)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) workspaceGit(w http.ResponseWriter, r *http.Request) {
	ws, err := s.store.Workspace(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": worktree.Detect(ws)})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceID string `json:"workspaceId"`
		Title       string `json:"title"`
		Worktree    bool   `json:"worktree"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Worktree && body.Title == "" {
		body.Title = "Worktree session"
	}
	sess, err := s.store.CreateSession(body.WorkspaceID, body.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Worktree {
		if err := s.attachWorktree(&sess); err != nil {
			_ = s.store.DeleteSession(sess.ID)
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) attachWorktree(sess *domain.Session) error {
	ws, err := s.store.Workspace(sess.WorkspaceID)
	if err != nil {
		return err
	}
	repos := worktree.Detect(ws)
	if len(repos) == 0 {
		return errors.New("workspace has no git repository")
	}
	repo := repos[0]
	branch := "weft/" + sess.ID[:8]
	dir := filepath.Join(s.cfg.DataDir, "worktrees", filepath.Base(repo.Root)+"-"+sess.ID[:8])
	if err := worktree.Create(repo.Root, dir, branch); err != nil {
		return err
	}
	wt := &domain.Worktree{FolderID: repo.FolderID, RepoPath: repo.Root, Path: dir, Branch: branch}
	if err := s.store.SetSessionWorktree(sess.ID, wt); err != nil {
		_ = worktree.Remove(repo.Root, dir)
		return err
	}
	sess.Worktree = wt
	return nil
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.store.Session(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	ws, _ := s.store.Workspace(sess.WorkspaceID)
	writeJSON(w, http.StatusOK, map[string]any{
		"session":   sess,
		"workspace": ws,
		"running":   s.agent.Running(id),
	})
}

func (s *Server) getEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	after := parseSeq(r)
	events, err := s.store.EventsAfter(id, after)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	var last int64 = after
	if len(events) > 0 {
		last = events[len(events)-1].Seq
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "lastSeq": last})
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	after := parseSeq(r)
	if _, err := s.store.Session(id); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch, cancel := s.store.Subscribe(id)
	defer cancel()

	var lastSeq int64
	if events, err := s.store.EventsAfter(id, after); err == nil {
		for _, ev := range events {
			if ev.Seq <= lastSeq {
				continue
			}
			writeSSE(w, ev)
			lastSeq = ev.Seq
		}
	}
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Seq > 0 && ev.Seq <= lastSeq {
				continue
			}
			if ev.Seq > 0 {
				lastSeq = ev.Seq
			}
			writeSSE(w, ev)
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, ev domain.Event) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return
	}
	if ev.Seq > 0 {
		fmt.Fprintf(w, "id: %d\n", ev.Seq)
	}
	fmt.Fprintf(w, "data: %s\n\n", raw)
}

func (s *Server) getRequests(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := s.store.EventsAfter(id, 0)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	results := map[string]json.RawMessage{}
	for _, ev := range events {
		switch ev.Type {
		case domain.EventRequestCompleted:
			var data struct {
				RequestID string `json:"requestId"`
			}
			if raw, err := json.Marshal(ev.Data); err == nil && json.Unmarshal(raw, &data) == nil {
				results[data.RequestID] = raw
			}
		}
	}
	out := make([]map[string]any, 0)
	for _, ev := range events {
		if ev.Type != domain.EventRequestStarted {
			continue
		}
		raw, err := json.Marshal(ev.Data)
		if err != nil {
			continue
		}
		var snapshot map[string]any
		if json.Unmarshal(raw, &snapshot) != nil {
			continue
		}
		reqID, _ := snapshot["requestId"].(string)
		item := map[string]any{"at": ev.At, "started": snapshot}
		if result, ok := results[reqID]; ok {
			var parsed map[string]any
			if json.Unmarshal(result, &parsed) == nil {
				item["result"] = parsed
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.agent.Send(r.Context(), id, body.Text); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, agent.ErrBusy) {
			status = http.StatusConflict
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.agent.Cancel(id) {
		writeJSON(w, http.StatusOK, map[string]bool{"cancelled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

func (s *Server) resolvePermission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	requestID := r.PathValue("requestId")
	var body struct {
		Decision string `json:"decision"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.agent.ResolvePermission(id, requestID, body.Decision); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	requestID := r.PathValue("requestId")
	var body struct {
		Answers []domain.QuestionAnswer `json:"answers"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.agent.AnswerQuestion(id, requestID, body.Answers); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type checkpointSummary struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"sessionId"`
	RunID      string     `json:"runId"`
	ToolCallID string     `json:"toolCallId"`
	Tool       string     `json:"tool"`
	Path       string     `json:"path"`
	Existed    bool       `json:"existed"`
	Diff       string     `json:"diff,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	RevertedAt *time.Time `json:"revertedAt,omitempty"`
}

func summarizeCheckpoints(list []domain.Checkpoint) []checkpointSummary {
	out := make([]checkpointSummary, 0, len(list))
	for _, cp := range list {
		out = append(out, checkpointSummary{
			ID:         cp.ID,
			SessionID:  cp.SessionID,
			RunID:      cp.RunID,
			ToolCallID: cp.ToolCallID,
			Tool:       cp.Tool,
			Path:       cp.Path,
			Existed:    cp.Existed,
			Diff:       cp.Diff,
			CreatedAt:  cp.CreatedAt,
			RevertedAt: cp.RevertedAt,
		})
	}
	return out
}

func (s *Server) listCheckpoints(w http.ResponseWriter, r *http.Request) {
	list, err := s.agent.Checkpoints(r.PathValue("id"))
	if err != nil {
		writeCheckpointError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkpoints": summarizeCheckpoints(list)})
}

func (s *Server) revertCheckpoint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Force bool `json:"force"`
	}
	if err := readOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	reverted, err := s.agent.RevertCheckpoint(r.PathValue("id"), r.PathValue("checkpointId"), body.Force)
	if err != nil {
		writeCheckpointError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reverted": summarizeCheckpoints(reverted)})
}

func (s *Server) revertRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Force bool `json:"force"`
	}
	if err := readOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	reverted, err := s.agent.RevertRun(r.PathValue("id"), r.PathValue("runId"), body.Force)
	if err != nil {
		writeCheckpointError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reverted": summarizeCheckpoints(reverted)})
}

func writeCheckpointError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, agent.ErrBusy), errors.Is(err, agent.ErrConflict):
		status = http.StatusConflict
	}
	writeError(w, status, err)
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	sess, ws, ok := s.sessionWorkspace(w, r)
	if !ok {
		return
	}
	ws = worktree.Apply(ws, sess.Worktree)
	root, target, err := resolveWorkspacePath(ws, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	type entry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"isDir"`
	}
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".github" && name != ".weft" {
			continue
		}
		out = append(out, entry{Name: name, Path: filepath.Join(target, name), IsDir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"root":    root,
		"path":    target,
		"entries": out,
	})
}

func (s *Server) readWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	sess, ws, ok := s.sessionWorkspace(w, r)
	if !ok {
		return
	}
	ws = worktree.Apply(ws, sess.Worktree)
	root, target, err := resolveWorkspacePath(ws, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, errors.New("path is a directory"))
		return
	}
	const limit = 2 << 20
	truncated := false
	size := info.Size()
	if size > limit {
		size = limit
		truncated = true
	}
	f, err := os.Open(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer f.Close()
	raw := make([]byte, size)
	if _, err := io.ReadFull(f, raw); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"root":      root,
		"path":      target,
		"content":   string(raw),
		"truncated": truncated,
	})
}

func (s *Server) sessionWorkspace(w http.ResponseWriter, r *http.Request) (domain.Session, domain.Workspace, bool) {
	sess, err := s.store.Session(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return domain.Session{}, domain.Workspace{}, false
	}
	ws, err := s.store.Workspace(sess.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return domain.Session{}, domain.Workspace{}, false
	}
	return sess, ws, true
}

func resolveWorkspacePath(ws domain.Workspace, path string) (string, string, error) {
	if len(ws.Folders) == 0 {
		return "", "", errors.New("workspace has no folders")
	}
	root := ws.Folders[0].Path
	if strings.TrimSpace(path) == "" {
		return root, root, nil
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target = filepath.Clean(target)
	for _, folder := range ws.Folders {
		if within(folder.Path, target) {
			return folder.Path, target, nil
		}
	}
	return "", "", errors.New("path is outside the workspace")
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
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func parseSeq(r *http.Request) int64 {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		raw = r.Header.Get("Last-Event-ID")
	}
	n, _ := strconv.ParseInt(raw, 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

func readJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func readOptionalJSON(r *http.Request, out any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return readJSON(r, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

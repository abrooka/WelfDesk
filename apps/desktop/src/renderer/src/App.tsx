import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from './api'
import { Sidebar } from './components/Sidebar'
import { Timeline } from './components/Timeline'
import { Composer } from './components/Composer'
import { PlanBar, latestTodos } from './components/PlanBar'
import { Outline } from './components/Outline'
import { ChangesPanel } from './components/ChangesPanel'
import { RequestsPanel } from './components/RequestsPanel'
import { FilesPanel } from './components/FilesPanel'
import { SettingsDialog } from './components/SettingsDialog'
import { WorkspaceDialog } from './components/WorkspaceDialog'
import { Icon } from './components/Icon'
import { Toasts, type ToastItem } from './components/Toasts'
import { useSessionStream } from './hooks/useSessionStream'
import { applyTheme, currentTheme, type ThemeId } from './lib/theme'
import type { Config, Health, QuestionAnswer, Session, WeftEvent, Workspace } from './types'

type InspectorTab = 'outline' | 'changes' | 'requests' | 'files'

const STORAGE = {
  workspace: 'weft.workspace',
  session: 'weft.session',
  inspector: 'weft.inspector',
}

export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [config, setConfig] = useState<Config | null>(null)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [sessions, setSessions] = useState<Session[]>([])
  const [activeWorkspaceId, setActiveWorkspaceId] = useState<string | null>(null)
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null)
  const [detail, setDetail] = useState<{ session: Session; workspace: Workspace } | null>(null)
  const [booting, setBooting] = useState(true)
  const [appError, setAppError] = useState<string | null>(null)
  const [inspectorOpen, setInspectorOpen] = useState(() => localStorage.getItem(STORAGE.inspector) !== 'closed')
  const [inspectorTab, setInspectorTab] = useState<InspectorTab>('outline')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [workspaceOpen, setWorkspaceOpen] = useState(false)
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const [requestRevision, setRequestRevision] = useState(0)
  const [changeRevision, setChangeRevision] = useState(0)
  const [maximized, setMaximized] = useState(false)
  const [theme, setTheme] = useState<ThemeId>(currentTheme)

  const changeTheme = useCallback((id: ThemeId) => {
    applyTheme(id)
    setTheme(id)
  }, [])

  useEffect(() => {
    if (!window.weft?.windowState) return
    void window.weft.windowState().then((state) => setMaximized(state.maximized))
    return window.weft.onWindowState((state) => setMaximized(state.maximized))
  }, [])

  const notify = useCallback((text: string, tone: 'info' | 'error' = 'info') => {
    const id = Date.now() + Math.random()
    setToasts((current) => [...current, { id, tone, text }])
  }, [])

  const dismissToast = useCallback((id: number) => {
    setToasts((current) => current.filter((item) => item.id !== id))
  }, [])

  const bootstrap = useCallback(async () => {
    setBooting(true)
    setAppError(null)
    try {
      const [healthData, configData, workspaceData] = await Promise.all([
        api.health(),
        api.config(),
        api.workspaces(),
      ])
      setHealth(healthData)
      setConfig(configData)
      setWorkspaces(workspaceData.workspaces)
      const stored = localStorage.getItem(STORAGE.workspace)
      const workspace =
        workspaceData.workspaces.find((item) => item.id === stored) ?? workspaceData.workspaces[0] ?? null
      setActiveWorkspaceId(workspace?.id ?? null)
    } catch (cause) {
      setAppError(cause instanceof Error ? cause.message : 'Cannot reach the Weft server')
      setHealth(null)
    } finally {
      setBooting(false)
    }
  }, [])

  useEffect(() => {
    void bootstrap()
  }, [bootstrap])

  const refreshSessions = useCallback(async () => {
    if (!activeWorkspaceId) {
      setSessions([])
      return
    }
    try {
      const data = await api.sessions(activeWorkspaceId)
      setSessions(data.sessions)
    } catch {
      // The next event or explicit action will retry.
    }
  }, [activeWorkspaceId])

  useEffect(() => {
    setSessions([])
    setActiveSessionId(null)
    setDetail(null)
    if (!activeWorkspaceId) return
    localStorage.setItem(STORAGE.workspace, activeWorkspaceId)
    void refreshSessions()
  }, [activeWorkspaceId, refreshSessions])

  useEffect(() => {
    if (sessions.length === 0) return
    if (activeSessionId && sessions.some((session) => session.id === activeSessionId)) return
    const stored = localStorage.getItem(STORAGE.session)
    const next = sessions.find((session) => session.id === stored) ?? sessions[0]
    setActiveSessionId(next.id)
  }, [sessions, activeSessionId])

  useEffect(() => {
    if (activeSessionId) localStorage.setItem(STORAGE.session, activeSessionId)
    if (!activeSessionId) {
      setDetail(null)
      return
    }
    let cancelled = false
    api
      .session(activeSessionId)
      .then((data) => {
        if (!cancelled) setDetail({ session: data.session, workspace: data.workspace })
      })
      .catch(() => {
        if (!cancelled) setDetail(null)
      })
    return () => {
      cancelled = true
    }
  }, [activeSessionId])

  const refreshDetail = useCallback(async () => {
    if (!activeSessionId) return
    try {
      const data = await api.session(activeSessionId)
      setDetail({ session: data.session, workspace: data.workspace })
    } catch {
      // keep the last known state
    }
  }, [activeSessionId])

  const onStreamEvent = useCallback(
    (event: WeftEvent) => {
      if (event.type === 'turn.started') {
        setDetail((current) =>
          current ? { ...current, session: { ...current.session, status: 'running' } } : current,
        )
      }
      if (event.type === 'user.message') {
        void refreshSessions()
      }
      if (event.type === 'tool.result' || event.type === 'checkpoint.reverted') {
        setChangeRevision((value) => value + 1)
      }
      if (event.type === 'turn.completed' || event.type === 'error') {
        void refreshDetail()
        void refreshSessions()
        setRequestRevision((value) => value + 1)
      }
    },
    [refreshDetail, refreshSessions],
  )

  const { events, live, loading, connected, error } = useSessionStream(activeSessionId, onStreamEvent)

  const activeSession = useMemo(
    () => detail?.session ?? sessions.find((session) => session.id === activeSessionId) ?? null,
    [detail, sessions, activeSessionId],
  )
  const activeWorkspace = useMemo(
    () => detail?.workspace ?? workspaces.find((workspace) => workspace.id === activeWorkspaceId) ?? null,
    [detail, workspaces, activeWorkspaceId],
  )
  const running = activeSession?.status === 'running'
  const todos = useMemo(() => latestTodos(events), [events])

  const toggleInspector = () => {
    setInspectorOpen((value) => {
      localStorage.setItem(STORAGE.inspector, value ? 'closed' : 'open')
      return !value
    })
  }

  const selectWorkspace = (id: string) => {
    setActiveWorkspaceId(id)
    setActiveSessionId(null)
    setDetail(null)
  }

  const selectSession = (id: string) => {
    setActiveSessionId(id)
  }

  const newSession = useCallback(async () => {
    if (!activeWorkspaceId) {
      setWorkspaceOpen(true)
      return
    }
    try {
      const session = await api.createSession(activeWorkspaceId)
      setSessions((current) => [session, ...current])
      setActiveSessionId(session.id)
    } catch (cause) {
      notify(cause instanceof Error ? cause.message : 'Failed to create session', 'error')
    }
  }, [activeWorkspaceId, notify])

  const workspaceCreated = useCallback(
    async (workspace: Workspace) => {
      setWorkspaces((current) => [...current, workspace])
      setActiveWorkspaceId(workspace.id)
      setActiveSessionId(null)
      setDetail(null)
      try {
        const session = await api.createSession(workspace.id)
        setSessions([session])
        setActiveSessionId(session.id)
      } catch {
        // A session can be created from the sidebar later.
      }
    },
    [],
  )

  const send = async (text: string) => {
    if (!activeSessionId) return
    try {
      await api.send(activeSessionId, text)
      setDetail((current) =>
        current ? { ...current, session: { ...current.session, status: 'running' } } : current,
      )
    } catch (cause) {
      notify(cause instanceof Error ? cause.message : 'Failed to send message', 'error')
    }
  }

  const cancel = async () => {
    if (!activeSessionId) return
    try {
      await api.cancel(activeSessionId)
    } catch (cause) {
      notify(cause instanceof Error ? cause.message : 'Failed to cancel run', 'error')
    }
  }

  const resolvePermission = async (requestId: string, decision: 'allow' | 'deny') => {
    if (!activeSessionId) return
    try {
      await api.resolvePermission(activeSessionId, requestId, decision)
    } catch (cause) {
      notify(cause instanceof Error ? cause.message : 'Failed to resolve approval', 'error')
    }
  }

  const answerQuestion = async (requestId: string, answers: QuestionAnswer[]) => {
    if (!activeSessionId) return
    try {
      await api.answerQuestion(activeSessionId, requestId, answers)
    } catch (cause) {
      notify(cause instanceof Error ? cause.message : 'Failed to submit answers', 'error')
    }
  }

  const reveal = (path: string) => {
    if (window.weft) {
      void window.weft.reveal(path).catch((cause: unknown) => {
        notify(cause instanceof Error ? cause.message : 'Cannot reveal path', 'error')
      })
    } else {
      notify(path, 'info')
    }
  }

  const copy = (text: string) => {
    if (window.weft) {
      void window.weft.copyText(text).then(() => notify('Copied'))
    } else {
      void navigator.clipboard.writeText(text).then(() => notify('Copied'))
    }
  }

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const mod = event.metaKey || event.ctrlKey
      if (mod && event.key === ',') {
        event.preventDefault()
        setSettingsOpen(true)
      }
      if (mod && event.key.toLowerCase() === 'n' && !event.shiftKey) {
        event.preventDefault()
        void newSession()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [newSession])

  if (booting) {
    return (
      <div className="boot">
        <div className="boot-mark">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round">
            <path d="M3.5 8.5c4.5-4 12.5 4 17 0" />
            <path d="M3.5 15.5c4.5-4 12.5 4 17 0" />
          </svg>
        </div>
        <span>Starting WeftDesk…</span>
      </div>
    )
  }

  return (
    <div className="app">
      <Sidebar
        workspaces={workspaces}
        sessions={sessions}
        activeWorkspaceId={activeWorkspaceId}
        activeSessionId={activeSessionId}
        health={health}
        onSelectWorkspace={selectWorkspace}
        onSelectSession={selectSession}
        onNewWorkspace={() => setWorkspaceOpen(true)}
        onNewSession={() => void newSession()}
        onOpenSettings={() => setSettingsOpen(true)}
      />

      <main className="main">
        <header className="topbar">
          <div className="topbar-main">
            <div className="topbar-title">
              <h1>{activeSession?.title || (activeWorkspace ? 'New session' : 'WeftDesk')}</h1>
              <div className="topbar-sub">
                {activeWorkspace?.folders.map((folder) => (
                  <span key={folder.id} className="folder-chip" title={folder.path}>
                    <Icon name="folder" size={11} />
                    {folder.name}
                  </span>
                ))}
              </div>
            </div>
            <div className="topbar-actions">
              {activeSession && (
                <span className={`run-pill ${running ? 'running' : activeSession.status}`}>{running ? 'Running' : activeSession.status}</span>
              )}
              {health && (
                <span className="model-chip" title={`${health.provider} · ${health.model}`}>
                  {health.provider} · {health.model}
                </span>
              )}
              {activeSession && (
                <span className={`connection ${connected ? 'on' : 'off'}`} title={connected ? 'Live stream connected' : 'Reconnecting'}>
                  <span className="status-dot" />
                </span>
              )}
              <button
                className={`icon-btn ${inspectorOpen ? 'active' : ''}`}
                onClick={toggleInspector}
                title="Toggle inspector"
              >
                <Icon name="panel" size={16} />
              </button>
              <button className="icon-btn" onClick={() => setSettingsOpen(true)} title="Settings">
                <Icon name="settings" size={16} />
              </button>
              {window.weft?.windowControl && <WindowControls maximized={maximized} />}
            </div>
          </div>
        </header>

        {appError && (
          <div className="offline-banner">
            <Icon name="alert" size={15} />
            <span>{appError}</span>
            <button className="btn ghost small" onClick={() => void bootstrap()}>Retry</button>
          </div>
        )}

        <div className="content">
          <section className="center">
            {activeSession ? (
              <>
                <Timeline
                  events={events}
                  live={live}
                  running={running}
                  loading={loading}
                  error={error}
                  onResolvePermission={resolvePermission}
                  onAnswerQuestion={answerQuestion}
                  onReveal={reveal}
                  onCopy={copy}
                />
                {todos.length > 0 && <PlanBar todos={todos} />}
                <Composer sessionId={activeSession.id} running={running} onSend={send} onCancel={() => void cancel()} />
              </>
            ) : activeWorkspace ? (
              <div className="empty-main">
                <div className="empty-card">
                  <div className="empty-mark">
                    <Icon name="message" size={22} />
                  </div>
                  <h2>Ready when you are</h2>
                  <p>
                    This workspace is connected to {activeWorkspace.folders.length} folder
                    {activeWorkspace.folders.length === 1 ? '' : 's'}. Start a session to talk to Weft.
                  </p>
                  <button className="btn primary" onClick={() => void newSession()}>
                    <Icon name="plus" size={15} /> New session
                  </button>
                </div>
              </div>
            ) : (
              <div className="empty-main">
                <div className="empty-card onboarding">
                  <div className="empty-mark large">
                    <Icon name="spark" size={24} />
                  </div>
                  <h2>An agent workbench for your code</h2>
                  <p>
                    WeftDesk connects a model to your local folders. It reads files, makes edits and runs commands,
                    while every step stays visible and reviewable.
                  </p>
                  <button className="btn primary" onClick={() => setWorkspaceOpen(true)}>
                    <Icon name="folderPlus" size={15} /> Create workspace
                  </button>
                </div>
              </div>
            )}
          </section>

          {inspectorOpen && (
            <aside className="inspector">
              <div className="inspector-tabs">
                <button
                  className={`tab ${inspectorTab === 'outline' ? 'active' : ''}`}
                  onClick={() => setInspectorTab('outline')}
                >
                  <Icon name="list" size={14} /> Outline
                </button>
                <button
                  className={`tab ${inspectorTab === 'changes' ? 'active' : ''}`}
                  onClick={() => setInspectorTab('changes')}
                >
                  <Icon name="history" size={14} /> Changes
                </button>
                <button
                  className={`tab ${inspectorTab === 'requests' ? 'active' : ''}`}
                  onClick={() => setInspectorTab('requests')}
                >
                  <Icon name="network" size={14} /> Requests
                </button>
                <button
                  className={`tab ${inspectorTab === 'files' ? 'active' : ''}`}
                  onClick={() => setInspectorTab('files')}
                >
                  <Icon name="folder" size={14} /> Files
                </button>
                <button className="icon-btn inspector-close" onClick={toggleInspector} title="Close inspector">
                  <Icon name="close" size={14} />
                </button>
              </div>

              <div className="inspector-body">
                {!activeSession && (
                  <p className="panel-note">Open a session to inspect its outline, changes, requests and files.</p>
                )}
                {activeSession && inspectorTab === 'outline' && <Outline events={events} />}
                {activeSession && inspectorTab === 'changes' && activeWorkspace && (
                  <ChangesPanel
                    key={activeSession.id}
                    sessionId={activeSession.id}
                    folders={activeWorkspace.folders}
                    running={running}
                    revision={changeRevision}
                    onNotify={notify}
                  />
                )}
                {activeSession && inspectorTab === 'requests' && (
                  <RequestsPanel sessionId={activeSession.id} revision={requestRevision} />
                )}
                {activeSession && inspectorTab === 'files' && activeWorkspace && (
                  <FilesPanel
                    key={activeSession.id}
                    sessionId={activeSession.id}
                    folders={activeWorkspace.folders}
                    onNotify={notify}
                  />
                )}
              </div>
            </aside>
          )}
        </div>
      </main>

      {settingsOpen && (
        <SettingsDialog
          config={config}
          health={health}
          theme={theme}
          onTheme={changeTheme}
          onClose={() => setSettingsOpen(false)}
          onSaved={(updated) => {
            setConfig(updated)
            void api.health().then(setHealth).catch(() => undefined)
          }}
          onNotify={notify}
        />
      )}

      {workspaceOpen && (
        <WorkspaceDialog
          onClose={() => setWorkspaceOpen(false)}
          onCreated={(workspace) => void workspaceCreated(workspace)}
          onNotify={notify}
        />
      )}

      <Toasts items={toasts} onDismiss={dismissToast} />
    </div>
  )
}

function WindowControls({ maximized }: { maximized: boolean }) {
  const control = (action: 'minimize' | 'toggle-maximize' | 'close') => {
    void window.weft?.windowControl(action)
  }
  return (
    <div className="window-controls">
      <button className="window-btn" onClick={() => control('minimize')} title="Minimize" aria-label="Minimize">
        <Icon name="minimize" size={14} />
      </button>
      <button
        className="window-btn"
        onClick={() => control('toggle-maximize')}
        title={maximized ? 'Restore' : 'Maximize'}
        aria-label={maximized ? 'Restore' : 'Maximize'}
      >
        <Icon name={maximized ? 'restore' : 'maximize'} size={13} />
      </button>
      <button className="window-btn close" onClick={() => control('close')} title="Close" aria-label="Close">
        <Icon name="close" size={14} />
      </button>
    </div>
  )
}

import { Icon } from './Icon'
import { relativeTime } from '../lib/format'
import type { Health, Session, Workspace } from '../types'

export function Sidebar({
  workspaces,
  sessions,
  activeWorkspaceId,
  activeSessionId,
  health,
  onSelectWorkspace,
  onSelectSession,
  onNewWorkspace,
  onNewSession,
  onOpenSettings,
}: {
  workspaces: Workspace[]
  sessions: Session[]
  activeWorkspaceId: string | null
  activeSessionId: string | null
  health: Health | null
  onSelectWorkspace: (id: string) => void
  onSelectSession: (id: string) => void
  onNewWorkspace: () => void
  onNewSession: () => void
  onOpenSettings: () => void
}) {
  return (
    <aside className="sidebar">
      <div className="brand">
        <svg className="brand-mark" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
          <path d="M3.5 8.5c4.5-4 12.5 4 17 0" />
          <path d="M3.5 15.5c4.5-4 12.5 4 17 0" />
          <path d="M12 4v16" opacity=".35" />
        </svg>
        <span className="brand-name">WeftDesk</span>
      </div>

      <div className="side-section">
        <div className="side-head">
          <span>Workspaces</span>
          <button className="icon-btn" onClick={onNewWorkspace} title="Add workspace">
            <Icon name="plus" size={15} />
          </button>
        </div>
        <div className="side-list">
          {workspaces.map((workspace) => (
            <button
              key={workspace.id}
              className={`side-item ${workspace.id === activeWorkspaceId ? 'active' : ''}`}
              onClick={() => onSelectWorkspace(workspace.id)}
            >
              <Icon name="folder" size={15} />
              <span className="side-label">{workspace.name}</span>
              <span className="side-count">{workspace.folders.length}</span>
            </button>
          ))}
          {workspaces.length === 0 && <p className="side-empty">No workspaces yet.</p>}
        </div>
      </div>

      <div className="side-section grow">
        <div className="side-head">
          <span>Sessions</span>
          <button className="icon-btn" onClick={onNewSession} disabled={!activeWorkspaceId} title="New session">
            <Icon name="plus" size={15} />
          </button>
        </div>
        <div className="side-list">
          {sessions.map((session) => (
            <button
              key={session.id}
              className={`side-item session ${session.id === activeSessionId ? 'active' : ''}`}
              onClick={() => onSelectSession(session.id)}
            >
              <span className={`status-dot ${session.status}`} />
              <span className="side-label">{session.title}</span>
              <span className="side-time">{relativeTime(session.updatedAt)}</span>
            </button>
          ))}
          {sessions.length === 0 && (
            <p className="side-empty">{activeWorkspaceId ? 'No sessions yet.' : 'Choose a workspace to start.'}</p>
          )}
        </div>
      </div>

      <div className="side-foot">
        <div className={`server-state ${health ? 'online' : 'offline'}`}>
          <span className="status-dot" />
          <span className="server-text">
            {health ? `${health.provider} · ${health.model}` : 'server offline'}
          </span>
        </div>
        <button className="icon-btn" onClick={onOpenSettings} title="Settings">
          <Icon name="settings" size={16} />
        </button>
      </div>
    </aside>
  )
}

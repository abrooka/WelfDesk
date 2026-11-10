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
  onNewWorktreeSession,
  canUseWorktree,
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
  onNewWorktreeSession: () => void
  canUseWorktree: boolean
  onOpenSettings: () => void
}) {
  return (
    <aside className="sidebar">
      <div className="brand">
        <svg className="brand-mark" viewBox="0 0 256 256" role="img" aria-label="WeftDesk">
          <defs>
            <linearGradient id="tile" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0" stopColor="#261A30" />
              <stop offset="0.55" stopColor="#1A1120" />
              <stop offset="1" stopColor="#120B16" />
            </linearGradient>
            <radialGradient id="glow" cx="0.5" cy="0.42" r="0.55">
              <stop offset="0" stopColor="#F2B25C" stopOpacity="0.14" />
              <stop offset="0.65" stopColor="#F2B25C" stopOpacity="0.04" />
              <stop offset="1" stopColor="#120B16" stopOpacity="0" />
            </radialGradient>
            <linearGradient id="weft" gradientUnits="userSpaceOnUse" x1="54" y1="86" x2="202" y2="174">
              <stop offset="0" stopColor="#F7C45F" />
              <stop offset="1" stopColor="#EE8353" />
            </linearGradient>
          </defs>
          <rect width="256" height="256" rx="58" fill="url(#tile)" />
          <rect width="256" height="256" rx="58" fill="url(#glow)" />
          <rect x="1" y="1" width="254" height="254" rx="57" fill="none" stroke="#FFFFFF" strokeOpacity="0.06" strokeWidth="2" />
          <path d="M54 86 L91 174 L128 110 L165 174 L202 86" fill="none" stroke="url(#weft)" strokeWidth="30" strokeLinecap="round" strokeLinejoin="round" />
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
          <div className="side-head-actions">
            <button
              className="icon-btn"
              onClick={onNewWorktreeSession}
              disabled={!activeWorkspaceId || !canUseWorktree}
              title="New worktree session"
            >
              <Icon name="branch" size={15} />
            </button>
            <button className="icon-btn" onClick={onNewSession} disabled={!activeWorkspaceId} title="New session">
              <Icon name="plus" size={15} />
            </button>
          </div>
        </div>
        <div className="side-list">
          {sessions.map((session) => (
            <button
              key={session.id}
              className={`side-item session ${session.id === activeSessionId ? 'active' : ''}`}
              onClick={() => onSelectSession(session.id)}
              title={session.worktree ? `${session.title} — ${session.worktree.branch}` : session.title}
            >
              <span className={`status-dot ${session.status}`} />
              <span className="side-label">{session.title}</span>
              {session.worktree && <Icon name="branch" size={11} />}
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

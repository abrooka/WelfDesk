import { Icon } from './Icon'
import type { Session } from '../types'

export function SessionTabs({
  sessions,
  tabIds,
  activeSessionId,
  onSelect,
  onClose,
}: {
  sessions: Session[]
  tabIds: string[]
  activeSessionId: string | null
  onSelect: (id: string) => void
  onClose: (id: string) => void
}) {
  const byId = new Map(sessions.map((session) => [session.id, session]))
  const tabs = tabIds.map((id) => byId.get(id)).filter((session): session is Session => Boolean(session))
  if (tabs.length === 0) return null

  return (
    <div className="session-tabs" role="tablist">
      {tabs.map((session) => {
        const active = session.id === activeSessionId
        return (
          <div
            key={session.id}
            role="tab"
            aria-selected={active}
            tabIndex={0}
            className={`session-tab ${active ? 'active' : ''}`}
            title={session.title}
            onClick={() => onSelect(session.id)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                onSelect(session.id)
              }
            }}
            onAuxClick={(event) => {
              if (event.button === 1) {
                event.preventDefault()
                onClose(session.id)
              }
            }}
          >
            <span className={`status-dot ${session.status}`} />
            <span className="session-tab-title">{session.title}</span>
            <button
              className="session-tab-close"
              title="Close tab"
              onClick={(event) => {
                event.stopPropagation()
                onClose(session.id)
              }}
            >
              <Icon name="close" size={11} />
            </button>
          </div>
        )
      })}
    </div>
  )
}

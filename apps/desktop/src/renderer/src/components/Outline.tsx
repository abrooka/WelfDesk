import { useMemo, useState } from 'react'
import { Icon } from './Icon'
import { firstLine, toolSummary } from '../lib/format'
import type {
  AssistantMessageData,
  PermissionRequestData,
  ToolCallData,
  UserMessageData,
  WeftEvent,
} from '../types'

interface Entry {
  id: string
  label: string
  meta?: string
}

export function Outline({ events }: { events: WeftEvent[] }) {
  const [query, setQuery] = useState('')
  const [openTools, setOpenTools] = useState(true)
  const [openApprovals, setOpenApprovals] = useState(true)
  const [openMessages, setOpenMessages] = useState(true)

  const sections = useMemo(() => {
    const messages: Entry[] = []
    const approvals: Entry[] = []
    const tools: Entry[] = []
    for (const event of events) {
      if (event.type === 'user.message') {
        const data = event.data as UserMessageData
        if (data?.text) messages.push({ id: event.id, label: firstLine(data.text, 80), meta: 'You' })
      } else if (event.type === 'assistant.message') {
        const data = event.data as AssistantMessageData
        if (data?.text) messages.push({ id: event.id, label: firstLine(data.text, 80), meta: 'Weft' })
      } else if (event.type === 'permission.request') {
        const data = event.data as PermissionRequestData
        approvals.push({
          id: event.id,
          label: `${data?.tool ?? 'tool'} · ${toolSummary(data?.tool ?? '', data?.input)}`,
          meta: 'Approval',
        })
      } else if (event.type === 'tool.call') {
        const data = event.data as ToolCallData
        tools.push({
          id: event.id,
          label: toolSummary(data?.name ?? '', data?.arguments),
          meta: data?.name,
        })
      }
    }
    return { messages, approvals, tools }
  }, [events])

  const filter = (list: Entry[]) =>
    query.trim() === ''
      ? list
      : list.filter((entry) => `${entry.meta ?? ''} ${entry.label}`.toLowerCase().includes(query.toLowerCase()))

  return (
    <div className="outline">
      <div className="panel-search">
        <Icon name="search" size={14} />
        <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Filter this session" />
      </div>
      <div className="outline-scroll">
        <Section
          title="Messages"
          entries={filter(sections.messages)}
          open={openMessages}
          onToggle={() => setOpenMessages((value) => !value)}
          icon="message"
        />
        <Section
          title="Approvals"
          entries={filter(sections.approvals)}
          open={openApprovals}
          onToggle={() => setOpenApprovals((value) => !value)}
          icon="alert"
          tone="accent"
        />
        <Section
          title="Tool calls"
          entries={filter(sections.tools)}
          open={openTools}
          onToggle={() => setOpenTools((value) => !value)}
          icon="terminal"
        />
        {!sections.messages.length && !sections.tools.length && !sections.approvals.length && (
          <p className="outline-empty">Messages and tool calls will appear here as the session grows.</p>
        )}
      </div>
    </div>
  )
}

function Section({
  title,
  entries,
  open,
  onToggle,
  icon,
  tone,
}: {
  title: string
  entries: Entry[]
  open: boolean
  onToggle: () => void
  icon: string
  tone?: 'accent'
}) {
  return (
    <div className="outline-section">
      <button className="outline-head" onClick={onToggle}>
        <Icon name={open ? 'chevronDown' : 'chevronRight'} size={13} />
        <Icon name={icon} size={13} className={tone === 'accent' ? 'accent-icon' : undefined} />
        <span>{title}</span>
        <span className="outline-count">{entries.length}</span>
      </button>
      {open && (
        <div className="outline-list">
          {entries.map((entry) => (
            <button key={entry.id} className="outline-item" onClick={() => jump(entry.id)}>
              <span className="outline-meta">{entry.meta}</span>
              <span className="outline-label">{entry.label}</span>
            </button>
          ))}
          {entries.length === 0 && <p className="outline-none">None</p>}
        </div>
      )}
    </div>
  )
}

function jump(id: string) {
  const element = document.getElementById(`ev-${id}`)
  if (!element) return
  element.scrollIntoView({ behavior: 'smooth', block: 'center' })
  element.classList.remove('flash')
  void element.offsetWidth
  element.classList.add('flash')
  window.setTimeout(() => element.classList.remove('flash'), 1300)
}

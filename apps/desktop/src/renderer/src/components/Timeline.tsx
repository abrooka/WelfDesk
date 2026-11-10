import { useEffect, useMemo, useRef, useState } from 'react'
import { DiffView } from './DiffView'
import { Icon } from './Icon'
import { Markdown } from './Markdown'
import { clockTime, duration, parseArgs, toolSummary } from '../lib/format'
import type { LiveText } from '../hooks/useSessionStream'
import type {
  AssistantMessageData,
  ErrorData,
  PermissionRequestData,
  PermissionResolvedData,
  ToolResultData,
  UserMessageData,
  WeftEvent,
} from '../types'

interface Props {
  events: WeftEvent[]
  live: LiveText | null
  running: boolean
  loading: boolean
  error: string | null
  onResolvePermission: (requestId: string, decision: 'allow' | 'deny') => Promise<void>
  onReveal: (path: string) => void
  onCopy: (text: string) => void
}

export function Timeline({ events, live, running, loading, error, onResolvePermission, onReveal, onCopy }: Props) {
  const scroll = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const [showJump, setShowJump] = useState(false)

  const toolResults = useMemo(() => {
    const map = new Map<string, { data: ToolResultData; eventId: string }>()
    for (const event of events) {
      if (event.type !== 'tool.result') continue
      const data = event.data as ToolResultData | undefined
      if (data?.toolCallId) map.set(data.toolCallId, { data, eventId: event.id })
    }
    return map
  }, [events])

  const permissions = useMemo(() => {
    const resolved = new Map<string, 'allow' | 'deny'>()
    for (const event of events) {
      if (event.type !== 'permission.resolved') continue
      const data = event.data as PermissionResolvedData | undefined
      if (data?.requestId) resolved.set(data.requestId, data.decision)
    }
    return resolved
  }, [events])

  const lastVisible = useMemo(() => {
    for (let i = events.length - 1; i >= 0; i--) {
      const event = events[i]
      if (event.type === 'user.message' || event.type === 'assistant.message' || event.type === 'error') return event
      if (event.type === 'tool.call' || event.type === 'permission.request') return event
    }
    return null
  }, [events])

  useEffect(() => {
    if (stick.current) scroll.current?.scrollTo({ top: scroll.current.scrollHeight })
  }, [events.length, live?.text, running])

  const onScroll = () => {
    const element = scroll.current
    if (!element) return
    stick.current = element.scrollHeight - element.scrollTop - element.clientHeight < 90
    setShowJump(!stick.current)
  }

  const jumpDown = () => {
    stick.current = true
    setShowJump(false)
    scroll.current?.scrollTo({ top: scroll.current.scrollHeight, behavior: 'smooth' })
  }

  return (
    <div className="timeline-scroll" ref={scroll} onScroll={onScroll}>
      <div className="timeline">
        {loading && (
          <div className="timeline-loading">
            <span />
            <span />
            <span />
          </div>
        )}
        {!loading && events.length === 0 && !live && (
          <div className="timeline-empty">
            <div className="empty-mark">
              <Icon name="spark" size={22} />
            </div>
            <h2>Ask Weft to work on your code</h2>
            <p>Describe a change, ask a question, or point it at a file. Tool calls and diffs will appear here.</p>
          </div>
        )}

        {events.map((event) => {
          switch (event.type) {
            case 'user.message': {
              const data = event.data as UserMessageData
              return (
                <div key={event.id} id={`ev-${event.id}`} className="timeline-item user-item">
                  <div className="user-bubble selectable">{data?.text}</div>
                  <span className="item-time">{clockTime(event.at)}</span>
                </div>
              )
            }
            case 'assistant.message': {
              const data = event.data as AssistantMessageData
              if (!data?.text) return null
              return (
                <div key={event.id} id={`ev-${event.id}`} className="timeline-item assistant-item">
                  <AssistantAvatar />
                  <Markdown text={data.text} />
                </div>
              )
            }
            case 'tool.call': {
              const data = event.data as { id: string; name: string; arguments: string }
              if (!data) return null
              const result = toolResults.get(data.id)
              return (
                <div key={event.id} id={`ev-${event.id}`} className="timeline-item tool-item">
                  <ToolCard
                    name={data.name}
                    args={data.arguments}
                    result={result?.data}
                    resultId={result?.eventId}
                    onReveal={onReveal}
                    onCopy={onCopy}
                  />
                </div>
              )
            }
            case 'permission.request': {
              const data = event.data as PermissionRequestData
              if (!data) return null
              const decision = permissions.get(data.requestId)
              return (
                <div key={event.id} id={`ev-${event.id}`} className="timeline-item">
                  <PermissionCard
                    data={data}
                    decision={decision}
                    onResolve={onResolvePermission}
                  />
                </div>
              )
            }
            case 'error': {
              const data = event.data as ErrorData
              return (
                <div key={event.id} id={`ev-${event.id}`} className="timeline-item">
                  <div className="error-card">
                    <Icon name="alert" size={15} />
                    <span className="selectable">{data?.message ?? 'Something went wrong'}</span>
                  </div>
                </div>
              )
            }
            default:
              return null
          }
        })}

        {live && (
          <div className="timeline-item assistant-item live-item">
            <AssistantAvatar />
            <div className="live-text selectable">{live.text}<span className="caret" /></div>
          </div>
        )}

        {running && !live && (
          <div className="timeline-item running-item">
            <AssistantAvatar />
            <span className="thinking">
              <i />
              <i />
              <i />
            </span>
            <span className="thinking-label">Working…</span>
          </div>
        )}

        {error && (
          <div className="timeline-item">
            <div className="error-card">
              <Icon name="alert" size={15} />
              <span>{error}</span>
            </div>
          </div>
        )}

        {!running && lastVisible?.type === 'error' && <div className="timeline-tail">Stopped</div>}
        <div className="timeline-bottom" />
      </div>

      {showJump && (
        <button className="jump-btn" onClick={jumpDown} title="Jump to latest">
          <Icon name="chevronDown" size={16} />
        </button>
      )}
    </div>
  )
}

function AssistantAvatar() {
  return (
    <div className="assistant-avatar">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M4 9c4-3.6 12 3.6 16 0" />
        <path d="M4 15c4-3.6 12 3.6 16 0" />
      </svg>
    </div>
  )
}

function toolIcon(name: string): string {
  switch (name) {
    case 'read_file':
      return 'eye'
    case 'write_file':
      return 'filePlus'
    case 'edit_file':
      return 'filePen'
    case 'bash':
      return 'terminal'
    case 'grep':
      return 'search'
    case 'glob':
      return 'layers'
    default:
      return 'file'
  }
}

function ToolCard({
  name,
  args,
  result,
  resultId,
  onReveal,
  onCopy,
}: {
  name: string
  args: string
  result?: ToolResultData
  resultId?: string
  onReveal: (path: string) => void
  onCopy: (text: string) => void
}) {
  const [open, setOpen] = useState(false)
  const auto = useRef(true)
  const parsed = parseArgs(args)
  const summary = toolSummary(name, args)
  const path = result?.path || (parsed && typeof parsed.path === 'string' ? parsed.path : '')
  const [showArgs, setShowArgs] = useState(false)
  const [showOutput, setShowOutput] = useState(true)

  useEffect(() => {
    if (auto.current && (result?.diff || result?.isError)) setOpen(true)
  }, [result?.diff, result?.isError])

  const state = !result ? 'running' : result.isError ? 'error' : 'ok'

  return (
    <div className={`tool-card ${open ? 'open' : ''}`} id={resultId ? `ev-${resultId}` : undefined}>
      <button
        className="tool-head"
        onClick={() => {
          auto.current = false
          setOpen((value) => !value)
        }}
      >
        <span className="tool-icon">
          <Icon name={toolIcon(name)} size={14} />
        </span>
        <span className="tool-name">{name}</span>
        <span className="tool-summary">{summary}</span>
        {result && <span className="tool-duration">{duration(result.durationMs)}</span>}
        <span className={`tool-state ${state}`}>
          {state === 'running' ? <span className="spinner" /> : state === 'ok' ? <Icon name="check" size={13} /> : <Icon name="close" size={13} />}
        </span>
        <span className={`chev ${open ? 'open' : ''}`}>
          <Icon name="chevronRight" size={14} />
        </span>
      </button>

      {open && (
        <div className="tool-body">
          {result?.diff ? (
            <div className="tool-section">
              <div className="tool-section-label">Diff</div>
              <DiffView diff={result.diff} />
            </div>
          ) : null}

          {!result?.diff && parsed && (
            <div className="tool-section">
              <div className="tool-section-label">
                Arguments
                <button className="mini-btn" onClick={() => setShowArgs((value) => !value)}>
                  {showArgs ? 'Hide' : 'JSON'}
                </button>
              </div>
              {showArgs ? (
                <pre className="tool-pre selectable">{JSON.stringify(parsed, null, 2)}</pre>
              ) : (
                <div className="tool-arg-line selectable">
                  {typeof parsed.command === 'string'
                    ? parsed.command
                    : typeof parsed.pattern === 'string'
                      ? parsed.pattern
                      : summary}
                </div>
              )}
            </div>
          )}

          <div className="tool-section">
            <div className="tool-section-label">
              Output
              <button className="mini-btn" onClick={() => setShowOutput((value) => !value)}>
                {showOutput ? 'Hide' : 'Show'}
              </button>
            </div>
            {showOutput &&
              (result ? (
                <pre className={`tool-pre selectable ${result.isError ? 'error' : ''}`}>{result.output || '(no output)'}</pre>
              ) : (
                <div className="tool-pending">Waiting for the runner…</div>
              ))}
          </div>

          <div className="tool-actions">
            {path && (
              <button className="link-btn" onClick={() => onReveal(path)}>
                <Icon name="reveal" size={13} /> Reveal
              </button>
            )}
            {result?.output && (
              <button className="link-btn" onClick={() => onCopy(result.output)}>
                <Icon name="copy" size={13} /> Copy output
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function PermissionCard({
  data,
  decision,
  onResolve,
}: {
  data: PermissionRequestData
  decision?: 'allow' | 'deny'
  onResolve: (requestId: string, decision: 'allow' | 'deny') => Promise<void>
}) {
  const [busy, setBusy] = useState<'allow' | 'deny' | null>(null)
  const [details, setDetails] = useState(false)
  const parsed = parseArgs(data.input)
  const summary = toolSummary(data.tool, data.input)

  const resolve = async (choice: 'allow' | 'deny') => {
    setBusy(choice)
    try {
      await onResolve(data.requestId, choice)
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className={`permission-card ${decision ?? 'pending'}`}>
      <div className="permission-head">
        <span className="permission-icon">
          <Icon name="alert" size={15} />
        </span>
        <div className="permission-title">
          <strong>{decision ? (decision === 'allow' ? 'Approved' : 'Denied') : 'Approval required'}</strong>
          <span>
            {data.tool} · {summary}
          </span>
        </div>
        {decision && (
          <span className={`decision-chip ${decision}`}>
            {decision === 'allow' ? <Icon name="check" size={12} /> : <Icon name="close" size={12} />}
            {decision}
          </span>
        )}
      </div>

      {!decision && (
        <>
          <p className="permission-reason">{data.reason}</p>
          {parsed && (
            <button className="details-toggle" onClick={() => setDetails((value) => !value)}>
              <Icon name={details ? 'chevronDown' : 'chevronRight'} size={13} />
              {details ? 'Hide details' : 'Show details'}
            </button>
          )}
          {details && parsed && <pre className="tool-pre selectable">{JSON.stringify(parsed, null, 2)}</pre>}
          <div className="permission-actions">
            <button className="btn ghost" disabled={busy !== null} onClick={() => resolve('deny')}>
              Deny
            </button>
            <button className="btn primary" disabled={busy !== null} onClick={() => resolve('allow')}>
              {busy === 'allow' ? 'Allowing…' : 'Allow once'}
            </button>
          </div>
        </>
      )}
    </div>
  )
}

import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { Icon } from './Icon'
import { clockTime, duration } from '../lib/format'
import type { RequestRecord } from '../types'

export function RequestsPanel({ sessionId, revision }: { sessionId: string; revision: number }) {
  const [records, setRecords] = useState<RequestRecord[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [openIndex, setOpenIndex] = useState<number | null>(null)
  const [tab, setTab] = useState<'request' | 'response'>('request')

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.requests(sessionId)
      setRecords([...data.requests].reverse())
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load requests')
    } finally {
      setLoading(false)
    }
  }, [sessionId])

  useEffect(() => {
    void load()
  }, [load, revision])

  return (
    <div className="requests">
      <div className="panel-head">
        <span>{records.length} model calls</span>
        <button className="icon-btn" onClick={() => void load()} title="Refresh">
          <Icon name="refresh" size={14} />
        </button>
      </div>
      <div className="requests-scroll">
        {loading && records.length === 0 && <p className="panel-note">Loading request log…</p>}
        {error && <p className="panel-note error">{error}</p>}
        {!loading && records.length === 0 && !error && (
          <p className="panel-note">Every model request will be recorded here, like a network tab.</p>
        )}
        {records.map((record, index) => {
          const open = openIndex === index
          const usage = record.result?.usage
          return (
            <div key={record.started.requestId} className={`request-row ${open ? 'open' : ''}`}>
              <button
                className="request-head"
                onClick={() => {
                  setOpenIndex(open ? null : index)
                  setTab('request')
                }}
              >
                <span className="request-index">#{records.length - index}</span>
                <span className="request-main">
                  <span className="request-model">
                    {record.started.provider} · {record.started.model}
                  </span>
                  <span className="request-meta">
                    {clockTime(record.at)}
                    {record.result ? ` · ${duration(record.result.durationMs)}` : ' · pending'}
                    {usage ? ` · ${usage.inputTokens}→${usage.outputTokens} tok` : ''}
                  </span>
                </span>
                {record.result?.error && <span className="error-chip">error</span>}
                <Icon name={open ? 'chevronDown' : 'chevronRight'} size={13} />
              </button>
              {open && (
                <div className="request-body">
                  <div className="segmented small">
                    <button className={tab === 'request' ? 'active' : ''} onClick={() => setTab('request')}>
                      Request
                    </button>
                    <button className={tab === 'response' ? 'active' : ''} onClick={() => setTab('response')}>
                      Response
                    </button>
                  </div>
                  <div className="request-stats">
                    <span>{record.started.messages.length} messages</span>
                    <span>{record.started.tools.length} tools</span>
                    <button
                      className="mini-btn"
                      onClick={() =>
                        void window.weft?.copyText(
                          JSON.stringify(tab === 'request' ? record.started : record.result ?? {}, null, 2),
                        )
                      }
                    >
                      <Icon name="copy" size={12} /> Copy
                    </button>
                  </div>
                  <pre className="json-pre selectable">
                    {JSON.stringify(tab === 'request' ? record.started : record.result ?? {}, null, 2)}
                  </pre>
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

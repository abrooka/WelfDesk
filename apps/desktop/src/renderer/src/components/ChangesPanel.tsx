import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { DiffView } from './DiffView'
import { Icon } from './Icon'
import { Modal } from './Modal'
import { clockTime, fileBaseName, parentPath, relativePath, relativeTime, withinPath } from '../lib/format'
import type { Checkpoint, Folder } from '../types'

interface Props {
  sessionId: string
  folders: Folder[]
  running: boolean
  revision: number
  onNotify: (text: string, tone?: 'info' | 'error') => void
}

interface RunGroup {
  runId: string
  ordinal: number
  items: Checkpoint[]
}

type Pending = { kind: 'file'; cp: Checkpoint } | { kind: 'run'; runId: string; items: Checkpoint[] }

export function ChangesPanel({ sessionId, folders, running, revision, onNotify }: Props) {
  const [items, setItems] = useState<Checkpoint[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const [openFiles, setOpenFiles] = useState<Set<string>>(new Set())
  const [pending, setPending] = useState<Pending | null>(null)
  const [force, setForce] = useState(false)
  const [busy, setBusy] = useState(false)
  const [modalError, setModalError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.checkpoints(sessionId)
      setItems(data.checkpoints)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load changes')
    } finally {
      setLoading(false)
    }
  }, [sessionId])

  useEffect(() => {
    void load()
  }, [load, revision])

  const groups = useMemo<RunGroup[]>(() => {
    const map = new Map<string, Checkpoint[]>()
    for (const cp of items) {
      const list = map.get(cp.runId) ?? []
      list.push(cp)
      map.set(cp.runId, list)
    }
    return [...map.entries()]
      .map(([runId, list], index) => ({ runId, ordinal: index + 1, items: list }))
      .reverse()
  }, [items])

  const activeCount = items.filter((cp) => !cp.revertedAt).length

  const shownPath = (path: string) => {
    const folder = folders.find((item) => withinPath(item.path, path))
    return folder ? relativePath(folder.path, path) : path
  }

  const toggleRun = (runId: string) => {
    setCollapsed((current) => {
      const next = new Set(current)
      if (next.has(runId)) next.delete(runId)
      else next.add(runId)
      return next
    })
  }

  const toggleFile = (id: string) => {
    setOpenFiles((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const ask = (next: Pending) => {
    setForce(false)
    setModalError(null)
    setPending(next)
  }

  const confirm = async () => {
    if (!pending) return
    setBusy(true)
    setModalError(null)
    try {
      const data =
        pending.kind === 'file'
          ? await api.revertCheckpoint(sessionId, pending.cp.id, force)
          : await api.revertRun(sessionId, pending.runId, force)
      const count = data.reverted.length
      onNotify(`Reverted ${count} file${count === 1 ? '' : 's'}`)
      setPending(null)
      setForce(false)
      await load()
    } catch (cause) {
      setModalError(cause instanceof Error ? cause.message : 'Revert failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="changes">
      <div className="panel-head">
        <span>
          {activeCount} change{activeCount === 1 ? '' : 's'}
        </span>
        <button className="icon-btn" onClick={() => void load()} title="Refresh">
          <Icon name="refresh" size={14} />
        </button>
      </div>

      <div className="changes-scroll">
        {loading && items.length === 0 && <p className="panel-note">Loading changes…</p>}
        {error && <p className="panel-note error">{error}</p>}
        {!loading && !error && items.length === 0 && (
          <p className="panel-note">
            Edits made by write_file and edit_file are recorded here so you can undo them. Shell commands and
            changes made outside Weft are not tracked.
          </p>
        )}

        {groups.map((group) => {
          const open = !collapsed.has(group.runId)
          const active = group.items.filter((cp) => !cp.revertedAt)
          const allReverted = active.length === 0
          return (
            <div key={group.runId} className="change-run">
              <div className="change-run-head">
                <button className="change-run-toggle" onClick={() => toggleRun(group.runId)}>
                  <span className={`chev ${open ? 'open' : ''}`}>
                    <Icon name="chevronRight" size={13} />
                  </span>
                  <span className="change-run-title">Run {group.ordinal}</span>
                  <span className="change-run-meta">
                    {group.items.length} file{group.items.length === 1 ? '' : 's'} ·{' '}
                    {relativeTime(group.items[group.items.length - 1].createdAt)}
                  </span>
                  {allReverted && <span className="change-chip">Reverted</span>}
                </button>
                {!allReverted && (
                  <button
                    className="link-btn change-run-revert"
                    disabled={running}
                    title={running ? 'Wait for the run to finish' : 'Revert every file changed in this run'}
                    onClick={() => ask({ kind: 'run', runId: group.runId, items: active })}
                  >
                    <Icon name="undo" size={12} /> Revert all
                  </button>
                )}
              </div>

              {open && (
                <div className="change-run-body">
                  {group.items.map((cp) => {
                    const shown = shownPath(cp.path)
                    const name = fileBaseName(shown)
                    const dir = parentPath(shown)
                    const reverted = !!cp.revertedAt
                    const expanded = openFiles.has(cp.id)
                    return (
                      <div key={cp.id} className={`change-file ${reverted ? 'reverted' : ''}`}>
                        <div className="change-file-head">
                          <button className="change-file-toggle" onClick={() => toggleFile(cp.id)}>
                            <Icon name={expanded ? 'chevronDown' : 'chevronRight'} size={11} />
                            <span className="change-file-icon">
                              <Icon name={cp.tool === 'write_file' ? 'filePlus' : 'filePen'} size={13} />
                            </span>
                            <span className="change-file-main">
                              <span className="change-file-name">{name}</span>
                              {dir && <span className="change-file-dir">{dir}</span>}
                            </span>
                            {reverted && <span className="change-chip">Reverted</span>}
                            <span className="change-file-time">{clockTime(cp.createdAt)}</span>
                          </button>
                          {!reverted && (
                            <button
                              className="icon-btn change-file-revert"
                              disabled={running}
                              title={running ? 'Wait for the run to finish' : `Revert ${name}`}
                              onClick={() => ask({ kind: 'file', cp })}
                            >
                              <Icon name="undo" size={13} />
                            </button>
                          )}
                        </div>
                        {expanded && (
                          <div className="change-file-body">
                            {cp.diff ? (
                              <DiffView diff={cp.diff} />
                            ) : (
                              <p className="change-none">No textual diff was recorded for this change.</p>
                            )}
                          </div>
                        )}
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {pending && (
        <Modal
          title={pending.kind === 'file' ? 'Revert file change' : 'Revert run changes'}
          subtitle={
            pending.kind === 'file'
              ? shownPath(pending.cp.path)
              : `${pending.items.length} file${pending.items.length === 1 ? '' : 's'} changed in this run`
          }
          onClose={() => {
            if (!busy) setPending(null)
          }}
          footer={
            <>
              <button className="btn ghost" disabled={busy} onClick={() => setPending(null)}>
                Cancel
              </button>
              <button className="btn danger" disabled={busy} onClick={() => void confirm()}>
                {busy ? 'Reverting…' : 'Revert'}
              </button>
            </>
          }
        >
          <div className="revert-body">
            <p>
              {pending.kind === 'file'
                ? `This restores ${fileBaseName(shownPath(pending.cp.path))} to the state before this change.`
                : 'Every file edited in this run is restored to the state from before the run started.'}
            </p>
            {pending.kind === 'run' && (
              <ul className="revert-list">
                {pending.items.map((cp) => (
                  <li key={cp.id}>{shownPath(cp.path)}</li>
                ))}
              </ul>
            )}
            <label className="revert-force">
              <input type="checkbox" checked={force} onChange={(event) => setForce(event.target.checked)} />
              <span>
                <strong>Overwrite newer changes</strong>
                <span>Skip the safety check when a file was edited outside Weft after this change.</span>
              </span>
            </label>
            {modalError && <p className="revert-error">{modalError}</p>}
          </div>
        </Modal>
      )}
    </div>
  )
}

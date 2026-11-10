import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { Icon } from './Icon'
import { FilePreview } from './FilePreview'
import { fileBaseName, parentPath, relativePath } from '../lib/format'
import type { FileEntry, Folder } from '../types'

export function FilesPanel({
  sessionId,
  folders,
  onNotify,
}: {
  sessionId: string
  folders: Folder[]
  onNotify: (text: string, tone?: 'info' | 'error') => void
}) {
  const [root, setRoot] = useState(folders[0]?.path ?? '')
  const [path, setPath] = useState(folders[0]?.path ?? '')
  const [entries, setEntries] = useState<FileEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [preview, setPreview] = useState<{ path: string; content: string; truncated: boolean } | null>(null)

  const load = useCallback(
    async (target: string) => {
      setLoading(true)
      setError(null)
      try {
        const data = await api.files(sessionId, target)
        setEntries(data.entries)
        setPath(data.path)
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : 'Failed to read directory')
      } finally {
        setLoading(false)
      }
    },
    [sessionId],
  )

  useEffect(() => {
    const start = folders[0]?.path ?? ''
    setRoot(start)
    setPath(start)
    setEntries([])
    if (start) void load(start)
  }, [folders, load])

  const openEntry = async (entry: FileEntry) => {
    if (entry.isDir) {
      void load(entry.path)
      return
    }
    try {
      const data = await api.file(sessionId, entry.path)
      setPreview({ path: data.path, content: data.content, truncated: data.truncated })
    } catch (cause) {
      onNotify(cause instanceof Error ? cause.message : 'Failed to open file', 'error')
    }
  }

  const crumbs = useMemo(() => {
    const relative = relativePath(root, path)
    if (relative === '.') return []
    return relative.split(/[\\/]/).filter(Boolean)
  }, [root, path])

  const parent = parentPath(path)

  return (
    <div className="files">
      <div className="panel-head">
        <span>{fileBaseName(root) || 'Workspace'}</span>
        <div className="panel-actions">
          {folders.length > 1 && (
            <select
              className="mini-select"
              value={root}
              onChange={(event) => {
                const next = event.target.value
                setRoot(next)
                setPath(next)
                void load(next)
              }}
            >
              {folders.map((folder) => (
                <option key={folder.id} value={folder.path}>
                  {folder.name}
                </option>
              ))}
            </select>
          )}
          <button className="icon-btn" onClick={() => void load(path)} title="Refresh">
            <Icon name="refresh" size={14} />
          </button>
        </div>
      </div>

      <div className="breadcrumbs">
        <button className="crumb" onClick={() => void load(root)}>
          {fileBaseName(root)}
        </button>
        {crumbs.map((crumb, index) => {
          const target = `${root}/${crumbs.slice(0, index + 1).join('/')}`
          return (
            <span key={target} className="crumb-wrap">
              <Icon name="chevronRight" size={11} />
              <button className="crumb" onClick={() => void load(target)}>
                {crumb}
              </button>
            </span>
          )
        })}
      </div>

      <div className="files-scroll">
        {loading && <p className="panel-note">Loading…</p>}
        {error && <p className="panel-note error">{error}</p>}
        {!loading && !error && crumbs.length > 0 && (
          <button className="file-row parent" onClick={() => void load(parent)}>
            <Icon name="folder" size={14} />
            <span>..</span>
          </button>
        )}
        {!loading &&
          !error &&
          entries.map((entry) => (
            <button key={entry.path} className={`file-row ${entry.isDir ? 'dir' : ''}`} onClick={() => void openEntry(entry)}>
              <Icon name={entry.isDir ? 'folder' : 'file'} size={14} />
              <span>{entry.name}</span>
            </button>
          ))}
        {!loading && !error && entries.length === 0 && <p className="panel-note">This folder is empty.</p>}
      </div>

      {preview && (
        <FilePreview
          file={preview}
          onClose={() => setPreview(null)}
          onNotify={onNotify}
        />
      )}
    </div>
  )
}

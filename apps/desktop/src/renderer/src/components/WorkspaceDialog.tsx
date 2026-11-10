import { useState } from 'react'
import { api } from '../api'
import { Icon } from './Icon'
import { Modal } from './Modal'
import { fileBaseName } from '../lib/format'
import type { Workspace } from '../types'

export function WorkspaceDialog({
  onClose,
  onCreated,
  onNotify,
}: {
  onClose: () => void
  onCreated: (workspace: Workspace) => void
  onNotify: (text: string, tone?: 'info' | 'error') => void
}) {
  const [name, setName] = useState('')
  const [folders, setFolders] = useState<Array<{ name: string; path: string }>>([])
  const [manual, setManual] = useState('')
  const [busy, setBusy] = useState(false)

  const add = (path: string) => {
    const value = path.trim()
    if (!value) return
    setFolders((current) => {
      if (current.some((folder) => folder.path.toLowerCase() === value.toLowerCase())) return current
      return [...current, { path: value, name: fileBaseName(value) }]
    })
    setManual('')
  }

  const browse = async () => {
    if (!window.weft) {
      onNotify('Folder browsing is available in the desktop app; paste a path below.', 'info')
      return
    }
    try {
      const picked = await window.weft.pickFolders()
      for (const folder of picked) add(folder.path)
    } catch (cause) {
      onNotify(cause instanceof Error ? cause.message : 'Failed to open folder picker', 'error')
    }
  }

  const create = async () => {
    if (folders.length === 0) return
    setBusy(true)
    try {
      const workspace = await api.createWorkspace(name.trim(), folders.map((folder) => folder.path))
      onCreated(workspace)
      onNotify(`Workspace "${workspace.name}" created`)
      onClose()
    } catch (cause) {
      onNotify(cause instanceof Error ? cause.message : 'Failed to create workspace', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title="New workspace"
      subtitle="A workspace can span several folders on this machine."
      onClose={onClose}
      footer={
        <>
          <button className="btn ghost" onClick={onClose}>Cancel</button>
          <button className="btn primary" onClick={() => void create()} disabled={folders.length === 0 || busy}>
            {busy ? 'Creating…' : 'Create workspace'}
          </button>
        </>
      }
    >
      <label className="field">
        <span>Name</span>
        <input
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder={folders[0] ? fileBaseName(folders[0].path) : 'My project'}
          autoFocus
        />
      </label>

      <div className="folder-picker">
        <div className="folder-picker-head">
          <span>Folders</span>
          <button className="link-btn" onClick={() => void browse()}>
            <Icon name="folderPlus" size={14} /> Browse…
          </button>
        </div>
        {folders.map((folder) => (
          <div key={folder.path} className="folder-row">
            <Icon name="folder" size={14} />
            <span className="folder-name">{folder.name}</span>
            <code className="folder-path">{folder.path}</code>
            <button
              className="icon-btn"
              onClick={() => setFolders((current) => current.filter((item) => item.path !== folder.path))}
              title="Remove"
            >
              <Icon name="close" size={13} />
            </button>
          </div>
        ))}
        {folders.length === 0 && <p className="panel-note">Add at least one folder to begin.</p>}
      </div>

      <div className="manual-add">
        <input
          value={manual}
          onChange={(event) => setManual(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              add(manual)
            }
          }}
          placeholder="Or paste an absolute folder path"
          spellCheck={false}
        />
        <button className="btn ghost" onClick={() => add(manual)} disabled={!manual.trim()}>
          Add
        </button>
      </div>
    </Modal>
  )
}

import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { api } from '../api'
import { Icon } from './Icon'
import type { IndexedFile } from '../types'

function parts(file: IndexedFile) {
  const index = file.rel.lastIndexOf('/')
  if (index < 0) return { name: file.rel, dir: '' }
  return { name: file.rel.slice(index + 1), dir: file.rel.slice(0, index) }
}

export function QuickOpen({
  sessionId,
  onClose,
  onPick,
}: {
  sessionId: string
  onClose: () => void
  onPick: (file: IndexedFile) => void
}) {
  const [query, setQuery] = useState('')
  const [files, setFiles] = useState<IndexedFile[]>([])
  const [active, setActive] = useState(0)
  const [loading, setLoading] = useState(true)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    const timer = window.setTimeout(() => {
      api
        .searchFiles(sessionId, query)
        .then((data) => {
          if (cancelled) return
          setFiles(data.files)
          setActive(0)
          setLoading(false)
        })
        .catch(() => {
          if (cancelled) return
          setFiles([])
          setLoading(false)
        })
    }, 100)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [sessionId, query])

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
      return
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActive((index) => Math.min(index + 1, Math.max(files.length - 1, 0)))
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActive((index) => Math.max(index - 1, 0))
      return
    }
    if (event.key === 'Enter' && files[active]) {
      event.preventDefault()
      onPick(files[active])
    }
  }

  return (
    <div
      className="quick-open-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <div className="quick-open" role="dialog" aria-modal="true" aria-label="Quick open file">
        <div className="quick-open-input">
          <Icon name="search" size={15} />
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Search files by name"
            spellCheck={false}
          />
          <kbd>Esc</kbd>
        </div>
        <div className="quick-open-list">
          {loading && files.length === 0 && <p className="quick-open-note">Searching…</p>}
          {!loading && files.length === 0 && <p className="quick-open-note">No matching files.</p>}
          {files.map((file, index) => {
            const { name, dir } = parts(file)
            return (
              <button
                key={file.path}
                className={`quick-open-row ${index === active ? 'active' : ''}`}
                onMouseEnter={() => setActive(index)}
                onClick={() => onPick(file)}
                title={file.path}
              >
                <Icon name="file" size={13} />
                <span className="quick-open-name">{name}</span>
                {dir && <span className="quick-open-dir">{dir}</span>}
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

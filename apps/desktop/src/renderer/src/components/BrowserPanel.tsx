import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'

const STORAGE_PREFIX = 'weft.browser.'

function normalize(input: string): { url?: string; error?: string } {
  const value = input.trim()
  if (!value) return { error: 'Enter a URL to preview.' }
  const withScheme = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(value) ? value : `http://${value}`
  try {
    const parsed = new URL(withScheme)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      return { error: 'Only http and https URLs can be previewed.' }
    }
    return { url: parsed.toString() }
  } catch {
    return { error: 'That does not look like a valid URL.' }
  }
}

export function BrowserPanel({
  workspaceId,
  onClose,
  onNotify,
}: {
  workspaceId: string
  onClose: () => void
  onNotify: (text: string, tone?: 'info' | 'error') => void
}) {
  const storageKey = STORAGE_PREFIX + workspaceId
  const [url, setUrl] = useState(() => localStorage.getItem(storageKey) ?? '')
  const [draft, setDraft] = useState(url)
  const [reloadKey, setReloadKey] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!url) inputRef.current?.focus()
  }, [url])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const go = (input: string) => {
    const result = normalize(input)
    if (!result.url) {
      setError(result.error ?? 'Invalid URL.')
      return
    }
    setError(null)
    setDraft(result.url)
    localStorage.setItem(storageKey, result.url)
    if (result.url === url) setReloadKey((value) => value + 1)
    setUrl(result.url)
  }

  const openExternal = async () => {
    if (!url) return
    const bridge = window.weft
    if (!bridge) {
      window.open(url, '_blank', 'noopener')
      return
    }
    try {
      await bridge.openWith('default', url)
    } catch (cause) {
      onNotify(cause instanceof Error ? cause.message : 'Failed to open the browser', 'error')
    }
  }

  return (
    <div
      className="browser-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <div className="browser-window" role="dialog" aria-modal="true" aria-label="Browser preview">
        <header className="browser-bar">
          <form
            className="browser-address"
            onSubmit={(event) => {
              event.preventDefault()
              go(draft)
            }}
          >
            <Icon name="globe" size={14} />
            <input
              ref={inputRef}
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              placeholder="http://localhost:5173"
              spellCheck={false}
            />
          </form>
          <button
            className="icon-btn"
            onClick={() => setReloadKey((value) => value + 1)}
            disabled={!url}
            title="Reload"
          >
            <Icon name="refresh" size={14} />
          </button>
          <button className="icon-btn" onClick={() => void openExternal()} disabled={!url} title="Open in browser">
            <Icon name="external" size={14} />
          </button>
          <button className="icon-btn" onClick={onClose} title="Close">
            <Icon name="close" size={14} />
          </button>
        </header>
        {error && <p className="browser-error">{error}</p>}
        <div className="browser-body">
          {url ? (
            <iframe
              key={`${url}:${reloadKey}`}
              className="browser-frame"
              src={url}
              title="Browser preview"
              referrerPolicy="no-referrer"
            />
          ) : (
            <div className="browser-empty">
              <p className="panel-note">
                Preview a running dev server by entering its URL, for example http://localhost:5173.
              </p>
              <p className="panel-note">
                Sites that send X-Frame-Options or CSP frame-ancestors refuse to load here; use Open in browser instead.
              </p>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export function relativeTime(iso: string): string {
  const time = new Date(iso).getTime()
  if (!Number.isFinite(time)) return ''
  const diff = Date.now() - time
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  if (diff < 7 * 86_400_000) return `${Math.floor(diff / 86_400_000)}d ago`
  return new Date(iso).toLocaleDateString()
}

export function clockTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export function duration(ms: number | undefined): string {
  if (ms == null || ms < 0) return ''
  if (ms < 1) return '<1ms'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export function firstLine(text: string, max = 90): string {
  const line = text.split('\n').find((part) => part.trim() !== '') ?? ''
  return line.length > max ? `${line.slice(0, max)}…` : line
}

export function parseArgs(raw: string | undefined): Record<string, unknown> | null {
  if (!raw) return null
  try {
    const value = JSON.parse(raw)
    return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : null
  } catch {
    return null
  }
}

export function toolSummary(name: string, raw: string | undefined): string {
  const args = parseArgs(raw)
  if (!args) return firstLine(raw ?? '', 80)
  const pick = (key: string) => (typeof args[key] === 'string' ? (args[key] as string) : '')
  switch (name) {
    case 'read_file':
    case 'write_file':
    case 'edit_file':
      return pick('path') || 'file'
    case 'bash':
      return firstLine(pick('command'), 70)
    case 'grep':
      return pick('pattern') ? `/${pick('pattern')}/` : 'search'
    case 'glob':
      return pick('pattern')
    default:
      return firstLine(raw ?? '', 70)
  }
}

export function prettyJSON(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function fileBaseName(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() ?? path
}

export function parentPath(path: string): string {
  const normalized = path.replace(/[\\/]+$/, '')
  const index = Math.max(normalized.lastIndexOf('/'), normalized.lastIndexOf('\\'))
  return index <= 0 ? normalized : normalized.slice(0, index)
}

export function withinPath(root: string, path: string): boolean {
  const norm = (value: string) => value.replace(/\\/g, '/').toLowerCase()
  const r = norm(root).replace(/\/$/, '')
  const p = norm(path)
  return p === r || p.startsWith(`${r}/`)
}

export function relativePath(root: string, path: string): string {
  if (!withinPath(root, path)) return path
  const value = path.slice(root.length).replace(/^[\\/]/, '')
  return value === '' ? '.' : value
}

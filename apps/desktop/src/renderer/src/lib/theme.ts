export type ThemeId = 'ember' | 'graphite' | 'moss' | 'dusk' | 'parchment'

export interface ThemeMeta {
  id: ThemeId
  label: string
  description: string
  swatch: { bg: string; panel: string; line: string; accent: string }
}

export const THEMES: ThemeMeta[] = [
  {
    id: 'ember',
    label: 'Ember',
    description: 'Warm amber on charcoal.',
    swatch: { bg: '#0c0e0f', panel: '#1a1f20', line: '#333a3b', accent: '#d7a86e' },
  },
  {
    id: 'graphite',
    label: 'Graphite',
    description: 'Steel blue on slate.',
    swatch: { bg: '#0b0d10', panel: '#181c21', line: '#323945', accent: '#7aa7d9' },
  },
  {
    id: 'moss',
    label: 'Moss',
    description: 'Muted green on forest shadow.',
    swatch: { bg: '#0b0f0c', panel: '#171f19', line: '#344034', accent: '#9bc98f' },
  },
  {
    id: 'dusk',
    label: 'Dusk',
    description: 'Soft violet on deep indigo.',
    swatch: { bg: '#0e0c12', panel: '#1a1621', line: '#383147', accent: '#b79ae0' },
  },
  {
    id: 'parchment',
    label: 'Parchment',
    description: 'Warm paper with ink-brown accents.',
    swatch: { bg: '#f4f1ea', panel: '#f1ede4', line: '#c9c0ad', accent: '#a8672f' },
  },
]

const STORAGE_KEY = 'weft.theme'
const DEFAULT_THEME: ThemeId = 'ember'

function isThemeId(value: string | null | undefined): value is ThemeId {
  return !!value && THEMES.some((theme) => theme.id === value)
}

export function storedTheme(): ThemeId {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (isThemeId(saved)) return saved
  } catch {
    // storage may be unavailable in private browsing
  }
  return DEFAULT_THEME
}

export function currentTheme(): ThemeId {
  const applied = document.documentElement.dataset.theme
  return isThemeId(applied) ? applied : storedTheme()
}

export function applyTheme(id: ThemeId): void {
  document.documentElement.dataset.theme = id
  try {
    localStorage.setItem(STORAGE_KEY, id)
  } catch {
    // storage may be unavailable in private browsing
  }
}

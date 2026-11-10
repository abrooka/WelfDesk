import { useEffect, useState } from 'react'
import { api } from '../api'
import { THEMES, type ThemeId } from '../lib/theme'
import { Icon } from './Icon'
import { Modal } from './Modal'
import type { Config, Health } from '../types'

const PERMISSION_MODES = [
  {
    value: 'ask',
    title: 'Ask',
    body: 'Read-only tools run automatically. Edits and commands wait for approval.',
  },
  {
    value: 'acceptEdits',
    title: 'Accept edits',
    body: 'File edits are auto-approved. Commands still ask for approval.',
  },
  {
    value: 'yolo',
    title: 'YOLO',
    body: 'Run every tool without asking. Use only in trusted workspaces.',
  },
] as const

export function SettingsDialog({
  config,
  health,
  theme,
  onTheme,
  onClose,
  onSaved,
  onNotify,
}: {
  config: Config | null
  health: Health | null
  theme: ThemeId
  onTheme: (id: ThemeId) => void
  onClose: () => void
  onSaved: (config: Config) => void
  onNotify: (text: string, tone?: 'info' | 'error') => void
}) {
  const [kind, setKind] = useState('mock')
  const [baseUrl, setBaseUrl] = useState('')
  const [model, setModel] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [maxTokens, setMaxTokens] = useState(8192)
  const [maxTurns, setMaxTurns] = useState(24)
  const [permissionMode, setPermissionMode] = useState('ask')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!config) return
    setKind(config.provider.kind)
    setBaseUrl(config.provider.baseUrl)
    setModel(config.provider.model)
    setMaxTokens(config.provider.maxTokens)
    setMaxTurns(config.maxTurns)
    setPermissionMode(config.permissionMode)
  }, [config])

  const save = async () => {
    setSaving(true)
    try {
      const provider: Record<string, unknown> = { kind, baseUrl, model, maxTokens }
      if (apiKey.trim() !== '') provider.apiKey = apiKey.trim()
      const updated = await api.updateConfig({ permissionMode, maxTurns, provider })
      onSaved(updated)
      onNotify('Settings saved')
      onClose()
    } catch (cause) {
      onNotify(cause instanceof Error ? cause.message : 'Failed to save settings', 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title="Settings"
      subtitle={health ? `weft ${health.version} · ${health.provider}` : 'Model and permission configuration'}
      wide
      onClose={onClose}
      footer={
        <>
          <button className="btn ghost" onClick={onClose}>Cancel</button>
          <button className="btn primary" onClick={() => void save()} disabled={saving || !config}>
            {saving ? 'Saving…' : 'Save changes'}
          </button>
        </>
      }
    >
      <div className="settings-grid">
        <section className="settings-section">
          <h3>Model provider</h3>
          <label className="field">
            <span>Provider</span>
            <select value={kind} onChange={(event) => setKind(event.target.value)}>
              <option value="mock">Mock (offline demo)</option>
              <option value="openai">OpenAI</option>
              <option value="openai-compatible">OpenAI-compatible</option>
              <option value="anthropic">Anthropic</option>
            </select>
          </label>
          <label className="field">
            <span>Base URL</span>
            <input
              value={baseUrl}
              onChange={(event) => setBaseUrl(event.target.value)}
              placeholder={kind === 'anthropic' ? 'https://api.anthropic.com' : 'https://api.openai.com/v1'}
              spellCheck={false}
            />
          </label>
          <label className="field">
            <span>Model</span>
            <input value={model} onChange={(event) => setModel(event.target.value)} placeholder="gpt-5.1" spellCheck={false} />
          </label>
          <label className="field">
            <span>API key {config?.provider.keySet && <em>saved</em>}</span>
            <input
              type="password"
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
              placeholder={config?.provider.keySet ? 'Leave blank to keep the saved key' : 'sk-…'}
              spellCheck={false}
            />
          </label>
          <label className="field">
            <span>Max output tokens</span>
            <input
              type="number"
              min={256}
              step={256}
              value={maxTokens}
              onChange={(event) => setMaxTokens(Number(event.target.value))}
            />
          </label>
        </section>

        <section className="settings-section">
          <h3>Permissions</h3>
          <div className="mode-list">
            {PERMISSION_MODES.map((mode) => (
              <button
                key={mode.value}
                className={`mode-card ${permissionMode === mode.value ? 'active' : ''}`}
                onClick={() => setPermissionMode(mode.value)}
              >
                <span className="mode-radio">{permissionMode === mode.value && <span />}</span>
                <span className="mode-copy">
                  <strong>{mode.title}</strong>
                  <span>{mode.body}</span>
                </span>
              </button>
            ))}
          </div>
          <label className="field">
            <span>Max agent turns per message</span>
            <input
              type="number"
              min={1}
              max={200}
              value={maxTurns}
              onChange={(event) => setMaxTurns(Number(event.target.value))}
            />
          </label>

          <h3 className="spaced">Local server</h3>
          <div className="readonly-row">
            <span>Data directory</span>
            <code>{config?.dataDir ?? '—'}</code>
          </div>
          <div className="readonly-row">
            <span>Listen address</span>
            <code>{config?.listen ?? '—'}</code>
          </div>
          {window.weft && config && (
            <button className="link-btn" onClick={() => void window.weft?.openWith('default', config.dataDir)}>
              <Icon name="external" size={13} /> Open data folder
            </button>
          )}
        </section>
      </div>

      <section className="settings-section settings-appearance">
        <h3>Appearance</h3>
        <p className="settings-hint">Theme applies immediately and is saved on this device.</p>
        <div className="theme-grid">
          {THEMES.map((item) => (
            <button
              key={item.id}
              className={`theme-card ${theme === item.id ? 'active' : ''}`}
              onClick={() => onTheme(item.id)}
            >
              <span
                className="theme-preview"
                style={{ background: item.swatch.bg, borderColor: item.swatch.line }}
              >
                <span
                  className="theme-preview-side"
                  style={{ background: item.swatch.panel, borderColor: item.swatch.line }}
                />
                <span className="theme-preview-body">
                  <span className="theme-preview-line" style={{ background: item.swatch.line }} />
                  <span className="theme-preview-line short" style={{ background: item.swatch.line }} />
                  <span className="theme-preview-chip" style={{ background: item.swatch.accent }} />
                </span>
              </span>
              <span className="theme-copy">
                <strong>
                  {item.label}
                  {theme === item.id && <Icon name="check" size={12} />}
                </strong>
                <span>{item.description}</span>
              </span>
            </button>
          ))}
        </div>
      </section>
    </Modal>
  )
}

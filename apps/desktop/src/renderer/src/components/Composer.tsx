import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'

export function Composer({
  sessionId,
  running,
  onSend,
  onCancel,
}: {
  sessionId: string | null
  running: boolean
  onSend: (text: string) => void
  onCancel: () => void
}) {
  const [text, setText] = useState('')
  const area = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    setText('')
    if (sessionId) area.current?.focus()
  }, [sessionId])

  useEffect(() => {
    const element = area.current
    if (!element) return
    element.style.height = 'auto'
    element.style.height = `${Math.min(element.scrollHeight, 220)}px`
  }, [text])

  const submit = () => {
    const value = text.trim()
    if (!value || running || !sessionId) return
    onSend(value)
    setText('')
  }

  return (
    <div className="composer-wrap">
      <div className={`composer ${running ? 'running' : ''}`}>
        <textarea
          ref={area}
          value={text}
          rows={1}
          placeholder={sessionId ? 'Describe what you want to build or change…' : 'Select a session to start'}
          disabled={!sessionId}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault()
              submit()
            }
          }}
        />
        <div className="composer-bar">
          <span className="composer-hint">Enter to send · Shift+Enter for a new line</span>
          {running ? (
            <button className="send-btn stop" onClick={onCancel} title="Stop run">
              <Icon name="stop" size={15} />
            </button>
          ) : (
            <button className="send-btn" onClick={submit} disabled={!sessionId || !text.trim()} title="Send">
              <Icon name="send" size={16} />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

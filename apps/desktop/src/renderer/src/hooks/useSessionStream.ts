import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import type { WeftEvent } from '../types'

export interface LiveText {
  runId: string
  text: string
}

export function useSessionStream(sessionId: string | null, onEvent?: (event: WeftEvent) => void) {
  const [events, setEvents] = useState<WeftEvent[]>([])
  const [live, setLive] = useState<LiveText | null>(null)
  const [loading, setLoading] = useState(false)
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const seen = useRef<Set<string>>(new Set())
  const source = useRef<EventSource | null>(null)
  const onEventRef = useRef(onEvent)

  useEffect(() => {
    onEventRef.current = onEvent
  }, [onEvent])

  useEffect(() => {
    if (!sessionId) {
      setEvents([])
      setLive(null)
      setError(null)
      setLoading(false)
      setConnected(false)
      return
    }

    let cancelled = false
    seen.current = new Set()
    setEvents([])
    setLive(null)
    setError(null)
    setLoading(true)
    setConnected(false)

    const connect = async () => {
      try {
        const history = await api.events(sessionId, 0)
        if (cancelled) return
        for (const event of history.events) seen.current.add(event.id)
        setEvents(history.events)
        setLoading(false)

        const es = new EventSource(api.streamURL(sessionId, history.lastSeq))
        source.current = es
        es.onopen = () => {
          if (!cancelled) setConnected(true)
        }
        es.onerror = () => {
          if (!cancelled) setConnected(false)
        }
        es.onmessage = (message) => {
          if (cancelled) return
          let event: WeftEvent
          try {
            event = JSON.parse(message.data) as WeftEvent
          } catch {
            return
          }
          if (event.id && seen.current.has(event.id)) return
          if (event.id) seen.current.add(event.id)

          if (event.type === 'assistant.delta') {
            const text = typeof event.data === 'object' && event.data !== null ? (event.data as { text?: string }).text : undefined
            if (!text) return
            setLive((current) => ({
              runId: event.runId ?? '',
              text: (current && current.runId === event.runId ? current.text : '') + text,
            }))
            return
          }

          if (event.type === 'assistant.message' || event.type === 'turn.completed' || event.type === 'error') {
            setLive(null)
          }
          setEvents((current) => [...current, event])
          onEventRef.current?.(event)
        }
      } catch (cause) {
        if (cancelled) return
        setLoading(false)
        setError(cause instanceof Error ? cause.message : 'Failed to load session')
      }
    }

    void connect()

    return () => {
      cancelled = true
      source.current?.close()
      source.current = null
      setConnected(false)
    }
  }, [sessionId])

  return { events, live, loading, connected, error }
}

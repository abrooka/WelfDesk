import { useEffect } from 'react'

export interface ToastItem {
  id: number
  tone: 'info' | 'error'
  text: string
}

export function Toasts({ items, onDismiss }: { items: ToastItem[]; onDismiss: (id: number) => void }) {
  return (
    <div className="toasts">
      {items.map((item) => (
        <Toast key={item.id} item={item} onDismiss={onDismiss} />
      ))}
    </div>
  )
}

function Toast({ item, onDismiss }: { item: ToastItem; onDismiss: (id: number) => void }) {
  useEffect(() => {
    const timer = window.setTimeout(() => onDismiss(item.id), item.tone === 'error' ? 7000 : 3200)
    return () => window.clearTimeout(timer)
  }, [item, onDismiss])

  return (
    <div className={`toast ${item.tone}`} onClick={() => onDismiss(item.id)}>
      {item.text}
    </div>
  )
}

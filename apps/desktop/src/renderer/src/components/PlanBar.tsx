import { useState } from 'react'
import { Icon } from './Icon'
import type { TodoItem, TodosData, WeftEvent } from '../types'

export function latestTodos(events: WeftEvent[]): TodoItem[] {
  for (let i = events.length - 1; i >= 0; i--) {
    const event = events[i]
    if (event.type !== 'todos.updated') continue
    const data = event.data as TodosData | undefined
    if (data?.todos) return data.todos
  }
  return []
}

export function PlanBar({ todos }: { todos: TodoItem[] }) {
  const [open, setOpen] = useState(false)
  const done = todos.filter((todo) => todo.status === 'completed').length
  const current = todos.find((todo) => todo.status === 'in_progress')
  const percent = todos.length > 0 ? Math.round((done / todos.length) * 100) : 0
  const summary = current?.content ?? (done === todos.length ? 'All steps complete' : 'Not started')

  return (
    <div className="plan-wrap">
      <div className={`plan ${open ? 'open' : ''}`}>
        <button className="plan-head" aria-expanded={open} onClick={() => setOpen((value) => !value)}>
          <span className="plan-icon">
            <Icon name="list" size={13} />
          </span>
          <span className="plan-label">Plan</span>
          <span className="plan-meter" aria-hidden>
            <span style={{ width: `${percent}%` }} />
          </span>
          <span className="plan-count">
            {done}/{todos.length}
          </span>
          <span className="plan-current">{summary}</span>
          <span className={`chev ${open ? 'open' : ''}`}>
            <Icon name="chevronRight" size={13} />
          </span>
        </button>
        {open && (
          <ol className="plan-list">
            {todos.map((todo) => (
              <li key={todo.id} className={`plan-item ${todo.status}`}>
                <span className="plan-mark">
                  {todo.status === 'completed' ? (
                    <Icon name="check" size={11} />
                  ) : todo.status === 'in_progress' ? (
                    <span className="spinner" />
                  ) : (
                    <span className="plan-dot" />
                  )}
                </span>
                <span className="plan-text">{todo.content}</span>
              </li>
            ))}
          </ol>
        )}
      </div>
    </div>
  )
}

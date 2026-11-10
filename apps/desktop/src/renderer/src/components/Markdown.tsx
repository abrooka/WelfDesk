import { memo, type MouseEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

function openExternal(event: MouseEvent<HTMLAnchorElement>, href?: string) {
  if (!href) return
  if (!/^https?:\/\//i.test(href)) return
  event.preventDefault()
  if (window.weft) void window.weft.openExternal(href)
  else window.open(href, '_blank', 'noopener,noreferrer')
}

export const Markdown = memo(function Markdown({ text }: { text: string }) {
  return (
    <div className="md selectable">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ href, children }) => (
            <a href={href} onClick={(event) => openExternal(event, href)}>
              {children}
            </a>
          ),
          pre: ({ children }) => <pre className="md-pre">{children}</pre>,
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
})
